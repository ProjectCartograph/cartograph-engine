# About checks

Cartograph tells you when something is missing or doubtful. It almost never stops you. This page explains why. The examples come from the produce cooperative in Cartograph's example.

## What a check is

A check is a mark beside a field and a list with a **Fix** link that takes you to the field. It says what is missing, wrong or worth a look. It reads the records as they are now.

Checks come in three weights.

- **Fix before merging.** The record cannot be saved as it stands. A gap that names an outcome which does not exist is an example: the link points at nothing.
- **Needed before hand-off.** A field is not filled in yet. A project with no problem written is an example. Only a project's hand-off waits on these.
- **Suggestions.** Advice, and nothing waits on it. A programme that no project names yet is an example: it coordinates nothing, and the check says so.

## Why checks advise

There are three reasons.

**Work happens in the wrong order.** A person may know a project's sponsor before they know its budget. A form that refuses to save until everything is complete makes them invent answers. A wrong answer entered to get past a form is worse than a blank, because it looks finished.

**Other people's work changes yours.** Most of what is worth checking about a programme is found in other records: the projects that name it and the goals it aligns to. A programme that saved yesterday should not be refused today because someone edited another record. For that reason a programme's checks cannot block at all. The same holds for goals, operations, portfolios and gaps. A project's checks can block, because most of them read only the project ([TAXONOMY.md](../../TAXONOMY.md), decision D6).

**A list nobody can clear gets ignored.** If a rule can never be met, people learn to skip the whole list. Cartograph aims for checks a person can satisfy.

So the line is this. A save is never refused for being incomplete. A version is refused only when it is malformed: a value of the wrong form, or a reference to nothing. A project's hand-off is refused while a blocking check stands, because that is the moment someone else relies on the record being whole ([ADR 0035](../../adr/0035-a-version-is-well-formed-complete-is-a-check.md)). An organisation can tighten the rules for merging through change control: see [About change sets](about-change-sets.md).

## Checks above the project

A programme's checks read the work that names it. They say when it has no
components, when a component serves none of its outcomes, and when it has
no theory of change. The last one adds that, without a theory of change,
it may be a portfolio. A portfolio has three: whether it names a strategic
objective, whether anything names it, and whether everything it holds has
a decision. None of them can stop a save. For the reasoning about what a
programme is and is not, see [About programmes, portfolios and
projects](about-programmes-portfolios-and-projects.md).

## What it means for you

A check that bothers you does not stop you saving. A suggestion you disagree with can stay open, and it will not stop a merge unless your administrator has said every check must be met.

## Related

- Explanation: [About the decision model](about-the-decision-model.md), [About change sets](about-change-sets.md)
