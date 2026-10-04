// Package laya is a Decider backed by Laya, an open decision model, run as
// a sidecar beside Cartograph (deploy/laya): the model needs a runtime the
// static binary does not carry, so it is reached over HTTP on the same
// host or network. The protocol is Laya's own systemOne call as JSON:
//
//	POST <url>/decide
//	{"state": "...", "questions": {"name": {"type": "choice"|"noul",
//	  "instructions": "...", "criteria": {"key": "description"}}}}
//	-> {"answers": {"name": {"choice": "key", "probabilities": {...},
//	  "noul": 0.82}}}
//
// A sidecar that cannot be reached, or answers with an error, is
// decide.ErrUnavailable, so the engine answers as it would without one.
package laya

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/decide"
)

// Decider is the Laya sidecar's client.
type Decider struct {
	url    string
	client *http.Client
}

// New returns a client for the sidecar at url (http://host:port), each
// call given at most timeout.
func New(url string, timeout time.Duration) *Decider {
	return &Decider{url: strings.TrimRight(url, "/"), client: &http.Client{Timeout: timeout}}
}

type question struct {
	Type         string   `json:"type"`
	Instructions string   `json:"instructions"`
	Criteria     criteria `json:"criteria,omitempty"`
}

// criteria are a choice's options as a JSON object in the order they were
// given. Laya reads the order (the first option is favoured, and keys
// carry meaning), so it must not be the alphabetical order Go gives a map.
type criteria []decide.Option

func (c criteria) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, o := range c {
		if i > 0 {
			b.WriteByte(',')
		}
		k, err := json.Marshal(o.Key)
		if err != nil {
			return nil, err
		}
		v, err := json.Marshal(o.Description)
		if err != nil {
			return nil, err
		}
		b.Write(k)
		b.WriteByte(':')
		b.Write(v)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

type answer struct {
	Choice        string             `json:"choice"`
	Probabilities map[string]float64 `json:"probabilities"`
	Noul          *float64           `json:"noul"`
}

// Decide asks the sidecar every question in one call.
// Ready asks the sidecar's GET /ready, which answers 200 once its model
// is loaded.
func (d *Decider) Ready(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.url+"/ready", nil)
	if err != nil {
		return err
	}
	resp, err := d.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", decide.ErrUnavailable, err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: the sidecar answered %s", decide.ErrUnavailable, resp.Status)
	}
	return nil
}

func (d *Decider) Decide(ctx context.Context, state string, questions map[string]decide.Question) (map[string]decide.Answer, error) {
	body := struct {
		State     string              `json:"state"`
		Questions map[string]question `json:"questions"`
	}{State: state, Questions: map[string]question{}}
	for name, q := range questions {
		switch q.Type {
		case decide.Choice:
			body.Questions[name] = question{Type: "choice", Instructions: q.Instructions, Criteria: criteria(q.Options)}
		case decide.YesNo:
			body.Questions[name] = question{Type: "noul", Instructions: q.Instructions}
		default:
			return nil, fmt.Errorf("decide: question %s has no type Laya answers: %q", name, q.Type)
		}
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url+"/decide", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := d.client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("%w: %v", decide.ErrUnavailable, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%w: the sidecar answered %s", decide.ErrUnavailable, resp.Status)
	}
	var out struct {
		Answers map[string]answer `json:"answers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("%w: %v", decide.ErrUnavailable, err)
	}
	answers := make(map[string]decide.Answer, len(questions))
	for name, q := range questions {
		a, ok := out.Answers[name]
		if !ok {
			return nil, fmt.Errorf("%w: no answer to %s", decide.ErrUnavailable, name)
		}
		switch q.Type {
		case decide.Choice:
			answers[name] = decide.Answer{Choice: a.Choice, Probabilities: a.Probabilities}
		case decide.YesNo:
			if a.Noul == nil {
				return nil, fmt.Errorf("%w: no probability for %s", decide.ErrUnavailable, name)
			}
			answers[name] = decide.Answer{Yes: *a.Noul}
		}
	}
	return answers, nil
}
