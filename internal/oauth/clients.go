package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"syscall"
	"time"
)

// A client is the application an agent runs in. It identifies itself in
// one of two ways, and neither leaves anything to store:
//
//   - by a Client ID Metadata Document: its client_id is an https
//     address serving its name and redirect addresses, fetched when its
//     person is asked to consent;
//   - by dynamic registration (RFC 7591): it posts the same, and its
//     client_id is that registration, signed.

// client is what is known of a client when its person consents.
type client struct {
	ID        string
	Name      string
	Redirects []string
}

// maxMetadata bounds a Client ID Metadata Document.
const maxMetadata = 64 << 10

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name      string   `json:"client_name"`
		Redirects []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, maxMetadata)).Decode(&req); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "the registration is not JSON")
		return
	}
	if err := checkRedirects(req.Redirects); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
		return
	}
	name := clientName(req.Name, req.Redirects[0])
	id := s.seal(claims{Type: typeClient, Name: name, Redirects: req.Redirects})
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  id,
		"client_name":                name,
		"redirect_uris":              req.Redirects,
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	})
}

// lookup returns the client a client_id names.
func (s *Server) lookup(ctx context.Context, id string) (client, error) {
	if strings.HasPrefix(id, prefixes[typeClient]) {
		var c claims
		if err := s.open(id, typeClient, &c); err != nil {
			return client{}, errors.New("the client_id is not one this server registered")
		}
		return client{ID: id, Name: c.Name, Redirects: c.Redirects}, nil
	}
	return s.fetchClient(ctx, id)
}

// fetchClient reads a Client ID Metadata Document: an https address with
// a path, whose document names itself by that address.
func (s *Server) fetchClient(ctx context.Context, id string) (client, error) {
	u, err := url.Parse(id)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Path == "" || u.Path == "/" || u.Fragment != "" || u.User != nil {
		return client{}, errors.New("the client_id is neither registered here nor an https address of client metadata")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, id, nil)
	if err != nil {
		return client{}, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.o.Fetch.Do(req)
	if err != nil {
		return client{}, fmt.Errorf("the client metadata could not be fetched: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return client{}, fmt.Errorf("the client metadata answered %d", resp.StatusCode)
	}
	var doc struct {
		ID        string   `json:"client_id"`
		Name      string   `json:"client_name"`
		Redirects []string `json:"redirect_uris"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxMetadata)).Decode(&doc); err != nil {
		return client{}, errors.New("the client metadata is not JSON")
	}
	if doc.ID != id {
		return client{}, errors.New("the client metadata names another client_id")
	}
	if err := checkRedirects(doc.Redirects); err != nil {
		return client{}, err
	}
	return client{ID: id, Name: clientName(doc.Name, id), Redirects: doc.Redirects}, nil
}

// checkRedirects allows https addresses, and http on the loopback
// interface for a client on the person's own machine (RFC 8252).
func checkRedirects(uris []string) error {
	if len(uris) == 0 || len(uris) > 10 {
		return errors.New("name from one to ten redirect_uris")
	}
	for _, raw := range uris {
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" || u.Fragment != "" || u.User != nil {
			return fmt.Errorf("%q is not a redirect address", raw)
		}
		if u.Scheme != "https" && !(u.Scheme == "http" && loopback(u.Hostname())) {
			return fmt.Errorf("%q: a redirect address is https, or http on this machine", raw)
		}
	}
	return nil
}

func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// redirectAllowed is whether uri is one the client named. A loopback
// address matches on any port, which a native client picks when it
// starts (RFC 8252, section 7.3).
func (c client) redirectAllowed(uri string) bool {
	if slices.Contains(c.Redirects, uri) {
		return true
	}
	got, err := url.Parse(uri)
	if err != nil || got.Scheme != "http" || !loopback(got.Hostname()) {
		return false
	}
	for _, r := range c.Redirects {
		want, err := url.Parse(r)
		if err == nil && want.Scheme == "http" && want.Hostname() == got.Hostname() && want.Path == got.Path && want.RawQuery == got.RawQuery {
			return true
		}
	}
	return false
}

// clientName is how a client is shown to its person and recorded beside
// them: its own name, else its address's host.
func clientName(name, fallback string) string {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		if u, err := url.Parse(fallback); err == nil && u.Host != "" {
			name = u.Hostname()
		}
	}
	if len(name) > 64 {
		name = strings.TrimSpace(name[:64])
	}
	if name == "" {
		name = "an agent"
	}
	return name
}

// publicClient fetches client metadata from the public internet only:
// an address that resolves to a private, loopback or link-local one is
// refused when connecting, so a client_id cannot reach inside the
// deployment's network.
func publicClient() *http.Client {
	dialer := &net.Dialer{
		Timeout: 5 * time.Second,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			ip := net.ParseIP(host)
			if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				return fmt.Errorf("%s is not a public address", host)
			}
			return nil
		},
	}
	return &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{DialContext: dialer.DialContext, Proxy: nil, TLSHandshakeTimeout: 5 * time.Second},
		// A metadata document is served where its client_id says.
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
