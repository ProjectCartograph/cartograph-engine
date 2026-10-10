package api_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/api"
	apigen "github.com/ProjectCartograph/cartograph-engine/v2/internal/api/gen"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

type acts struct {
	mu sync.Mutex
	in []activity.Event
}

func (a *acts) Record(e activity.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.in = append(a.in, e)
}

func (a *acts) named(name string) []activity.Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []activity.Event
	for _, e := range a.in {
		if e.Name == name {
			out = append(out, e)
		}
	}
	return out
}

func tracedServer(t *testing.T, rec activity.Recorder) string {
	t.Helper()
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()), engine.WithActivity(rec))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(api.Handler(e))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestEventsAreTakenOnlyWhenTheTraceIsOn(t *testing.T) {
	t.Parallel()
	off := tracedServer(t, nil)
	s := decode[apigen.Session](t, doJSON(t, http.MethodGet, off+"/session", nil, nil))
	if s.TraceOn == nil || *s.TraceOn {
		t.Fatalf("session with the trace off: %+v", s.TraceOn)
	}
	batch := apigen.EventBatch{Session: "w-1", Events: []apigen.InterfaceEvent{{Name: apigen.ActPress}}}
	if resp := doJSON(t, http.MethodPost, off+"/events", batch, nil); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("trace off: got %d, want 404", resp.StatusCode)
	}

	var a acts
	on := tracedServer(t, &a)
	s = decode[apigen.Session](t, doJSON(t, http.MethodGet, on+"/session", nil, nil))
	if s.TraceOn == nil || !*s.TraceOn {
		t.Fatal("session with the trace on says it is off")
	}
	step, kind := "aim", "Goal"
	batch.Events = append(batch.Events, apigen.InterfaceEvent{Name: apigen.ActStepEnter, Step: &step, Kind: &kind})
	resp := doJSON(t, http.MethodPost, on+"/events", batch, nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("got %d, want 202", resp.StatusCode)
	}
	if got := decode[struct{ Kept int }](t, resp); got.Kept != 2 {
		t.Fatalf("kept %d, want 2", got.Kept)
	}
	if e := a.named(activity.StepEnter); len(e) != 1 || e[0].Source != activity.Interface || e[0].Session != "w-1" {
		t.Fatalf("kept %+v", a.in)
	}
}

func TestABatchCarryingAValueIsRefusedWhole(t *testing.T) {
	t.Parallel()
	var a acts
	base := tracedServer(t, &a)
	field := "/spec/objective"
	written := "Feed the town"
	batch := apigen.EventBatch{Session: "w-1", Events: []apigen.InterfaceEvent{
		{Name: apigen.ActFieldSet, Field: &field},
		{Name: apigen.ActStepEnter, Step: &written},
	}}
	if resp := doJSON(t, http.MethodPost, base+"/events", batch, nil); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", resp.StatusCode)
	}
	if len(a.in) != 0 {
		t.Fatalf("kept %+v from a refused batch", a.in)
	}
}

func TestTheServerRecordsAPersonsVersions(t *testing.T) {
	t.Parallel()
	var a acts
	base := tracedServer(t, &a)
	bad := "apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec:\n  parent: nope\n"
	doJSON(t, http.MethodPut, base+"/manifests/Team/t1", apigen.WriteRequest{Yaml: &bad, Reason: "save"}, nil)
	doJSON(t, http.MethodPut, base+"/manifests/Team/t1", apigen.WriteRequest{Yaml: strPtr(teamYAML), Reason: "save"}, nil)
	saves := a.named(activity.VersionSave)
	if len(saves) != 2 {
		t.Fatalf("version saves %+v", a.in)
	}
	if saves[0].Outcome != activity.Refused || len(saves[0].Paths) == 0 || saves[0].Paths[0] != "/spec/parent" || saves[0].Source != activity.Server {
		t.Fatalf("refused save %+v", saves[0])
	}
	if saves[1].Outcome != activity.OK || saves[1].Kind != "Team" || saves[1].Record != "t1" {
		t.Fatalf("kept save %+v", saves[1])
	}
}
