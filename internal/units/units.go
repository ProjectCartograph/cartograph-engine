// Package units holds the units every vault starts with (TAXONOMY.md
// D10): ordinary data, seeded by the engine into a vault that holds none
// and read when an earlier release's free-text unit is mapped to one.
package units

// Unit is one standard unit, as it is seeded.
type Unit struct {
	ID, Name, Symbol, Dimension, Description string
}

// Standard are the units every vault starts with.
//
// "Sensible defaults with an open extension" is the whole shape of D10: a
// closed enum would refuse whatever an organisation measures that nobody
// anticipated, and sending that number into a note is worse than a list
// somebody can add to. So these are ordinary manifests, written into a
// vault that holds none, and an instance declares its own beside them
// with the picker's own add. There is no second code path for a custom
// unit, which is the point.
//
// Only when a vault has no Unit at all. Deleting one does not bring it
// back: these are a starting point, not a policy.
//
// Money is deliberately absent. A money unit is a currency, the funding
// line already picks from the ISO 4217 list, and seeding one currency
// would be Cartograph guessing which country it is in.
var Standard = []Unit{
	{"percent", "Percent", "%", "percent",
		"A share of a whole, out of a hundred."},
	{"count", "Count", "", "count",
		"A plain number of things. Say what is being counted in the indicator's own definition."},
	{"ratio", "Ratio", "", "ratio",
		"One quantity divided by another, written as a single number."},
	{"score", "Score", "", "ratio",
		"A number on a scale somebody defined. The indicator's definition says what the scale is."},
	{"index", "Index", "", "ratio",
		"A number set against a base period, so movement rather than level is what it shows."},
	{"days", "Days", "d", "duration", "Whole days."},
	{"hours", "Hours", "hrs", "duration", "Hours, to the nearest hour unless the indicator says otherwise."},
	{"months", "Months", "mo", "duration", "Whole months."},
}
