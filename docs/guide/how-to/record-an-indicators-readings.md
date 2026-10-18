# Record an indicator's readings

This guide shows you how to enter the values measured for an indicator,
period by period. The example is *Quality pass rate*, the share of
deliveries that meet the quality standard at intake, read each season.

You need an indicator with a reporting cycle and a baseline or a target.
To set those up, see [Kinds of record](../reference/kinds-of-record.md)
and [Add a reporting cycle](add-a-reporting-cycle.md).

## Steps

1. In the rail, choose **Indicators** and select *Quality pass rate*.
   The list is grouped by result level, and selecting a row opens the
   indicator on its **Definition** step.
2. Open **Actuals**, the third step after **Definition** and **Metric**.
   This is where readings are entered.
3. Find the row for the period you measured in the **Readings** table.
   It has one row for each period of the indicator's cycle, from the
   first period to the target's. In the example, periods up to 2027-02
   already have a reading, so the next empty row is 2027-05.
4. Type the value in **Reading**. Typing into an empty row creates the
   reading. Clearing the box removes it.
5. If the figure is early or unconfirmed, tick **Provisional**.
6. Your first edit starts a change set, named *Changes to Quality pass
   rate readings*, with **Agent**, a bin, an eye and **Merge** in the top bar.
   Press **Merge** when you are done, to put the readings into the
   workspace. See [Review and merge a change set](review-and-merge-a-change-set.md).

Leave a row blank if nobody has read it yet. Blank is not zero. A zero is
a measured value.

## If no periods appear

- If you see *Add a baseline or a target to see the periods to read.*,
  open **Definition** and add a baseline (a value and its month) or a
  target.
- If you see *Choose how often this indicator is read to see its
  periods.*, open **Definition** and choose a reporting cycle under
  **Read each**. If the cycle you need does not exist, [add a
  reporting cycle](add-a-reporting-cycle.md) first.

## See also

- [Indicator and data fields](../reference/indicator-and-data-fields.md)
