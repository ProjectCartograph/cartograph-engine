# About indicator specifications

An indicator is a number you watch to see whether something is working. A number alone is not much use. Two people can read "62" and mean different things by it. This page explains what a good written description of an indicator and its data holds, why each part is there, and where Cartograph keeps it.

## Why a number needs a description

The OECD's glossary of evaluation and results terms gives the reason in one note. An indicator is always only an approximation, and it needs interpretation and explanation even when it is measured accurately ([OECD, Glossary of Key Terms in Evaluation and Results-Based Management, second edition](https://www.oecd.org/content/dam/oecd/en/publications/reports/2023/06/glossary-of-key-terms-in-evaluation-and-results-based-management-for-sustainable-development-second-edition_2767e14e/632da462-en-fr-es.pdf)). The same glossary says indicators should give simple, verifiable and reliable means to track change.

So the written description does two jobs. It lets a stranger count the same thing the same way. And it says what the number can and cannot tell you.

## The common core

Specification formats from different bodies look different, but they keep asking the same questions. These are the ones that recur.

1. **What exactly is counted.** A precise definition, including the concepts it relies on.
2. **In what unit.**
3. **Which way is better.** Benefits guidance says this; the UN template does not.
4. **Where each figure comes from,** and who provides it.
5. **How it is collected and worked out.**
6. **How often** it is collected and reported.
7. **Who is responsible** for the indicator and for the data.
8. **A baseline and a target,** each with a date.
9. **Breakdowns,** such as by region.
10. **Known limits,** and how quality is checked.

You will see each of them below, in the sources and then in Cartograph.

## What the sources hold

### The UN's SDG indicator metadata

Every Sustainable Development Goal indicator comes with a metadata sheet. The current template is used for [proposals in the 2025 comprehensive review](https://unstats.un.org/sdgs/files/2025-comprehensive-review/Target_2.2_addition_proposal_from_Switzerland_FSO-metadata.pdf). Its numbered groups are:

- Indicator information: which goal and target, related indicators.
- Data reporter: the organisation and the contact for questions.
- Definition, concepts and classifications: the definition, the unit of measure, any classification used.
- Data source type and collection method: the kind of source, how data are collected, the collection calendar and the release calendar, who provides and who compiles the data.
- Other methodological considerations: why the indicator exists, its limitations, how it is computed, how it is validated, how missing values are treated, and quality management and assessment.
- Data availability and disaggregation: coverage, the time series, and the breakdowns available. The example sheet lists sex, urban or rural residence, wealth, age group, mother's education and region.
- Comparability, and references.

One habit in it is worth borrowing. The template says to write "not applicable" or "TBD" where a field does not apply or is not decided yet, rather than leave it blank. A stated unknown is information. A blank is not.

### USAID's Performance Indicator Reference Sheet

USAID uses a Performance Indicator Reference Sheet, called a PIRS, to define each indicator. Its guidance describes the sheet as a way to keep indicator data consistent and of good quality, and it asks for known limitations to be identified for each indicator and for steps to address them ([USAID, Activity MEL Plan template, 2021, Annex 2 and section C](https://apply07.grants.gov/grantsws/rest/opportunity/att/download/329282)).

A PIRS covers a precise definition, the unit of measure, how the indicator is broken down, the data source, how the data are collected, how often they are reported, who is responsible, the history of data quality assessments, known limits, and the reason for the target ([USAID, Activity MEL Plan template, 2021, Annex 2 and section C](https://apply07.grants.gov/grantsws/rest/opportunity/att/download/329282)).

### USAID's five data quality standards

USAID asks that the data behind an indicator meet five standards ([USAID, Activity MEL Plan template, 2021, Annex 2 and section C](https://apply07.grants.gov/grantsws/rest/opportunity/att/download/329282)):

- **Validity:** the data clearly and adequately represent the result you mean to measure.
- **Integrity:** there are safeguards against transcription mistakes and against data being changed on purpose.
- **Precision:** the data are detailed enough for the decisions they inform.
- **Reliability:** data are collected and analysed in a consistent way over time.
- **Timeliness:** the data are recent enough, and come often enough, to be useful.

These are a good set of questions to ask of any source, whoever you are.

### Benefits registers

The UK government's [Guide for Effective Benefits Management in Major Projects](https://www.apm.org.uk/media/5csnk4oe/guideforeffectivebenefitsmanagementinmajorprojects.pdf) describes a benefits register. It lists each planned benefit, who is responsible for measuring it, when it is expected, how it will be measured, and the baseline and current measurements. A profile for each benefit adds the measure, the baseline, the target, how often it is measured and the risks.

The guide is firm on the baseline. It should be taken at the start, so that delivery does not disturb it, and so that there is something to compare the results with afterwards. The [benefits workbook](https://projectdelivery.gov.uk/library-products/benefits-practitioner-workbook/part-a-an-overview-of-benefits-management-in-government/) adds that each benefit has one named owner, and that new measures should be in proportion to the benefit's importance.

## How Cartograph holds it

Cartograph keeps an indicator in four places that work together. Keeping them apart is deliberate. What is measured, where it comes from, when it is read and what it is read in each change for different reasons.

| The question | Where it lives |
|---|---|
| What is counted, which way is better, baseline, target, breakdown, owner | The **indicator** |
| Where the number comes from, how records get in, how often it changes, what is known to be wrong | The **data source** |
| How often the indicator is read, and when each report is due | The **reporting cycle** |
| What it is counted in | The **unit** |
| What the number was in each period | The indicator's **readings** |

For example, in the cooperative's records, **Quality pass rate** is an indicator. Its unit is **Percent**. It is read from the **Quality Check Tool**, a data source whose records are paper forms typed into a shared spreadsheet, and it is read on the **Seasonal Cycle**. Each season's number is a reading. If the source changes, you edit one data source and every indicator read from it follows.

### The common core, in Cartograph's words

| Common question | Cartograph field |
|---|---|
| What exactly is counted | Indicator: **Definition** |
| In what unit | Indicator: **Unit**, which refers to a unit record |
| Which way is better | Indicator: **Which way is good** |
| Where it comes from | Indicator: **Read from**; the data source's **Category**, **Provenance**, **Team**, **Kept by** and **Access** |
| How it is collected | Data source: **How records get in**; indicator: the **Metric** step |
| How often | Indicator: **Read each**; data source: **Refresh** |
| Who is responsible | Indicator: **Owned by** (a role, not a person); data source: **Team** |
| Baseline and target | Indicator: **Baseline** and **Target** |
| Breakdowns | Indicator: **Breakdown** |
| Known limits | Data source: **Quality issues** |

Two points deserve a word.

**Unknowns are first-class.** The baseline can be **Not known yet**, with the reason and a month by which it will be known. A target can be **Set when something happens**. This is the same habit as the UN template's "TBD", and it is the benefits guide's point about baselines too: it is better to say a baseline is unknown, and when it will be, than to guess one.

**The source decides how soon you can know.** The data source asks how records get in: typed in by hand, scanned from paper forms, filled in online, fed from another system, or not decided yet. A source filled from paper cannot report a day after the period ends. This is why the indicator's guidance suggests choosing a cycle that its data source can keep up with.

### The results chain

An indicator also says what it shows. **Result level** is **Output** (something the work produced), **Outcome** (something that changed because of it) or **Impact** (the longer-term change it aims at). This keeps a count of things delivered from being mistaken for proof that something changed.

## What an analyst may add

You can describe an indicator in plain words and stop. Someone who builds the numbers in a warehouse can go further, and nothing in the plain description gets in their way.

- **Metrics on a data source.** A source can list the numbers it gives, each as an aggregation such as counting every row, or counting rows where a field has a value. The indicator's **Metric** step then picks from them: a count or sum, a share (one count out of another), a running total, or one worked out from other indicators.
- **Specification limits and a response.** **Holding the gain** asks for the lowest and highest values a reading may have, and what is done when one goes outside.
- **Classification** of a source, and custom units and reporting cycles.

## What is not yet asked

Set against the sources above, some questions do not have a field of their own yet. They are worth keeping in mind when you write an indicator:

- **Why this target.** USAID's sheet asks for the reason. Cartograph's target has a value and a date, so the reason belongs in your definition or in a note on the project.
- **Limits of the indicator itself.** **Quality issues** sits on the data source. If an indicator can mislead whatever its source (for example, a share that moves because the total moved), say so in its definition.
- **A named quality worry.** The five USAID standards, or the data quality characteristics in the international standard [ISO/IEC 25012](https://www.iso25000.com/index.php/en/iso-25000-standards/iso-25012), are useful prompts when you fill in **Quality issues**.
- **How the baseline was measured, and by whom.** Cartograph records the figure and its month.

## Where to go next

- For every field and what it holds, see the [Indicator and data fields reference](../reference/indicator-and-data-fields.md).
- For why Cartograph asks for so little at the start, see [About planning ahead](about-planning-ahead.md).
