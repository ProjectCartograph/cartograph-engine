# Connect an indicator to a data source

This guide shows you how to say how an indicator's number is worked out
from a data source's metrics. The example connects *Post harvest loss
rate* to the *Collection Log*, as the share of crates lost out of crates
collected.

You need the indicator, and a data source with metrics. See [Add a metric
to a data source](add-a-metric-to-a-data-source.md).

## Steps

1. In the rail, choose **Indicators** and select *Post harvest loss
   rate*.
2. On **Definition**, check that the data source is listed under **Read
   from**. In the example it is *Collection Log*. To add one, choose it
   in the same box.
3. Open **Metric**.
4. Under **Kind of number**, choose **Share**. The other choices are
   **Count or sum**, **Running total** and **From other KPIs**.
5. Open **What is counted** and choose *Crates lost*. Open **Out of** and
   choose *Crates collected*. Each choice shows its source, as in
   *Crates collected · Collection Log*.

The page now reads *The share of crates lost out of crates collected.*

For **Count or sum**, the one picker is called **Its number**.

If a picker says the source *gives no metrics yet*, add them to the data
source first.

## See also

- [Export the indicators for dbt](export-the-indicators-for-dbt.md)
- [Indicator and data fields](../reference/indicator-and-data-fields.md)
