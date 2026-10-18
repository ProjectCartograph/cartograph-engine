# Add a data source

This guide shows you how to record where a number comes from. The
example adds a *Buyer Complaint Log*: complaints from retail buyers about
deliveries, emailed to the quality team and entered by hand each month.

You need to know which team keeps the source and how often it changes.

## Steps

1. In the rail, open **Sheets**, then **Data sources**.
2. Press **Data source** (its label reads *Add data source*). A box
   titled *Add Data source* opens.
3. Type the **Name**: *Buyer Complaint Log*. If a source with a similar
   name exists, a hint offers it so you do not add it twice.
4. Under **One entry is**, write what a single entry stands for: *One
   complaint received from a retail buyer.*
5. Choose the **Category**: *Form or survey*.
6. Choose the **Team** that keeps it: *Quality Team*. The form needs a
   name and a team; everything else can wait.
7. Fill in what you know:
   - **Access**: *Complaints are emailed to the quality team and entered
     by hand.*
   - **Refresh**: *Monthly*.
   - **Quality issues**: *Some complaints name no delivery.*
8. Press **Save**.

The change goes into your change set. The top bar now shows **Agent**
and **Merge**. Press **Merge** when you want it in the workspace.

To change a source later, select its row on the sheet. A box titled
*Edit: <name>* opens, with a **What changed** field at the end, which you
answer in a few words.

## Next

- [Add a metric to a data source](add-a-metric-to-a-data-source.md)
- Every field of a data source is in [Indicator and data
  fields](../reference/indicator-and-data-fields.md).
