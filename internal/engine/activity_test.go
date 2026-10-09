package engine_test

import (
	"context"
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/activity"
	codecyaml "github.com/ProjectCartograph/cartograph-engine/v2/internal/codec/yaml"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/store/memory"
)

type keptActs []activity.Event

func (k *keptActs) Record(e activity.Event) { *k = append(*k, e) }

// Only a person's own request is their work: the engine's own writes and
// an agent's are left out of the people's trace.
func TestOnlyAPersonsRequestsAreRecorded(t *testing.T) {
	t.Parallel()
	var k keptActs
	e, err := engine.New(memory.NewManifestStore(), memory.NewOperationalStore(), engine.WithCodec(codecyaml.New()), engine.WithActivity(&k))
	if err != nil {
		t.Fatal(err)
	}
	y := []byte("apiVersion: cartograph/v1\nkind: Team\nmetadata:\n  id: t1\n  name: Team One\nspec: {}\n")
	if _, err := e.Commit(context.Background(), "Team", "t1", y, "anyone", "the engine's own"); err != nil {
		t.Fatal(err)
	}
	agent := engine.ByPerson(identity.WithPrincipal(context.Background(), identity.Principal{Subject: "p", Agent: "helper"}))
	_ = e.PutWorking(agent, "Team", "t1", y)
	if len(k) != 0 {
		t.Fatalf("recorded %+v", k)
	}
	person := engine.ByPerson(identity.WithPrincipal(context.Background(), identity.Principal{Subject: "p"}))
	if err := e.PutWorking(person, "Team", "t1", y); err != nil {
		t.Fatal(err)
	}
	if len(k) != 1 || k[0].Name != activity.DraftSave || k[0].Person != "p" || k[0].Outcome != activity.OK {
		t.Fatalf("recorded %+v", k)
	}
}
