# 0029. No version brings in a shape the strict profile refuses

**Status:** Accepted. Extends [0027](0027-agents-draft-to-a-strict-profile.md).

## Context

0027 held an agent's drafts to the strict profile: a second objective
on a project, a person's name on a role, refused when written. A person
was held only by the checks: `goals-objective` blocks a project with
two objectives, and a proposal may waive any open check with a reason.
And an agent's write outside a change set draft (a proposal of a whole
manifest) never met the profile at all.

So whether a project had one objective rested on the writer: a person
choosing not to waive, a model choosing not to send a second. A model's
answers vary from run to run; the engine's must not.

Refusing the profile's shapes in every stored manifest would stop a
deployed vault that already holds a project with two objectives from
importing or saving it: the narrowed type VERSIONING.md calls a major
change.

## Decision

**No version brings in a shape the strict profile refuses, whoever
writes it.** Every version save, every proposal of a manifest and every
change set proposed is refused, with the problem on its field, when the
version has a strict-profile problem its current version does not. No
waiver reaches it: it is a refusal, not a check.

**What is stored stays readable, and may only shed.** A project stored
with two objectives before this rule imports, loads and saves as it is;
it may drop to one, never gain a third (a list over its limit may
shrink, never grow). A backup restores as it was taken.

**An agent's drafts keep 0027's rule**: held to the whole profile when
written, so they are never built wrong in the first place.

## Consequences

- One objective per project, and no person where a role goes, hold for
  every writer, person or model, as a property of the engine.
- The schemas are unchanged, so no deployed version breaks; the rule is
  a refusal at save, as `goals-objective` was a block at handoff.
- A new shape the profile refuses is added to the profile once and is
  then refused everywhere at once.
