package clientdriver_test

import (
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/api"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/crdt/automerge"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	fanoutmemory "github.com/ProjectCartograph/cartograph-engine/v2/internal/fanout/memory"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
	"github.com/ProjectCartograph/cartograph-engine/v2/pkg/client"
	"github.com/ProjectCartograph/cartograph-engine/v2/pkg/client/inproc"
	"github.com/ProjectCartograph/cartograph-engine/v2/pkg/client/remote"
	"github.com/ProjectCartograph/cartograph-engine/v2/pkg/uiconformance"
	"github.com/ProjectCartograph/cartograph-engine/v2/pkg/uiconformance/clientdriver"
)

// am is compiled once for the test binary; it is a quarter of a second.
var am = sync.OnceValues(func() (*automerge.Engine, error) { return automerge.New(2) })

// newEngine composes an engine the way serve does, shared drafts
// included, over memory adapters.
func newEngine(t *testing.T) *engine.Engine {
	t.Helper()
	crdt, err := am()
	if err != nil {
		t.Fatal(err)
	}
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()),
		engine.WithCRDT(crdt), engine.WithDocStore(memory.NewDocStore()), engine.WithFanout(fanoutmemory.New()))
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// The reference driver passes the suite over the in-process client:
// the scenarios describe what the engine does.
func TestSuiteOverInProcess(t *testing.T) {
	uiconformance.Run(t, func(t *testing.T) (uiconformance.Driver, client.Client) {
		c := inproc.New(newEngine(t))
		return clientdriver.New(c), c
	})
}

// And over the HTTP transport: the wire carries the same behaviour.
func TestSuiteOverHTTP(t *testing.T) {
	uiconformance.Run(t, func(t *testing.T) (uiconformance.Driver, client.Client) {
		srv := httptest.NewServer(api.Handler(newEngine(t)))
		t.Cleanup(srv.Close)
		c := remote.New(srv.URL)
		return clientdriver.New(c), c
	})
}
