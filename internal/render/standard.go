package render

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
)

// The parts a standard charter has that are not one field of the
// definition: the key facts at the top, the milestone schedule, the budget
// with its total, the stakeholders, and the sign-off table (PMI project
// charter, PRINCE2 PID, MSP programme brief, ITIL service description).
// Shared by every kind so the three documents look like one family.

// facts prints the key facts box under the title.
func (d *doc) facts(fs ...field) {
	var kept []field
	for _, f := range fs {
		if strings.TrimSpace(f.value) != "" {
			kept = append(kept, f)
		}
	}
	if len(kept) == 0 {
		return
	}
	d.flush()
	d.b.WriteString("<dl class=\"facts\">\n")
	for _, f := range kept {
		d.b.WriteString("<div><dt>")
		d.b.WriteString(esc(f.label))
		d.b.WriteString("</dt><dd>")
		d.b.WriteString(esc(f.value))
		d.b.WriteString("</dd></div>\n")
	}
	d.b.WriteString("</dl>\n")
}

// milestones turns a start month and phase lengths into dated rows, and
// returns the finish month too.
func milestones(start string, phases []map[string]any) (rows [][]string, finish string) {
	t, err := time.Parse("2006-01", start)
	for _, ph := range phases {
		months := 0
		fmt.Sscanf(number(ph["months"]), "%d", &months)
		from, to := "", ""
		if err == nil && months > 0 {
			from = t.Format("Jan 2006")
			end := t.AddDate(0, months-1, 0)
			to = end.Format("Jan 2006")
			finish = end.Format("January 2006")
			t = t.AddDate(0, months, 0)
		}
		rows = append(rows, []string{str(ph["name"]), from, to, number(ph["months"])})
	}
	return rows, finish
}

// budget returns the funding rows with a total per currency.
func budget(n names, funding []map[string]any) (rows [][]string, total string) {
	sums := map[string]float64{}
	for _, f := range funding {
		amount := number(f["amount"])
		cur := str(f["currency"])
		if a, ok := f["amount"].(float64); ok {
			sums[cur] += a
		} else if a, ok := f["amount"].(int); ok {
			sums[cur] += float64(a)
		}
		shown := money(amount)
		if cur != "" && shown != "" {
			shown = cur + " " + shown
		}
		rows = append(rows, []string{shown, n.of("FundingSource", str(f["source"])), chip("status", str(f["status"]))})
	}
	curs := make([]string, 0, len(sums))
	for c := range sums {
		curs = append(curs, c)
	}
	sort.Strings(curs)
	var parts []string
	for _, c := range curs {
		parts = append(parts, strings.TrimSpace(c+" "+money(fmt.Sprintf("%g", sums[c]))))
	}
	return rows, strings.Join(parts, "; ")
}

// money groups thousands: 1500000 reads 1,500,000.
func money(s string) string {
	if s == "" {
		return ""
	}
	whole, frac, _ := strings.Cut(s, ".")
	neg := strings.HasPrefix(whole, "-")
	whole = strings.TrimPrefix(whole, "-")
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if frac != "" {
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}

// stakeholders reads the StakeholderMap scoped to this piece of work.
func stakeholders(ctx context.Context, e *engine.Engine, n names, kind, id string) [][]string {
	var rows [][]string
	each(ctx, e, "StakeholderMap", func(_, _ string, spec map[string]any) {
		scope := obj(spec["scope"])
		if str(scope["kind"]) != kind || str(scope["id"]) != id {
			return
		}
		for _, entry := range list(spec["entries"]) {
			who := n.of("Resource", str(entry["resource"]))
			if g := str(entry["group"]); g != "" {
				who = n.of("BeneficiaryGroup", g)
			}
			rows = append(rows, []string{
				who,
				str(entry["stake"]),
				chip("level", number(entry["influence"])),
				chip("level", number(entry["interest"])),
				chip("approach", approach(entry["influence"], entry["interest"])),
				chip("priority", str(entry["tier"])),
				n.ref(entry["owner"], "Resource", spec),
			})
		}
	})
	return rows
}

func (d *doc) stakeholders(rows [][]string) {
	if len(rows) == 0 {
		return
	}
	d.h2("Stakeholders")
	d.table([]string{"Stakeholder", "Stake", "Influence", "Interest", "Approach", "Type", "Relationship owner"}, rows)
}

// approach reads Mendelow's power and interest grid: high on both is
// managed closely, high influence alone kept satisfied, high interest alone
// kept informed, and neither monitored. High is the top of Cartograph's three
// levels. Unscored stakeholders get no approach.
func approach(influence, interest any) string {
	i, e := number(influence), number(interest)
	if i == "" || e == "" {
		return ""
	}
	switch hi, he := i == "3", e == "3"; {
	case hi && he:
		return "manageClosely"
	case hi:
		return "keepSatisfied"
	case he:
		return "keepInformed"
	default:
		return "monitor"
	}
}

// measures prints KPIs with their starting point and target, read from
// the KPI register.
func measures(ctx context.Context, e *engine.Engine, n names, ids []string) [][]string {
	want := map[string]bool{}
	for _, id := range ids {
		want[id] = true
	}
	byID := map[string][]string{}
	each(ctx, e, "KPI", func(id, name string, spec map[string]any) {
		if !want[id] {
			return
		}
		unit := n.of("Unit", str(spec["unit"]))
		point := func(v any) string {
			p := obj(v)
			val := number(p["value"])
			if val == "" {
				return ""
			}
			if unit != "" {
				val += " " + strings.ToLower(unit)
			}
			if m := month(str(p["date"])); m != "" {
				val += ", " + m
			}
			return val
		}
		byID[id] = []string{name, point(spec["baseline"]), point(spec["target"]),
			sourcesOf(n, spec), n.of("ReportingCycle", str(spec["cycle"]))}
	})
	var rows [][]string
	for _, id := range ids {
		if r, ok := byID[id]; ok {
			rows = append(rows, r)
		}
	}
	return rows
}

// signOff prints the approval table a charter is signed on. Cartograph names
// roles, never people, so the name, signature and date are left to be
// filled in on the printed page.
func (d *doc) signOff(roles []string) {
	if len(roles) == 0 {
		return
	}
	d.h2("Approval")
	d.flush()
	d.b.WriteString("<table class=\"signoff\">\n<tr><th>Role</th><th>Name</th><th>Signature</th><th>Date</th></tr>\n")
	for _, r := range roles {
		d.b.WriteString("<tr><td>")
		d.b.WriteString(esc(r))
		d.b.WriteString("</td><td></td><td></td><td></td></tr>\n")
	}
	d.b.WriteString("</table>\n")
}

// roleNames lists the roles of a kind that hold a given role, by name.
func roleNames(n names, resources []map[string]any, role string) []string {
	var out []string
	for _, r := range resources {
		if str(r["role"]) == role {
			out = append(out, n.of("Resource", str(r["resource"])))
		}
	}
	return out
}

// sourcesOf names every data source an indicator is read from, and the one
// a manifest from before 2.7.0 names.
func sourcesOf(n names, spec map[string]any) string {
	var out []string
	if ids, ok := spec["sources"].([]any); ok {
		for _, id := range ids {
			if s := str(id); s != "" {
				out = append(out, n.of("DataSource", s))
			}
		}
	}
	if s := str(spec["source"]); s != "" && len(out) == 0 {
		out = append(out, n.of("DataSource", s))
	}
	return strings.Join(out, ", ")
}
