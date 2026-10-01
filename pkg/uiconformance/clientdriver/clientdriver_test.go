package clientdriver_test

import (
	codecyaml "github.com/ProjectCartograph/cartograph-engine/internal/codec/yaml"
	"net/http/httptest"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/internal/api"
	"github.com/ProjectCartograph/cartograph-engine/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/internal/store/memory"
	"github.com/ProjectCartograph/cartograph-engine/pkg/client"
	"github.com/ProjectCartograph/cartograph-engine/pkg/client/inproc"
	"github.com/ProjectCartograph/cartograph-engine/pkg/client/remote"
	"github.com/ProjectCartograph/cartograph-engine/pkg/uiconformance"
	"github.com/ProjectCartograph/cartograph-engine/pkg/uiconformance/clientdriver"
)

func newEngine(t *testing.T) *engine.Engine {
	t.Helper()
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithOpLog(memory.NewOpLog()), engine.WithCodec(codecyaml.New()))
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
