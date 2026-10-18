# Writing for the guide

This page is for anyone adding or changing a page in this guide. The
guide follows [Diátaxis](https://diataxis.fr/): every page does one job
for one kind of reader, and the kind decides how it is written.

## Choose the kind

Ask the two questions of the [compass](https://diataxis.fr/compass/).

1. Does the page help someone act, or help them know something?
2. Is the reader studying, or at work?

| Acting, studying | Acting, at work | Knowing, at work | Knowing, studying |
|---|---|---|---|
| Tutorial | How-to guide | Reference | Explanation |

A tutorial teaches by doing. A how-to guide gets a competent person to a
goal. Reference describes. Explanation gives reasons. If a page needs two
of these, write two pages and link them.

Folders: `tutorials/`, `how-to/`, `reference/`, `explanation/`. File names
are lower case with hyphens. Link with relative links.

## The voice of each kind

Examples use the fictional produce cooperative in
`cartograph-engine/examples/minimal`: depots, grading, members, retail
buyers, and its real record names. Never use a real organisation, or
an example from any real one.

**Tutorial. "We", one route, a result at every step**
([tutorials](https://diataxis.fr/tutorials/)).

- Do: "In this tutorial we start a project and read its charter."
- Do: "Type `Quality Team` and press **Add**. You see *Which team owns
  it?*"
- Don't: "You will learn what a team is." (Presumes what the reader takes
  away.)
- Don't: "You can also choose a programme or a portfolio." (A choice.)
- Don't: a paragraph on why checks only advise. Link to it.

**How-to guide. A goal, the imperative, forks where the work forks**
([how-to guides](https://diataxis.fr/how-to-guides/)).

- Do: a title that says the goal: "Record an indicator's readings".
- Do: "This guide shows you how to record a reading."
- Do: "If the indicator has no reporting cycle, add one first."
- Don't: "How to think about readings." (That is explanation.)
- Don't: "Readings are the values measured over time, and matter
  because..." (Background.)
- Don't: list every field. Link to the reference.

**Reference. Neutral, complete, the same shape every time**
([reference](https://diataxis.fr/reference/)).

- Do: "Direction. One of: increase, decrease, maintain."
- Do: put entries in the order of the product, and give each the same
  parts in the same order.
- Don't: "We recommend that you start with a project." (Advice.)
- Don't: explain why. Link to the explanation.

**Explanation. Discursive, with reasons and alternatives**
([explanation](https://diataxis.fr/explanation/)).

- Do: title it "About ...".
- Do: "A gap is what is wrong. A problem is what it does to somebody.
  Cartograph keeps them apart so a gap can be measured."
- Do: say where Cartograph departs from a standard, and why.
- Do: cite a standard or research inline, with its link, when a claim
  rests on one.
- Don't: give steps. Link to the how-to.
- Don't: give a scale or a duration as a fact ("usually three years").

## Rules for every page

- **One kind per page.** Link out for the other kinds.
- **Match the interface.** Every button, screen, field and word is as
  `cartograph-ui/src/copy.ts` and the routes have it today. Put button
  and field names in bold, and screen text in italics. If you cannot
  confirm a step from the code or by walking it, leave the step out.
- **Plain English.** Short sentences, standard terms, second person. Use
  Cartograph's own words (indicator, outcome, gap, service, change set)
  and do not shorten or swap them. Explain a new term once, in a short
  gloss.
- **No em dashes.** No rhetoric. No scale opinions.
- **Say it once.** A title and its first sentence should not repeat each
  other.
- **No working notes in a page.** No "Check:", no "Open questions", no
  "Sources" about your own research. Put anything unsettled in the pull
  request. Cite sources a reader can use inline.
- **Paraphrase.** Quote a source at most once, in under fifteen words.

## Check a page before you publish it

1. Which kind is it? Ask the two compass questions.
2. Does anything in it belong to another kind? Move it, or link to it.
3. Does the title fit the kind? A goal for a how-to, a result for a
   tutorial, a thing for reference, "About ..." for explanation.
4. Tutorial: could a newcomer finish with no choice to make and no step
   that can fail? How-to: could a competent reader skip every sentence of
   background and still finish?
5. Is every example from the cooperative, with its real record names?
6. Does every word on a button or screen match the interface?
7. Does it link to its neighbours in the other kinds?

## Grow it one page at a time

Do not add empty sections. Pick one page, ask which need it serves and how
well, make the one change that helps most, and publish it
([workflow](https://diataxis.fr/how-to-use-diataxis/)).
