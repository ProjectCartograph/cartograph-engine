package engine

import (
	"context"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
)

// refuseAgent is the agent ceiling (docs/adr/0016): an agent may read and
// edit drafts, and propose; anything that makes the record (a version, a
// reading, a state change, a handoff, a deletion, an import, the access
// list) waits for its person. It holds whatever the access policy, and
// with none, so a local agent on a vault keeps it too.
func refuseAgent(ctx context.Context) error {
	if identity.PrincipalFrom(ctx).Agent != "" {
		return identity.ErrAgentProposes
	}
	return nil
}
