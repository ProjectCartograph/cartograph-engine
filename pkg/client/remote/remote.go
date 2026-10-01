// Package remote is the client.Client over the HTTP contract: what an
// interface uses when the engine is another process, on this machine
// (a UNIX socket) or elsewhere (TCP, through whatever proxy the
// deployment runs). It speaks only the contract in openapi.yaml, so it
// is the one place in an interface's dependencies where an HTTP path
// appears. The draft operations (Edit, OpsSince, Subscribe) answer
// ErrUnsupported until the ops and events endpoints land.
package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/pkg/client"
	"github.com/ProjectCartograph/cartograph-engine/pkg/merge"
)

// Client talks to Base ("http://host/api/v1").
type Client struct {
	Base string
	HTTP *http.Client
	// Headers are sent with every request: the proxy identity headers
	// when the client speaks for a person behind one, a bearer token
	// later.
	Headers http.Header
	actor   string
}

var _ client.Client = (*Client)(nil)

// Dial returns a client for an address: "http://..." or "https://..."
// over TCP, "unix:///path/to.sock" over a UNIX domain socket (the
// contract is then served at the socket's root, as `cartograph serve` does
// with CARTOGRAPH_ADDR=unix://...).
func Dial(addr string) (*Client, error) {
	if path, ok := strings.CutPrefix(addr, "unix://"); ok {
		tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		}}
		return &Client{Base: "http://cartograph/api/v1", HTTP: &http.Client{Transport: tr, Timeout: 30 * time.Second}, Headers: http.Header{}}, nil
	}
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") {
		return nil, fmt.Errorf("address %q: want http://, https:// or unix://", addr)
	}
	return &Client{Base: strings.TrimSuffix(addr, "/") + "/api/v1", HTTP: &http.Client{Timeout: 30 * time.Second}, Headers: http.Header{}}, nil
}

// New returns a client over an exact API base, for tests.
func New(base string) *Client {
	return &Client{Base: strings.TrimSuffix(base, "/"), HTTP: http.DefaultClient, Headers: http.Header{}}
}

func (c *Client) do(ctx context.Context, method, path string, body any, into any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, rd)
	if err != nil {
		return 0, nil, err
	}
	for k, vs := range c.Headers {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 && into != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, into); err != nil {
			return resp.StatusCode, raw, fmt.Errorf("%s %s: %w", method, path, err)
		}
	}
	return resp.StatusCode, raw, nil
}

// refusal turns a 4xx with a problem list into the error an interface
// shows; anything else is a transport failure.
func refusal(method, path string, status int, raw []byte) error {
	var pl struct {
		Problems []client.Problem `json:"problems"`
	}
	if (status == 400 || status == 409 || status == 422) && json.Unmarshal(raw, &pl) == nil && len(pl.Problems) > 0 {
		return &client.Refused{Problems: pl.Problems}
	}
	if status == 404 {
		return client.ErrNotFound
	}
	return fmt.Errorf("%s %s: %d %s", method, path, status, strings.TrimSpace(string(raw)))
}

func (c *Client) Actor(ctx context.Context) (string, error) {
	if c.actor != "" {
		return c.actor, nil
	}
	s, err := c.Settings(ctx)
	if err != nil {
		return "", err
	}
	if v := c.Headers.Get("X-Forwarded-User"); v != "" {
		return v, nil
	}
	return s.Operator, nil
}

func (c *Client) Kinds(ctx context.Context) ([]string, error) {
	var rows []struct {
		Kind string `json:"kind"`
	}
	st, raw, err := c.do(ctx, http.MethodGet, "/kinds", nil, &rows)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, refusal("GET", "/kinds", st, raw)
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Kind
	}
	return out, nil
}

func (c *Client) Schema(ctx context.Context, kind string) ([]byte, error) {
	st, raw, err := c.do(ctx, http.MethodGet, "/schemas/"+kind, nil, nil)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, refusal("GET", "/schemas/"+kind, st, raw)
	}
	return raw, nil
}

func (c *Client) Flow(ctx context.Context, kind string) (client.Flow, bool, error) {
	var f client.Flow
	st, raw, err := c.do(ctx, http.MethodGet, "/flows/"+kind, nil, &f)
	if err != nil {
		return nil, false, err
	}
	if st == 404 {
		return nil, false, nil
	}
	if st != 200 {
		return nil, false, refusal("GET", "/flows/"+kind, st, raw)
	}
	return f, true, nil
}

func (c *Client) Settings(ctx context.Context) (client.Settings, error) {
	var s struct {
		GoalLevels       []string            `json:"goalLevels"`
		ProjectLevelName string              `json:"projectLevelName"`
		Operator         string              `json:"operator"`
		Examples         map[string][]string `json:"examples"`
	}
	st, raw, err := c.do(ctx, http.MethodGet, "/settings", nil, &s)
	if err != nil {
		return client.Settings{}, err
	}
	if st != 200 {
		return client.Settings{}, refusal("GET", "/settings", st, raw)
	}
	return client.Settings{GoalLevels: s.GoalLevels, ProjectLevelName: s.ProjectLevelName, Operator: s.Operator, Examples: s.Examples}, nil
}

func (c *Client) List(ctx context.Context, kind, query string) ([]client.Summary, error) {
	var page struct {
		Items []struct {
			Kind      string            `json:"kind"`
			ID        string            `json:"id"`
			Name      string            `json:"name"`
			Version   int               `json:"version"`
			UpdatedOn time.Time         `json:"updatedOn"`
			Labels    map[string]string `json:"labels"`
		} `json:"items"`
	}
	path := "/manifests/" + kind
	if query != "" {
		path += "?q=" + query
	}
	st, raw, err := c.do(ctx, http.MethodGet, path, nil, &page)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, refusal("GET", path, st, raw)
	}
	out := make([]client.Summary, len(page.Items))
	for i, r := range page.Items {
		out[i] = client.Summary{Kind: r.Kind, ID: r.ID, Name: r.Name, Version: r.Version, UpdatedOn: r.UpdatedOn, Labels: r.Labels}
	}
	return out, nil
}

type manifestView struct {
	Version struct {
		Number int `json:"number"`
	} `json:"version"`
	Manifest map[string]any `json:"manifest"`
	Yaml     string         `json:"yaml"`
}

func (c *Client) Get(ctx context.Context, kind, id string) (client.Manifest, error) {
	var v manifestView
	path := "/manifests/" + kind + "/" + id
	st, raw, err := c.do(ctx, http.MethodGet, path, nil, &v)
	if err != nil {
		return client.Manifest{}, err
	}
	if st != 200 {
		return client.Manifest{}, refusal("GET", path, st, raw)
	}
	return client.Manifest{Kind: kind, ID: id, Version: v.Version.Number, Doc: v.Manifest, Text: []byte(v.Yaml)}, nil
}

func (c *Client) Working(ctx context.Context, kind, id string) (client.Manifest, error) {
	var v manifestView
	path := "/manifests/" + kind + "/" + id + "/working"
	st, raw, err := c.do(ctx, http.MethodGet, path, nil, &v)
	if err != nil {
		return client.Manifest{}, err
	}
	if st != 200 {
		return client.Manifest{}, refusal("GET", path, st, raw)
	}
	return client.Manifest{Kind: kind, ID: id, Doc: v.Manifest, Text: []byte(v.Yaml)}, nil
}

func (c *Client) SaveWorking(ctx context.Context, kind, id string, doc map[string]any) error {
	// The contract's working save takes the text in the deployment's
	// codec; JSON is valid YAML, so a JSON encoding is accepted by the
	// YAML codec and is exactly the JSON codec's own text.
	b, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	path := "/manifests/" + kind + "/" + id + "/working"
	st, raw, err := c.do(ctx, http.MethodPut, path, map[string]any{"yaml": string(b)}, nil)
	if err != nil {
		return err
	}
	if st < 200 || st >= 300 {
		return refusal("PUT", path, st, raw)
	}
	return nil
}

func (c *Client) SaveVersion(ctx context.Context, kind, id string, doc map[string]any, reason string) (client.Version, error) {
	var v client.Version
	path := "/manifests/" + kind + "/" + id
	st, raw, err := c.do(ctx, http.MethodPut, path, map[string]any{"manifest": doc, "reason": reason}, &v)
	if err != nil {
		return client.Version{}, err
	}
	if st != 200 {
		return client.Version{}, refusal("PUT", path, st, raw)
	}
	return v, nil
}

func (c *Client) Validate(ctx context.Context, kind string, doc map[string]any) ([]client.Problem, error) {
	var pl struct {
		Problems []client.Problem `json:"problems"`
	}
	path := "/validate/" + kind
	st, raw, err := c.do(ctx, http.MethodPost, path, map[string]any{"manifest": doc}, &pl)
	if err != nil {
		return nil, err
	}
	if st != 200 && st != 422 {
		return nil, refusal("POST", path, st, raw)
	}
	if st == 422 {
		_ = json.Unmarshal(raw, &pl)
	}
	if pl.Problems == nil {
		pl.Problems = []client.Problem{}
	}
	return pl.Problems, nil
}

func (c *Client) Checks(ctx context.Context, kind, id string) ([]client.Check, error) {
	if kind != "Project" {
		return []client.Check{}, nil
	}
	var pc struct {
		Items []struct {
			Key     string `json:"key"`
			State   string `json:"state"`
			Section string `json:"section"`
			Message string `json:"message"`
			Fix     struct {
				Section string `json:"section"`
			} `json:"fix"`
		} `json:"items"`
	}
	path := "/manifests/Project/" + id + "/checks"
	st, raw, err := c.do(ctx, http.MethodGet, path, nil, &pc)
	if err != nil {
		return nil, err
	}
	if st != 200 {
		return nil, refusal("GET", path, st, raw)
	}
	out := make([]client.Check, len(pc.Items))
	for i, it := range pc.Items {
		out[i] = client.Check{Key: it.Key, State: it.State, Path: it.Section, Message: it.Message, Fix: it.Fix.Section}
	}
	return out, nil
}

func (c *Client) Edit(context.Context, string, string, []merge.Op) (client.EditResult, error) {
	return client.EditResult{}, client.ErrUnsupported
}

func (c *Client) OpsSince(context.Context, string, string, int64) ([]client.Op, error) {
	return nil, client.ErrUnsupported
}

func (c *Client) Subscribe(context.Context, string, string) (<-chan client.Event, error) {
	return nil, client.ErrUnsupported
}

func (c *Client) Close() error { return nil }
