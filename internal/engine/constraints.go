package engine

import (
	"context"
	"fmt"
	"sort"
)

// The triple constraint (TAXONOMY.md D60): a project's scope, schedule
// and cost, the stance it takes on each, and how exposed each is to its
// risks. Worked out here so every interface, the charter and the agents
// see the same triangle.

// Sides of the triangle, in the order they are drawn.
var Sides = []string{"scope", "schedule", "cost"}

// Stances a project takes on a side.
const (
	StanceHold    = "hold"
	StanceAdjust  = "adjust"
	StanceConcede = "concede"
)

// Constraints is a project's triangle.
type Constraints struct {
	Sides []Side `json:"sides"`
	// MostConstrained is the side with the most weighted risk, empty when
	// no side carries any or two lead together.
	MostConstrained string `json:"mostConstrained,omitempty"`
	// Unplaced are the risks and issues that name no side.
	Unplaced []string `json:"unplaced,omitempty"`
}

// Side is one side of the triangle.
type Side struct {
	Constraint string `json:"constraint"`
	// Stance is hold, adjust or concede; empty when the project has not
	// said.
	Stance string `json:"stance,omitempty"`
	// Exposure is the sum over its risks of likelihood times impact on
	// this side, each low 1, medium 2, high 3 (an issue has happened, so
	// its likelihood counts as high).
	Exposure int `json:"exposure"`
	// Share is this side's part of the exposure of all three.
	Share float64    `json:"share"`
	Risks []SideRisk `json:"risks"`
	// Unweighed are its risks with no likelihood or no impact on it,
	// counted apart rather than guessed.
	Unweighed int `json:"unweighed"`
	// Unanswered are its risks with neither a response nor a mitigation.
	Unanswered int `json:"unanswered"`
}

// SideRisk is one risk on a side.
type SideRisk struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	Impact     string `json:"impact,omitempty"`
	Likelihood string `json:"likelihood,omitempty"`
	Weight     int    `json:"weight"`
	On         string `json:"on,omitempty"`
	Response   string `json:"response,omitempty"`
	Spends     string `json:"spends,omitempty"`
	// Implied is a schedule place read from a milestone whose timing names
	// the risk (D47), not written on the risk.
	Implied bool `json:"implied,omitempty"`
}

// ConstraintsOf reads a project's triangle from its spec.
func ConstraintsOf(spec map[string]any) Constraints {
	stances, _ := spec["constraints"].(map[string]any)
	sides := map[string]*Side{}
	var out Constraints
	for _, s := range Sides {
		st, _ := stances[s].(string)
		sides[s] = &Side{Constraint: s, Stance: st, Risks: []SideRisk{}}
	}

	// A milestone whose timing names a risk is that risk moving the
	// schedule there (D47), unless the risk says so itself.
	impliedOn := map[string]string{}
	for _, m := range listOf(spec["milestones"]) {
		mid, _ := m["id"].(string)
		timing, _ := m["timing"].(map[string]any)
		for _, r := range anyList(timing["risks"]) {
			if rid, _ := r.(string); rid != "" && impliedOn[rid] == "" {
				impliedOn[rid] = mid
			}
		}
	}

	total := 0
	for _, r := range listOf(spec["risks"]) {
		id, _ := r["id"].(string)
		typ, _ := r["type"].(string)
		lik, _ := r["likelihood"].(string)
		if typ == "issue" {
			lik = "high"
		}
		response, _ := r["response"].(string)
		mitigation, _ := r["mitigation"].(string)
		spends, _ := r["spends"].(string)
		placed := map[string]bool{}
		place := func(side, impact, on string, implied bool) {
			s := sides[side]
			if s == nil || placed[side] {
				return
			}
			placed[side] = true
			sr := SideRisk{ID: id, Type: typ, Impact: impact, Likelihood: lik, On: on, Response: response, Spends: spends, Implied: implied}
			sr.Weight = weightOf(lik) * weightOf(impact)
			if sr.Weight == 0 {
				s.Unweighed++
			}
			if response == "" && mitigation == "" && typ != "constraint" {
				s.Unanswered++
			}
			s.Exposure += sr.Weight
			total += sr.Weight
			s.Risks = append(s.Risks, sr)
		}
		for _, a := range listOf(r["affects"]) {
			side, _ := a["constraint"].(string)
			impact, _ := a["impact"].(string)
			on, _ := a["on"].(string)
			place(side, impact, on, false)
		}
		if mid := impliedOn[id]; mid != "" {
			impact, _ := r["impact"].(string)
			place("schedule", impact, mid, true)
		}
		if len(placed) == 0 && (typ == "risk" || typ == "issue") {
			out.Unplaced = append(out.Unplaced, id)
		}
	}

	best, tied := "", false
	for _, s := range Sides {
		side := sides[s]
		if total > 0 {
			side.Share = round3(float64(side.Exposure) / float64(total))
		}
		sort.SliceStable(side.Risks, func(i, j int) bool { return side.Risks[i].Weight > side.Risks[j].Weight })
		switch {
		case side.Exposure == 0:
		case best == "" || side.Exposure > sides[best].Exposure:
			best, tied = s, false
		case side.Exposure == sides[best].Exposure:
			tied = true
		}
		out.Sides = append(out.Sides, *side)
	}
	if !tied {
		out.MostConstrained = best
	}
	return out
}

// Constraints is a project's triangle as it stands, the working copy
// where there is one.
func (e *Engine) Constraints(ctx context.Context, id string) (Constraints, error) {
	doc, err := e.loadProjectDoc(ctx, id)
	if err != nil {
		return Constraints{}, err
	}
	return ConstraintsOf(specOf(doc)), nil
}

func weightOf(level string) int {
	switch level {
	case "low":
		return 1
	case "medium":
		return 2
	case "high":
		return 3
	}
	return 0
}

func anyList(v any) []any {
	l, _ := v.([]any)
	return l
}

func round3(x float64) float64 {
	return float64(int(x*1000+0.5)) / 1000
}

// addConstraintChecks holds the risks to the stances the project took on
// scope, schedule and cost (D60). They advise; none blocks.
func addConstraintChecks(c checkAdder, spec map[string]any) {
	tri := ConstraintsOf(spec)
	stance := map[string]string{}
	held, stated := 0, 0
	for _, s := range tri.Sides {
		stance[s.Constraint] = s.Stance
		if s.Stance != "" {
			stated++
		}
		if s.Stance == StanceHold {
			held++
		}
	}
	if stated == len(Sides) {
		c.add("constraints-stated", "scope", phaseInitiation, checkOK, "Scope, schedule and cost each have a stance.")
	} else {
		c.add("constraints-stated", "scope", phaseInitiation, checkWarn,
			fmt.Sprintf("%d of 3 sides have no stance: say which of scope, schedule and cost you hold, adjust or concede.", len(Sides)-stated))
	}
	if held == len(Sides) {
		c.add("constraints-all-held", "scope", phaseInitiation, checkWarn,
			"Scope, schedule and cost are all held: when something has to give, nothing can. Say which one moves first.")
	} else if stated == len(Sides) {
		c.add("constraints-all-held", "scope", phaseInitiation, checkOK, "At least one side can give.")
	}

	risks := listOf(spec["risks"])
	weighable := 0
	for _, r := range risks {
		if t, _ := r["type"].(string); t == "risk" || t == "issue" {
			weighable++
		}
	}
	if weighable > 0 {
		if n := len(tri.Unplaced); n == 0 {
			c.add("risks-constrained", "risks", phaseInitiation, checkOK, "Every risk and issue names the side it would move.")
		} else {
			c.add("risks-constrained", "risks", phaseInitiation, checkWarn,
				fmt.Sprintf("%d of %d risks and issues name no side: say whether each would move scope, schedule or cost.", n, weighable))
		}
	}

	// A risk on a side the project holds needs more than accepting, and a
	// response should not spend a side the project holds.
	acceptedHeld, spendHeld := map[string]bool{}, map[string]bool{}
	for _, s := range tri.Sides {
		if s.Stance != StanceHold {
			continue
		}
		for _, r := range s.Risks {
			if r.Response == StanceAcceptResponse {
				acceptedHeld[r.ID] = true
			}
		}
	}
	for _, r := range risks {
		id, _ := r["id"].(string)
		if sp, _ := r["spends"].(string); sp != "" && stance[sp] == StanceHold {
			spendHeld[id] = true
		}
	}
	if held > 0 && len(risks) > 0 {
		if n := len(acceptedHeld); n == 0 {
			c.add("risks-held-accepted", "risks", phaseInitiation, checkOK, "No risk on a held side is simply accepted.")
		} else {
			c.add("risks-held-accepted", "risks", phaseInitiation, checkWarn,
				fmt.Sprintf("%d risk%s on a side you hold %s only accepted: avoid, mitigate or transfer, or adjust the stance.", n, plural(n), isAre(n)))
		}
		if n := len(spendHeld); n == 0 {
			c.add("risks-spend-held", "risks", phaseInitiation, checkOK, "No response spends a side you hold.")
		} else {
			c.add("risks-spend-held", "risks", phaseInitiation, checkWarn,
				fmt.Sprintf("%d response%s spend%s a side you hold: spend one you adjust or concede.", n, pluralES(n), singularS(n)))
		}
	}
}

// StanceAcceptResponse is the response that carries a risk as it is.
const StanceAcceptResponse = "accept"

func pluralES(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func singularS(n int) string {
	if n == 1 {
		return "s"
	}
	return ""
}
