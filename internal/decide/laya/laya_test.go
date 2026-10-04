package laya_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/decide"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/decide/conformance"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/decide/laya"
)

// sidecar answers as deploy/laya does: the first option most likely, and
// every statement more likely than not.
func sidecar(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ready" && r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"ready":true}`))
			return
		}
		if r.URL.Path != "/decide" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var in struct {
			Questions map[string]struct {
				Type     string            `json:"type"`
				Criteria map[string]string `json:"criteria"`
			} `json:"questions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		out := map[string]any{}
		for name, q := range in.Questions {
			switch q.Type {
			case "choice":
				probs := map[string]float64{}
				first := ""
				for key := range q.Criteria {
					if first == "" || key < first {
						first = key
					}
				}
				for key := range q.Criteria {
					probs[key] = 0
				}
				probs[first] = 1
				out[name] = map[string]any{"choice": first, "probabilities": probs}
			case "noul":
				out[name] = map[string]any{"noul": 0.8}
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": out})
	}))
}

func TestConformance(t *testing.T) {
	srv := sidecar(t)
	defer srv.Close()
	conformance.Run(t, laya.New(srv.URL, 2*time.Second))
}

// A sidecar that is down is unavailable, not a failure of the record: the
// engine answers as it would without one.
func TestASidecarThatIsDownIsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "loading", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	q := map[string]decide.Question{"x": {Type: decide.YesNo, Instructions: "?"}}
	if _, err := laya.New(srv.URL, time.Second).Decide(context.Background(), "x", q); !errors.Is(err, decide.ErrUnavailable) {
		t.Fatalf("a sidecar answering 503: %v", err)
	}
	srv.Close()
	if _, err := laya.New(srv.URL, time.Second).Decide(context.Background(), "x", q); !errors.Is(err, decide.ErrUnavailable) {
		t.Fatalf("a sidecar that is gone: %v", err)
	}
}

// A choice's options reach the model in the order they were put, not in
// Go's alphabetical order for a map: Laya reads the order.
func TestOptionsKeepTheirOrder(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var raw struct {
			Questions map[string]struct {
				Criteria json.RawMessage `json:"criteria"`
			} `json:"questions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&raw)
		got = string(raw.Questions["q"].Criteria)
		_ = json.NewEncoder(w).Encode(map[string]any{"answers": map[string]any{"q": map[string]any{"choice": "state", "probabilities": map[string]float64{"state": 1, "action": 0}}}})
	}))
	defer srv.Close()
	q := map[string]decide.Question{"q": {Type: decide.Choice, Instructions: "?", Options: []decide.Option{{Key: "state", Description: "a state"}, {Key: "action", Description: "an action"}}}}
	if _, err := laya.New(srv.URL, time.Second).Decide(context.Background(), "x", q); err != nil {
		t.Fatal(err)
	}
	if got != `{"state":"a state","action":"an action"}` {
		t.Fatalf("the options were sent as %s", got)
	}
}
