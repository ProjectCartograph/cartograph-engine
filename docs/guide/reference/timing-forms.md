# Timing forms

A timing says when something is due or happens. Cartograph uses one shape for it wherever a record says when. This page lists the four forms, their fields, what an event is, and where each form is used. It describes. For the reasons behind it, see [About planning ahead](../explanation/about-planning-ahead.md) and [About long horizons](../explanation/about-long-horizons.md).

Interface labels are given in bold. The contract names are in code.

## The four forms

Each timing has a `form`. The form decides which other fields it takes. The form is chosen by its icon and short label.

| Form | Interface label | Short label | Takes | Meaning |
|---|---|---|---|---|
| `date` | *On a date* | **Date** | `date` | A month, or a day where one is fixed. |
| `window` | *In a window* | **Window** | `notBefore`, `notAfter` | Not earlier than one date, not later than another, or both. At least one is required. |
| `after` | *After something else* | **After** | `event`, `lagMonths`, `lagDays` | Follows an event, with an optional lag. |
| `when` | *Set once something happens* | **Once** | `event`, `expectedBy`, `decidedBy` | Not known yet. It is set when an event happens. |

Choosing the form that is already chosen clears the timing.

## Fields

Every entry has the same parts: where it applies, its format, its limits, and its label.

### `form`

- **Applies to:** every timing.
- **Format:** one of `date`, `window`, `after`, `when`.
- **Limits:** required.
- **Label:** **How it is timed**.

### `date`

- **Applies to:** the `date` form.
- **Format:** `YYYY-MM`, or `YYYY-MM-DD`.
- **Limits:** required for the `date` form.
- **Label:** **On**. The sun icon, labelled **Give a day, not only a month**, switches between a month and a day.

### `notBefore`

- **Applies to:** the `window` form.
- **Format:** `YYYY-MM`, or `YYYY-MM-DD`.
- **Limits:** a window needs `notBefore`, `notAfter`, or both.
- **Label:** **Not before**.

### `notAfter`

- **Applies to:** the `window` form.
- **Format:** `YYYY-MM`, or `YYYY-MM-DD`.
- **Limits:** as for `notBefore`.
- **Label:** **Not after**.

### `event`

- **Applies to:** the `after` and `when` forms.
- **Format:** an event (see below).
- **Limits:** required for both forms.
- **Label:** **Waits on**. Beside it, the heading *What it waits on* names the choice.

### `lagMonths`

- **Applies to:** the `after` form.
- **Format:** whole number of months.
- **Limits:** 0 to 120.
- **Label:** **Months after it**, shown as *plus* a number and *months*.

### `lagDays`

- **Applies to:** the `after` form.
- **Format:** whole number of days.
- **Limits:** 0 to 366.
- **Label:** **Days after it, besides the months**.

### `expectedBy`

- **Applies to:** the `when` form.
- **Format:** `YYYY-MM`, or `YYYY-MM-DD`.
- **Limits:** required for the `when` form.
- **Label:** **Expected by**. Once this month passes and the event has not happened, the item is flagged.

### `decidedBy`

- **Applies to:** the `when` form.
- **Format:** a reference to a role or governance body.
- **Limits:** optional.
- **Label:** **Set by**.

### `risks`

- **Applies to:** every form, where the record has risks.
- **Format:** a list of the record's own risk ids.
- **Limits:** optional. Recorded and shown. It is never simulated.
- **Label:** **Could move it**, one chip for each risk.

### `note`

- **Applies to:** every form.
- **Format:** text.
- **Limits:** up to 240 characters.
- **Label:** **Note**.

## Events

An event is something that happens to an item. A timing of the form `after` or `when` waits on one.

| Part | Contract name | Meaning |
|---|---|---|
| What it happens to | `on` | A milestone, deliverable or condition of this project; another project (with `item` naming the milestone, else a deliverable or condition, in it); a record such as an indicator's first reading; or something outside the workspace, named in words. |
| The item in another project | `item` | The id of the item in the other project. Used when `on` names another project. |
| What happens | `happens` | One of `reached`, `accepted`, `met`, `firstReading`, `issued`, `decided`, `approved`, `closed`, `landed`, `occurred`. Left out, it is the item's own ending: a milestone reached, a deliverable accepted, a condition met. The screens do not set it. |

In the interface, the picker for an event offers these groups:

- **Milestones**, **Deliverables** and **Conditions** of this project.
- *Milestones of* another project, for each project linked to this one by components.
- *Something outside this project*, with **What happens, in words**.
- *A new milestone*, with *Name of the new milestone*. It is made in the plan, where you give it a date.

An event on another project's milestone is how a dependency between projects reaches the schedule. A reference inside an event says when, not what the record is built on, so it is exempt from the order in which records are written.

## Where timings are used

| Record | Field | Interface label | Where |
|---|---|---|---|
| Project | `milestones[].timing` | **When** | Plan, Schedule |
| Project | `milestones[].waitsOn` (events) | what the milestone waits on, drawn on the schedule | Plan, Schedule |
| Project | `deliverables[].due` | **Due** | Scope and deliverables |
| Project | `conditions[].due` | **Due** | Approval |
| Project | `conditions[].gates` (an event) | **Holds back** | Approval |
| Project | `risks[].depends.needed` | **Needed by** | Plan, Risks |
| Project | `procurement[].requiredBy` | **Needed by** | Plan, Costs and procurement |
| Indicator | target `due` | **When the target is due**: *By a month*, *In a window*, *Set when something happens* | The indicator's definition |
| Indicator | target `setWhen` | *Set when something happens*, with **Expected by** | The indicator's definition |
| Indicator | baseline marked *Not known yet* | **Why it is not known yet**, **Known by** | The indicator's definition |

The screens for an indicator's target offer three of the four forms. The contract allows all four.

Programmes and portfolios hold no timing. An aim in the strategy holds a period instead, its horizon: a start and an end, each a year (`2027`) or a year and month (`2027-09`). A start year is read as January and an end year as December.

## What the engine does with a timing

- A milestone that follows another is linked finish to start. The engine finds the longest chain, which it shows as the critical path.
- A loop of waits, within one project or across several, is refused or leaves the milestones on it unplaced.
- Nothing reschedules itself. A conflict is shown, and the person decides.
- A target or baseline set when an event happens meets its check until `expectedBy` passes. After that it is flagged.

## See also

- [About planning ahead](../explanation/about-planning-ahead.md)
- [About long horizons](../explanation/about-long-horizons.md)
- [Kinds of record](kinds-of-record.md)
