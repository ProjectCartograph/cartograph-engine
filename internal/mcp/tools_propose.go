package mcp

import (
	"context"
	"encoding/json"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
)

// registerProposeTools adds the tools that hand work to a person: a change set, or a manifest, proposed.
func registerProposeTools(s *sdk.Server, o Options, person identity.Principal) {
	e := o.Engine

	tool(s, o, person, &sdk.Tool{Name: "propose", Description: "Propose your change set for your person to review and accept in Cartograph, whole: every draft in it is checked with the others, " +
		"as a person's save would be, and saved together, in the order their references need, when they accept.", Annotations: proposal},
		func(c call, in proposeIn) (any, error) {
			cs, c, _, err := c.inChangeSet(in.ChangeSet, true)
			if err != nil {
				return nil, err
			}
			return proposeChangeSet(c, cs, in.Reason, in.OpenChecks)
		})

	tool(s, o, person, &sdk.Tool{Name: "propose_save", Description: "Propose your change set, after saving this manifest into it when you pass one: the same as propose, kept for agents that know it.", Annotations: proposal},
		func(c call, in proposeSaveIn) (any, error) {
			cs, c, _, err := c.inChangeSet(in.ChangeSet, true)
			if err != nil {
				return nil, err
			}
			if in.Manifest != nil {
				text, err := e.Codec().Encode(in.Manifest)
				if err != nil {
					return nil, err
				}
				if err := e.SaveInChangeSet(c.ctx, cs.ID, in.Kind, in.ID, text); err != nil {
					return nil, err
				}
			}
			waive := map[string]map[string]string{}
			if len(in.OpenChecks) > 0 {
				waive[in.Kind+"/"+in.ID] = in.OpenChecks
			}
			return proposeChangeSet(c, cs, in.Reason, waive)
		})

	tool(s, o, person, &sdk.Tool{Name: "propose_set", Description: "Propose your change set, after saving these manifests into it when you pass them: the same as propose, kept for agents that know it.", Annotations: proposal},
		func(c call, in proposeSetIn) (any, error) {
			cs, c, _, err := c.inChangeSet(in.ChangeSet, true)
			if err != nil {
				return nil, err
			}
			for _, m := range in.Manifests {
				if m.Manifest == nil {
					continue
				}
				text, err := e.Codec().Encode(m.Manifest)
				if err != nil {
					return nil, err
				}
				if err := e.SaveInChangeSet(c.ctx, cs.ID, m.Kind, m.ID, text); err != nil {
					return nil, err
				}
			}
			return proposeChangeSet(c, cs, in.Reason, in.OpenChecks)
		})

	tool(s, o, person, &sdk.Tool{Name: "propose_item", Description: "Propose recording one item of a series, such as a KPI reading (kind KPIReadings, series readings), for your person to accept.", Annotations: proposal},
		func(c call, in proposeItemIn) (any, error) {
			p, err := e.ProposeAppend(c.ctx, in.Kind, in.ID, in.Series, in.Item, in.Reason)
			if err == nil {
				c.announce(step{Step: "propose", Kind: in.Kind, ID: in.ID, Proposal: p.ID, Parts: 1})
			}
			return proposed(p), err
		})

	tool(s, o, person, &sdk.Tool{Name: "propose_state", Description: "Propose moving a project: defined, handed off or cancelled. A handoff must pass the project's blocking checks.", Annotations: proposal},
		func(c call, in proposeStateIn) (any, error) {
			p, err := e.ProposeState(c.ctx, in.Project, in.To, in.Reason)
			if err == nil {
				c.announce(step{Step: "propose", Kind: "Project", ID: in.Project, Proposal: p.ID, Parts: 1})
			}
			return proposed(p), err
		})

	for _, kind := range e.FlowKinds() {
		kind := kind
		s.AddPrompt(&sdk.Prompt{Name: "guide_" + strings.ToLower(kind), Description: "Define a " + kind + " with the person, step by step, as Cartograph's guide says."},
			func(ctx context.Context, _ *sdk.GetPromptRequest) (*sdk.GetPromptResult, error) {
				g, err := e.Guide(ctx, kind, "", "")
				if err != nil {
					return nil, err
				}
				b, err := json.MarshalIndent(g, "", " ")
				if err != nil {
					return nil, err
				}
				text := "Help me define a " + kind + " in Cartograph. Work as Cartograph's instructions say: settle the level first where there is one " +
					"(then call guide again for that level), go step by step, make every link, reuse my organisation's records before defining new ones, " +
					"ask me for what only I know with examples, save drafts and meet every check, then propose. The guide:\n\n" + string(b)
				return &sdk.GetPromptResult{Messages: []*sdk.PromptMessage{{Role: "user", Content: &sdk.TextContent{Text: text}}}}, nil
			})
	}
}
