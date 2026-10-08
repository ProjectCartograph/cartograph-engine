// Package trace is the port through which every call an agent makes to
// Cartograph's MCP server is recorded (docs/adr/0028), for governance and
// for finding where agents work well and where they vary: which tools are
// used most, which are refused, where work is redone. A call is recorded
// by its shape (the tool, the record it names, the keys it sent, its
// sizes, its outcome and how long it took), never by what it carried, so
// no document an agent ports and nothing a person wrote reaches a trace.
// An adapter keeps the calls; "off", the default, keeps none.
package trace

import "time"

// Outcomes of a call.
const (
	OK      = "ok"
	Refused = "refused" // the server said no and why: a defect the agent can fix
	Failed  = "failed"  // the server could not answer
)

// Call is one tool call. Its fields carry the names OpenTelemetry's
// semantic conventions give an MCP server span (mcp.*, gen_ai.*,
// error.type), so a collector reading the lines needs no mapping;
// Cartograph's own are under cartograph.*. Arguments and results, opt-in
// and sensitive under those conventions, are never recorded: only the
// argument keys.
type Call struct {
	At time.Time `json:"time"`
	// Method is always tools/call; Operation always execute_tool.
	Method    string `json:"mcp.method.name"`
	Operation string `json:"gen_ai.operation.name"`
	Tool      string `json:"gen_ai.tool.name"`
	RequestID string `json:"jsonrpc.request.id,omitempty"`
	Protocol  string `json:"mcp.protocol.version,omitempty"`
	// Session groups the calls of one piece of an agent's work: the
	// client's MCP session where it has one, else its agent and person.
	Session string `json:"mcp.session.id"`
	// Seconds is the server's time on the call.
	Seconds float64 `json:"mcp.server.operation.duration"`
	// ErrorType is tool_error for a call answered with isError, empty on
	// success.
	ErrorType string `json:"error.type,omitempty"`
	// ErrorMessage is what a refusal said, with every quoted value
	// masked and cut short: why a call was refused, never what was sent.
	ErrorMessage string `json:"error.message,omitempty"`
	Agent        string `json:"cartograph.agent,omitempty"`
	Person       string `json:"cartograph.person,omitempty"`
	ChangeSet    string `json:"cartograph.change_set,omitempty"`
	// Record is the Kind/id the call names, when it names one.
	Record string `json:"cartograph.record,omitempty"`
	// Keys are the argument's top-level keys, sorted; never their values.
	Keys        []string `json:"cartograph.argument.keys,omitempty"`
	InputBytes  int      `json:"cartograph.argument.bytes"`
	OutputBytes int      `json:"cartograph.result.bytes"`
	Outcome     string   `json:"cartograph.outcome"`
	// Defect classifies a refusal or failure, low in cardinality: schema,
	// structure, open-checks, not-found, conflict, input, access, other.
	Defect string `json:"cartograph.defect,omitempty"`
	// Problems counts what a refusal, or an answer, says is wrong, and
	// Paths are the fields it names, list items as "-": where the defect
	// is, never what was sent there.
	Problems int      `json:"cartograph.problems,omitempty"`
	Paths    []string `json:"cartograph.problem.paths,omitempty"`
}

// Recorder keeps calls. Record never fails the call it records.
type Recorder interface {
	Record(c Call)
}

// Off records nothing.
type Off struct{}

// Record does nothing.
func (Off) Record(Call) {}
