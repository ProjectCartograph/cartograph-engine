# 0030. Meaning in text is judged by a System-1 model, or not at all

**Status:** Accepted. Extends [0023](0023-a-decision-model-behind-a-port.md).

## Context

Cartograph's key operations read unstructured text: a charter brought in
to be ported, an objective a person types, a register's cells, a note on
a role. Where the engine needed to know what such text means, it used
patterns and word lists: a digit in an objective made it a target, a
title before a name made it a person, English words in a heading said
which part of a charter a section is. Each is wrong in a way no rule
can mend: "every 2-year-old reaches the milestone" names a group, not a
target; a name with no title is still a person; the same charter in
another language reads as nothing. And a heuristic that is wrong is
wrong the same way every time, which looks like determinism and is not
correctness.

0023 put a decision model behind a port, `internal/decide`, and used it
only for advisory checks, every other judgement left to a heuristic
when no model was set. Laya, the model it reaches, is a System-1 model:
it answers typed questions about a text (a choice, a yes or no) in one
forward pass with calibrated probabilities, the same answer for the
same input, in English or, with its multilingual checkpoint, in a
hundred languages. Jev, TypeSafe's model, answers the same questions in
the same shape, as a cloud service.

## Decision

**Every judgement of meaning in text is a question to the decision
model.** Whether an objective is an aim or a target, whether a text
names a person, which part of a charter a section is, what a register's
column and cell say, whether two names mean the same thing: each is a
named question the engine asks through `internal/decide`. A pattern
stays only where the text is structural and the pattern controls it:
dates and figures written in digits, codes, numbering, a table's layout.

**The engine owns the questions.** Each is a typed question with its
instructions and options, kept with examples whose answer is known; a
question's wording is kept only once it is measured against those
examples on the pinned model (`just decide-measure`), as 0023 required
of its contrasts. The gate tests the engine's use of the answers with a
scripted fake decider; the measurement runs locally, never in CI.

**A model's answer advises; only an exact rule refuses.** Measured on
the pinned English checkpoint, Laya tells an aim from a target in 24 to
25 of 28 known examples, and finds a person in a text in 16 to 17 of
20, missing names a pattern catches without fail. That is far better
than a pattern where a pattern cannot see (a group named by a number,
a name without a title, another language), and not good enough to
refuse a save. So a model's answer is an advisory check that shows how
sure it is, and an agent is asked to confirm or mend it; a refusal
stays only where a rule is exact (a person named with a title, a code,
a date written in digits). The digit rule that refused an objective
with a number is not exact, and goes: the model's question takes its
place, as an advisory check.

**Pure packages declare what they need, and the engine answers.**
`internal/document` reads a document's layout; where it needs to know
what a heading or a cell means it asks through an interface it
declares, which the engine implements with the decider. The domain
stays free of the port.

**Without a model, a check that needs meaning is off, and says so.**
There is no heuristic behind it. The check is not asked, the engine
reports which checks are off (`decision_model`, the checks' own
report), and the MCP server's instructions tell an agent that it must
judge those things itself. A port read without a model leans on the
agent more, and varies with it more; that is the cost of turning the
model off.

**The model is on by default.** `CARTOGRAPH_DECIDE` defaults to `laya`,
reaching the sidecar the flake builds, its model pinned to one commit
of its repository and served from the Nix store. `off` is a choice a
deployment makes, not the default. Jev is a second adapter behind the
same port, with the caveat that a cloud service's answers may change
when its model does.

## Consequences

- The checks that read meaning work in any language Laya's checkpoint
  reads, and stop misjudging groups, names and phrasing a pattern
  cannot see.
- A deployment without the sidecar loses those checks, visibly, and an
  agent working against it carries more of the judgement.
- Every new judgement of text is a question with measured examples,
  never a new pattern.
- Laya must run wherever Cartograph is deployed, arm64 as well as
  amd64 (#10).
