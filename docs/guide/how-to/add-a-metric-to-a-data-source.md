# Add a metric to a data source

This guide shows you how to say which numbers a data source can give.
The example adds *Crates collected* and *Crates lost* to the
cooperative's *Collection Log*, so that an indicator can say what share
of crates is lost after picking.

You need the data source. If it is not there yet, see [Add a data
source](add-a-data-source.md).

## Steps

1. In the rail, open **Sheets**, then **Data sources**. Select the row
   *Collection Log*. A box titled *Edit: Collection Log* opens.
2. Under **One entry is**, write what a single entry stands for: *One
   crate collected from a depot.*
3. Under **Metrics**, press **Metric**. Fill in the three fields:
   - **Name**: *Crates collected*.
   - **What it is**: *Every crate collected from a depot.*
   - **Aggregation**: `COUNT(*)`. It says what you mean to count, for
     whoever builds it in the warehouse. It is not the calculation. A
     tick and a short reading appear beneath it when it is understood.
4. Press **Metric** again and fill in the second one:
   - **Name**: *Crates lost*.
   - **Aggregation**: `COUNT(*) WHERE lost = true`.
5. Under **What changed**, at the end of the box, write *Added metrics*.
6. Press **Save**, then **Merge** in the top bar.

If the source's category is *Warehouse*, each metric also asks what it
**Counts over**, because a warehouse holds several kinds of record.

The aggregation shapes you can write are listed in [Indicator and data
fields](../reference/indicator-and-data-fields.md).

## Next

- [Connect an indicator to a data source](connect-an-indicator-to-a-data-source.md)
