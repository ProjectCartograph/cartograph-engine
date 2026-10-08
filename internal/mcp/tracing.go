package mcp

import (
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/engine"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/identity"
	"github.com/ProjectCartograph/cartograph-engine/v2/internal/trace"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Tracing each call by its shape (docs/adr/0028).

// record traces one call by its shape (docs/adr/0028): what was called,
// on what, by whom, with which keys, how big, how long and how it ended;
// never what the arguments or the answer said.
func (o Options) record(name string, req *sdk.CallToolRequest, c call, in, out any, err error, start time.Time) {
	if o.Trace == nil {
		return
	}
	tc := trace.Call{At: start.UTC(), Method: "tools/call", Operation: "execute_tool", Tool: name,
		Seconds: time.Since(start).Seconds(), Outcome: trace.OK, Agent: c.who.Agent, Person: c.who.Subject}
	var args map[string]any
	if b, mErr := json.Marshal(in); mErr == nil {
		tc.InputBytes = len(b)
		_ = json.Unmarshal(b, &args)
	}
	for k := range args {
		tc.Keys = append(tc.Keys, k)
	}
	sort.Strings(tc.Keys)
	kind, _ := args["kind"].(string)
	id, _ := args["id"].(string)
	if kind != "" && id != "" {
		tc.Record = kind + "/" + id
	}
	tc.ChangeSet, _ = args["changeSet"].(string)
	if req != nil && req.Extra != nil && req.Extra.Header != nil {
		tc.Session = req.Extra.Header.Get("Mcp-Session-Id")
		tc.Protocol = req.Extra.Header.Get("Mcp-Protocol-Version")
	}
	if tc.Session == "" {
		tc.Session = tc.Agent + "|" + tc.Person
	}
	if out != nil {
		if b, mErr := json.Marshal(out); mErr == nil {
			tc.OutputBytes = len(b)
		}
		// An answer that lists problems, and nothing else done, is a
		// refusal in all but name: structure and start_work answer so. A
		// settle that kept some fields and refused others is a partial
		// defect, its refused fields traced.
		if m, ok := out.(map[string]any); ok {
			if ps, ok := m["problems"].([]string); ok && len(ps) > 0 {
				tc.Outcome, tc.Defect, tc.Problems = trace.Refused, "structure", len(ps)
			}
			if ps, ok := m["refused"].([]engine.Problem); ok && len(ps) > 0 {
				tc.Outcome, tc.Defect, tc.Problems = trace.Refused, "partial", len(ps)
				tc.Paths = problemPaths(ps)
			}
		}
	}
	if err != nil {
		tc.Outcome, tc.ErrorType, tc.Defect = trace.Refused, "tool_error", defectOf(err)
		tc.ErrorMessage = maskQuoted(err.Error())
		var invalid *engine.ValidationError
		if errors.As(err, &invalid) {
			tc.Problems = len(invalid.Problems)
			tc.Paths = problemPaths(invalid.Problems)
		}
		if tc.Defect == "other" {
			tc.Outcome = trace.Failed
		}
	}
	o.Trace.Record(tc)
}

// defectOf classifies a refusal, low in cardinality, for the analysis.
func defectOf(err error) string {
	var invalid *engine.ValidationError
	var open *engine.OpenChecksError
	switch {
	case errors.As(err, &invalid):
		return "schema"
	case errors.As(err, &open):
		return "open-checks"
	case errors.Is(err, engine.ErrNotFound), errors.Is(err, engine.ErrUnknownKind):
		return "not-found"
	case errors.Is(err, identity.ErrForbidden):
		return "access"
	case errors.Is(err, engine.ErrBadEdit):
		return "input"
	case errors.Is(err, engine.ErrConflict):
		return "conflict"
	}
	// An argument the server could not read, or one it asked to be sent
	// otherwise, is the agent's input.
	if msg := err.Error(); strings.Contains(msg, "settle does not take") || strings.HasPrefix(msg, "give ") || strings.HasPrefix(msg, "name ") || strings.HasPrefix(msg, "pass ") {
		return "input"
	}
	return "other"
}

// problemPaths are the fields problems name, list items as "-", once
// each: where a defect is, never what was sent there.
func problemPaths(ps []engine.Problem) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range ps {
		var segs []string
		for _, t := range strings.Split(strings.Trim(p.Path, "/"), "/") {
			if _, num := strconv.Atoi(t); num == nil {
				t = "-"
			}
			segs = append(segs, t)
		}
		if path := "/" + strings.Join(segs, "/"); !seen[path] {
			seen[path] = true
			out = append(out, path)
		}
	}
	sort.Strings(out)
	return out
}

// maskQuoted is a message with every quoted value replaced by an
// ellipsis and the whole cut to 300 characters, so a trace says why a
// call was refused without keeping what the agent sent.
func maskQuoted(msg string) string {
	msg = quotedValue.ReplaceAllString(msg, "${1}…${1}")
	if r := []rune(msg); len(r) > 300 {
		msg = string(r[:300]) + "…"
	}
	return msg
}

var quotedValue = regexp.MustCompile(`(['"])[^'"]{4,}['"]`)
