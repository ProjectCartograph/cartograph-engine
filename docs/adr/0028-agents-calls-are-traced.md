# 0028. Every agent's call is traced by its shape

**Status:** Accepted. Extends [0016](0016-agents-read-and-propose-people-decide.md).

## Context

MCP itself defines no audit log. Governance guidance for MCP servers
(the Cloud Security Alliance's agentic MCP practices among them) asks
for every tool call to be logged with the agent and person behind it,
the tool, its inputs with sensitive values left out, the time, the
outcome and the duration, kept append-only and apart from the server.
OpenTelemetry's semantic conventions name these for an MCP server span:
`mcp.method.name` (`tools/call`), `gen_ai.tool.name`,
`gen_ai.operation.name` (`execute_tool`), `mcp.session.id`,
`error.type` (`tool_error` for a call answered with an error) and
`mcp.server.operation.duration`, with a tool's arguments and results
opt-in and marked sensitive.

Cartograph also needs to know where agents work well and where they
vary: which tools they use most, which refuse them, where they redo
work. Small agents porting documents were the case in point.

## Decision

**A trace port.** `internal/trace` defines a call by its shape (the
tool, the record it names, the argument keys, sizes, outcome, a low
cardinality defect class, the problems counted, the duration, the
agent, the person, the session and the change set) and a `Recorder`.
Every MCP tool call is recorded, refused or not. The fields carry
OpenTelemetry's names, Cartograph's own under `cartograph.*`. An
argument's value and a result are never recorded: a document an agent
ports, and anything a person wrote, never reach a trace.

**Off unless asked.** `CARTOGRAPH_MCP_TRACE` is `off` or a file the
`jsonl` adapter appends to, one call a line, which an OpenTelemetry
collector's file log receiver, or any JSON lines tool, reads as it is.
The operator keeps it on a volume apart from the server, for as long as
their audit policy says.

**Analysed as a process.** `cartograph traces` reads traces as Lean Six
Sigma reads a process: each call an opportunity and each refusal or
failure a defect, so each tool and the whole has its defects per million
and sigma level; first pass yield, and rolled throughput yield over the
write steps; rework (a step tried again after a defect) and the attempts
it took; the value stream (writes kept, reads needed, waste: defects and
reads repeated as they were); how much sessions vary in length; and the
steps most taken from one tool to the next.

## Consequences

- An operator can audit what each agent did, for whom, and when, without
  the trace holding what it read or wrote.
- A tool with a low first pass yield, or a step reworked often, is where
  the server should guide agents better: the analysis shows where to
  improve, and whether a change did.
- Another recorder (an OpenTelemetry exporter, a database) is another
  adapter.
