# 0017. One bar for people and agents, guided by the contract

**Status:** Accepted

## Context

Cartograph is worth using over an assistant on its own because what it
records is held to the discipline and to the organisation's own record:
the same checks, the same links, the same words, whoever writes it. An
agent asked to help define an objective (ADR 0016) showed where that did
not yet hold:

- It proposed an outcome written as an action, with no key results,
  owner or horizon, and nothing stopped it. The checks it missed were
  advice the engine gave a person in the editor; an agent was never
  asked to meet them.
- It could not check new work: the checks read saved versions only.
- Some checks a person saw existed only in the interface, as TypeScript
  over English word lists ("starts with an action", "describes a
  state"). No agent could see them, they meant nothing in another
  language, and a sentence passed them by avoiding a listed word.
- It walked one form. Nothing told it that an outcome closes a gap,
  that a gap is measured by a KPI, or that the cooperative already had
  an objective saying the same thing.
- Its proposal could not be opened before it was accepted.

## Decision

**One set of checks, run by the engine on any text.** Every kind's
checks (goal, project, programme, operation, gap) run on a draft or a
proposal as on a saved version (`ChecksOf`). The interface shows the
same checks. The rules the interface held alone were sorted: those a
machine decides honestly in any language became engine checks (a KPI
named twice, a risk not weighed, a programme member serving none of its
outcomes, a programme's change, pathway and lead team); the rest are
judgement.

**An agent proposes only what meets them, or says why not.** A proposal
whose checks are open is refused, listing each, unless the agent names
the check with a reason it is left open; the reasons go on the proposal
and its person reads them first. A person's own save is unchanged: a
check never blocks it.

**Judgement is guidance, never a mark.** Whether an outcome is a state,
an aim says one thing, a statement is specific: no word list decides
these, in English or any other language, and a mark that can be passed
by avoiding a word teaches the wrong thing. They are said in words,
with right and wrong examples, to people and agents alike. Words an
editor offers (the verbs an aim opens with) are a per-language
vocabulary, offered and never used to judge.

**The guidance is the contract's.** Every kind has a flow (its steps,
fields, checks, and the links held on other kinds that name it) and,
per language, a guidance bundle (`contract/guidance/<locale>/`): what
the kind is in the discipline's words, each field with right and wrong
examples drawn from the fictional cooperative, what to ask for each
link and what to define first when there is none, how to meet each
check. Contract tests hold every flow to every settable field, every
named check to one the engine reports, and every link to its words. A
language is added as a bundle; no rule depends on one.

**A guide joins the contract to the organisation.** `guide` (MCP) and
`GET /guides/{kind}` give, for a kind and level, the flow with its
words, a template to fill in, the records of that kind already defined
(to reuse before defining another), and for each reference and link
the organisation's own records to choose from. Agents read it before
drafting; the interface shows the same words behind each field's help.

**The strategy is linked top-down, and the links are checks.** A goal
has objectives under it, an objective outcomes, each outcome under an
objective closes a gap (`has-objectives`, `has-outcomes`,
`outcomes-close-gaps`, `closes-gap`). They read the rest of a proposal,
so the whole journey can be proposed at once.

**Proposals that reference each other are one proposal.** A KPI, the
gap it measures and the outcome that closes it are validated against
each other, reviewed together, and accepted in one transaction in the
order their references need, or declined together (`propose_set`).

**A proposal is its manifests' draft, and is read before it is
decided.** Proposing writes each manifest's shared draft, so it opens in
the editor where the person and the agent work on it together. Its
review page shows what each part changes, field by field, its checks
and the agent's reasons; accepting is there and nowhere else.

## Options considered

**Rules as data in the contract, with an evaluator in each language.**
One declaration and two evaluators held to shared vectors. Rejected for
now: the mechanical rules are already the engine's, served to every
interface, and the rules that wanted a declarative home were the word
lists, which are guidance.

**Word lists per locale, used for soft hints.** More help in English;
more to maintain, still spoofable, and a hint that is wrong half the
time in a language without a list.

**Instructions only.** Telling agents to meet the checks without the
engine refusing. A guarantee cannot rest on a model's diligence; small
models skipped steps the instructions named.

## Consequences

- Nothing an agent proposes falls below what a person must meet in the
  editor, and what it leaves open is in front of its person.
- The interface's verb pickers take their words from the guide; a
  language without them gets a plain sentence field.
- An agent's quality still depends on the model: one may stop to ask
  where another completes the journey. The engine decides what may be
  proposed; the guide makes the good path the obvious one.
- Check messages are English, as before; translating them is the next
  step for a second language.
