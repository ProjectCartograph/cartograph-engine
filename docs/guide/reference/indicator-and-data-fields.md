# Indicator and data fields

This page lists the fields of an indicator, a data source, a reporting cycle, a unit and an indicator's readings, with the names the interface shows. For why they are shaped this way, see [About indicator specifications](../explanation/about-indicator-specifications.md).

Every field is described the same way: what it holds, whether it is required, the form of the answer, and an example from the cooperative's records in `cartograph-engine/examples/minimal`. "Required" means the record cannot be saved without it. Other fields can be left empty, and Cartograph may raise a check about them.

Every kind also has a **Name** and an **Id**. The Id is a short name used in links, and it cannot be changed after the record is saved. When you change a record, you are asked **What changed**.

## Indicator

An indicator is a number you track over time to see whether an aim is being met. It has three pages: **Definition**, **Metric** and **Actuals**.

### Definition page

| Field | Holds | Required | Form | Example |
|---|---|---|---|---|
| **Name** | What people call the indicator. | Yes | Text | Quality pass rate |
| **Definition** | Exactly what is counted, where and when. Leave the target out. | Yes | One sentence | Share of deliveries meeting the quality standard at intake. |
| **Unit** | What the number is counted in. | Yes | One unit, chosen from the list. You can add a new unit from here. | Percent |
| **Which way is good** | Which readings are better. | Yes | One of **Higher is better**, **Lower is better**, **Closest to the target**, **Hold it steady** | Higher is better |
| **Baseline** | Today's figure and the month it is from. | No | **Known**, or **Not known yet** | See below |
| **Target** | The figure to reach, and when. | No | See below | 75, by 2027-06 |
| **Read from** | The data sources the number is read from. | Yes, at least one | One or more data sources, chosen from the list | Quality Check Tool |
| **Read each** | How often the number is read. Sets the periods on the Actuals page. | No | One reporting cycle | Seasonal Cycle |
| **Owned by** | The role that keeps the readings coming and explains them. Use a role, not a person. | No | One role | Quality reviewer |
| **Outcomes** | The outcomes this indicator tells you about. | No | One or more outcomes | Faults found before dispatch |
| **Result level** | What the number shows. | No | **Output** (something the work produced), **Outcome** (something that changed because of it) or **Impact** (the longer-term change it aims at) | Outcome |
| **Breakdown** | How the number is split, such as by region. Leave empty if only the total is reported. | No | One or more segments. You can add a new segment from here. | Region, Product line |

**Baseline.**

| Field | Holds | Form | Example |
|---|---|---|---|
| **Known**: value | Today's figure. | A number, in the indicator's unit | 62 |
| **Known**: month | The month the figure is from. | Year and month | 2025-09 |
| **Not known yet**: **Why it is not known yet** | The reason there is no figure. | Text | |
| **Not known yet**: **Known by** | The month it will be known. | Year and month | |

A zero is a figure. If nobody has measured it, choose **Not known yet**.

**Target.** Choose **When the target is due**:

| Choice | Fields | Holds |
|---|---|---|
| **By a month** | value, month | A figure and the month it is due by. |
| **In a window** | value, **Not before**, **Not after** | A figure and the span in which it is due. |
| **Set when something happens** | **What sets it, in words**, **Expected by**, **What is known of it already** | No figure yet. The event that sets it, the month it is expected by, and what is known of the target before then. |

### Metric page

The Metric page says how the number is worked out. It builds on the metrics a data source gives, so add the data sources on the Definition page first. It is optional.

| Field | Holds | Form |
|---|---|---|
| **Kind of number** | The shape of the number. | **Count or sum** (one count or total of rows), **Share** (one count out of another), **Running total** (a count added up over a window of time) or **From other KPIs** (worked out from other indicators' numbers) |
| **Its number** or **What is counted** | The data source metric the number is, or what is counted in a share. | One metric from the indicator's data sources |
| **Out of** | For a share: the metric it is out of. | One metric from the indicator's data sources |
| **Over the last** | For a running total: how far back it adds up. Empty means all time. | A number and **days**, **weeks**, **months**, **quarters** or **years** |
| **This indicator**, **Worked with**, **That indicator** | For a number from other indicators. | Two indicators and **plus**, **minus**, **times** or **divided by** |
| **Only where** | Which rows count. Empty counts every row. | Conditions: what is compared, how (**is**, **is not**, **is above**, **is below**, **is at least**, **is at most**, **is one of**) and the value |
| **For an analyst** | Columns and filters as the warehouse names them. | Text, left for whoever builds it there |

**Holding the gain** is on the same page.

| Field | Holds | Form |
|---|---|---|
| **Lowest it may be** | The lowest value that still meets the requirement. | A number. Empty means no floor. |
| **Highest it may be** | The highest value that still meets the requirement. | A number. Empty means no ceiling. |
| **When a reading signals trouble** | What sets the response off. | Text |
| **What is done first** | The first thing done. | Text |
| **Who acts** | The role that acts. | One role |
| **Within how many days** | How long it has to act. | A whole number of days |

### Actuals page

The Actuals page shows the readings table. See [Readings](#readings).

## Data source

A data source is a place a number comes from, such as a register, a system, a survey or a set of documents. It is one form.

| Field | Holds | Required | Form | Example |
|---|---|---|---|---|
| **Name** | The name people use for it. | Yes | Text | Quality Check Tool |
| **Category** | The form the source takes. | No | **Database**, **Spreadsheet**, **Paper record**, **Form or survey**, **Operational system**, **API or feed**, **Document store** or **Warehouse** | Form or survey |
| **Provenance** | Whether your organisation runs it. | No | **Internal** or **Third-Party** | Internal |
| **Team** | Your team that uses it and is responsible for it. | Yes | One team | Quality team |
| **Kept by** | Who keeps it, if not your organisation. If empty, your team keeps it. | No | One resource | |
| **Access** | How someone gets access, and what they need first. | No | Text | Paper forms transcribed into a shared spreadsheet each season. |
| **Refresh** | How often the numbers in it change. | No | One choice from the workspace's list | Quarterly |
| **How records get in** | How each record enters the source. | No | **Typed in by hand**, **Scanned from paper forms**, **Filled in online**, **Fed from another system** or **Not decided yet** | |
| **Classification** | How sensitive the contents are. | No | One level from the workspace's list: public, internal, confidential or restricted unless the workspace sets its own | Internal |
| **Quality issues** | What is already known to be wrong with it. | No | Text | Some forms arrive late and are transcribed after the season closes. |
| **One entry is** | What one record of this source is, so its metrics read clearly. | No | One sentence | One delivery checked against the quality standard at a depot's intake. |
| **Metrics** | The numbers this source gives, so every indicator read from it counts the same way. | No | A list | See below |

**Metrics.** Each metric has:

| Field | Holds | Form | Example |
|---|---|---|---|
| **Name** | What the number is called. | Text | Deliveries checked |
| **What it is** | The number in words, for whoever maps it to the warehouse. | Text | Every delivery checked at intake, passed or not. |
| **Aggregation** | One aggregate over this source's own fields: `COUNT(*)`, `COUNT(DISTINCT field)`, `SUM(field)`, `AVG(field)`, `MIN(field)` or `MAX(field)`, with an optional `WHERE` condition. No table and no join. | Text | `COUNT(*) WHERE passed = true` |
| **Counts over** | What one entry this number counts is, where the source holds more than one kind of record. Shown for a source with the category **Warehouse**. | Text | |

## Reporting cycle

A reporting cycle says how often a report is made and when each is due. Choose one of three forms: equal periods of months, equal periods of days, or named periods.

| Field | Holds | Required | Form | Example |
|---|---|---|---|---|
| **Name** | What the cycle is called. | Yes | Text | Seasonal Cycle |
| **Period** | How many months one round covers. | One of the three forms | **Monthly**, **Quarterly**, **Every six months** or **Yearly** | Quarterly |
| **Period in days** | How many days one round covers, for a number read more often than monthly. Leave empty for rounds of months. | One of the three forms | **Daily**, **Weekly** or **Every two weeks** | |
| **First day** | The first day of one round, so the rest line up with it. | With **Period in days** | A date | A Monday |
| **Start month** | The month of the year the first round opens. | With **Period** | 1 to 12 | 9 |
| **Named periods** | Periods that are not equal, such as terms or survey waves. Each is set by the month it ends. | One of the three forms | A list of a name and an end month, or a name and a year and month for a one-off period | |
| **Due offset** | How many days after a round closes the report is due. | No | A whole number of days | 14 |

A reading is filed by the month its period ends, or by the day it ends on a cycle of days.

## Unit

A unit is what a number is counted in. The standard units come with a workspace, and you can add your own.

| Field | Holds | Required | Form | Example |
|---|---|---|---|---|
| **Name** | What the unit is called. | Yes | Text | Percent |
| **Symbol** | The short form, if there is one. | No | Text, up to 12 characters | % |
| **Kind of quantity** | What kind of quantity this is. Hours and days are both a length of time. | Yes | **Percentage**, **Count**, **Money**, **Ratio** or **Length of time** | Percentage |
| **Description** | Exactly what is counted, where the name alone could be read two ways. | No | Text | A share of a whole, out of a hundred. |

## Readings

Readings are the figures an indicator recorded, one for each period. The indicator says what is measured. The readings say what the number was. They are filled in on the indicator's Actuals page.

| Column | Holds | Form | Example |
|---|---|---|---|
| **Period** | The period the reading is for, shown by the month it ends (or the day, on a cycle of days). The periods come from the indicator's reporting cycle. | Set by the cycle | 2026-11 |
| **Reading** | The figure for the period, in the indicator's unit. | A number | 69 |
| **Provisional** | Whether the figure may still change. | A tick | |

A row left blank means the period has not been read yet. It does not mean zero.

The table appears once the indicator has a reporting cycle and a baseline or a target. Until then the page says *Choose how often this indicator is read to see its periods.* or *Add a baseline or a target to see the periods to read.*

Each indicator also shows a chart of its readings against its baseline and target.
