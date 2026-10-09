# Evaluating what agents use

Agents use Cartograph through its MCP server. A change to anything they
touch (a tool, its answers, the instructions, the porting chain, a rule
the engine enforces) is judged the way Lean Six Sigma judges a process:
by defined criteria, measured from the outcome and the trace, never from
what the agent says it did. This is DMAIC, run on our own tools.

## Define

Before changing anything, write down what done means, as checks a
script can run against the server.

- Every criterion is read from the server: the change set, its records,
  its status, the trace. An agent's report is not evidence; agents
  report work they did not do.
- Criteria follow the contract and the porting map
  (`internal/mcp/porting.go`), not one run's habits. When a run is
  right and the criterion wrong (a component's own deliverable moved
  into the component, a completed milestone not ported), fix the
  criterion and say so.
- Name the bar: how many fresh runs in a row must pass, and on which
  model. Three in a row is the default.
- The machinery is in the repository (`cartograph eval`, the `just
  eval-*` recipes); a document's criteria file, the document and the
  runs are kept outside it, beside each other. A real organisation's
  document never enters the repository (`just words`).

A criteria file is JSON: what is judged, the agent's name, the bar, and
the checks (`wholeDocument`, `proposed`, `oneObjectiveEach`,
`noPeople`, `structure` with the component and operation patterns,
`rounds` for a run with a person present (docs/adr/0032),
`totals` across projects, and the `kpis` that must and must not exist).
`internal/evaluate` defines it.

## Measure

Everything runs in the test environment (`docs/CONTAINERS.md`), the
agent's calls included: each recipe below is run as `scripts/dev just
eval-...`, the evaluation directory and its runs live in the
environment's copy of the workspace, and the prompt `eval-serve` prints
tells the agent to prefix every command with `scripts/dev`.

| Do | Run |
|---|---|
| Freeze the build under test, the flake built at a commit | `just eval-build <dir> [rev]` |
| Serve a fresh traced run; print the agent's prompt (`person=1`: a person answers between its turns) | `just eval-serve <dir> <document> [agent] [person]` |
| Score a run from the server and its trace | `just eval-score <dir> <run> <criteria> <change set>` |
| Every run, its figures, and the streak | `just eval-status <dir> [criteria]` |
| Stop a run's server, or every run's | `just eval-stop <dir> [run]` |

- Each run is a fresh vault and trace of its own, served from the frozen
  build's Nix store path; never reuse one.
- Give the agent the minimal prompt `eval-serve` prints: the document,
  the server, the goal, and `cartograph eval call` from the same store
  path to reach it. What it needs to know comes from the server's
  instructions and answers, or the server is what needs fixing.
- The scorer reads through the same server as a client named `Scorer`,
  and the trace figures are the agent's calls only
  (`cartograph traces -agent <name>` reads them the same way).
- With a person present (`person=1`), the agent ends its turn with its
  questions and the evaluator answers as the person, by a policy
  written beside the criteria before the first run and kept word for
  word across runs: what to answer to each kind of question, and what
  to say when it is a figure the person does not have yet.
- Score every run, every criterion, pass or fail.

## Analyse

Read the trace as a value stream, and the runs as a process.

- **Defects:** refused and failed calls, by tool and kind; the fields
  most refused say which answer or shape misleads.
- **Waste:** a record read again unchanged, a write undone
  (`discard_draft`), a chain of reads one record at a time, a call
  repeated after a refusal. `cartograph traces` counts each.
- **Variance:** calls per run, sigma, and the structure produced, across
  runs and across models. A result that changes with the run is a rule
  the engine does not hold yet.
- Find the root cause in the engine or the MCP adapter, not in the
  agent: an answer that invites the wrong call, a rule left to the
  writer, a defect the server drafts itself.

## Improve

- Fix the cause deterministically, in the engine where it is a rule of
  the domain (docs/adr/0027, 0029): a shape refused when it is written,
  an id generated, a register read as the porting map says. A sentence
  added to the instructions is the weakest fix; use it only for what
  no rule can hold.
- Every defect found gets a test that fails without the fix.
- `just ci`, `just commit-check`, and the change pushed, before the next
  run.

## Control

- Freeze the build under test: the flake built at one commit, a Nix
  store path, for every run that counts. A change to the code is a new
  build, and resets the count (`just eval-status` counts on the latest
  run's build).
- Run the counted runs one at a time, each scored before the next
  starts. Never several agents at once without the person's leave: it
  spends tokens for nothing a sequence would not show.
- The feature is closed when the bar is met on one frozen build, and
  the result, with the trace figures (calls, defects per million, sigma,
  first pass yield, value added), goes in the pull request.
- Work found while the build is frozen waits until the bar is met, then
  goes in as its own change.
