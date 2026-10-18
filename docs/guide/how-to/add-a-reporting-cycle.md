# Add a reporting cycle

This guide shows you how to add a reporting cycle, which sets the periods
an indicator is read in. The example is a *Depot Audit Cycle*: one round
every six months, starting in March, with the report due ten days after a
round closes.

## Steps

1. In the rail, open **Sheets**, then **Reporting cycles**.
2. Press **Add reporting cycle**.
3. Type the **Name**: *Depot Audit Cycle*.
4. Choose the **Period**. The choices are *Monthly*, *Quarterly*, *Every
   six months* and *Yearly*. Choose *Every six months*.
   - For a number read daily, weekly or every two weeks, choose **Period
     in days** instead, and set the **First day**.
   - For rounds that are not equal, such as terms, fill in **Named
     periods** instead.
5. Type the **Start month**, the month the first round opens, as a number
   from 1 to 12. Type *3*.
6. Type the **Due offset**, the days after a round closes that the report
   is due. Type *10*.
7. Press **Save**. The cycle appears in the sheet, with its period shown
   as *Half year*.
8. Press **Merge** in the top bar to put it in the workspace.

The cycle can now be chosen under **Read each** on an indicator's
**Definition** step.

## See also

- [Record an indicator's readings](record-an-indicators-readings.md)
- [Indicator and data fields](../reference/indicator-and-data-fields.md)
