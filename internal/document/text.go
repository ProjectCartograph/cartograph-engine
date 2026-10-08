package document

import "strings"

// ReadMonth reads a date as its month, YYYY-MM: from YYYY-MM, YYYY-MM-DD,
// or DD/MM/YYYY.
func ReadMonth(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if len(s) >= 7 && s[4] == '-' && isDigits(s[:4]) && isDigits(s[5:7]) && s[5:7] >= "01" && s[5:7] <= "12" {
		return s[:7], len(s) == 7 || len(s) == 10 && s[7] == '-'
	}
	if len(s) == 10 && s[2] == '/' && s[5] == '/' && isDigits(s[:2]) && isDigits(s[3:5]) && isDigits(s[6:]) && s[3:5] >= "01" && s[3:5] <= "12" {
		return s[6:] + "-" + s[3:5], true
	}
	return "", false
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// Clip shortens text to n characters at the latest boundary that keeps
// at least half of it: the end of a sentence, else of a clause, else of
// a word; never mid-phrase where a boundary will do. A clause's
// trailing joining word ("and", "of") goes with it.
func Clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= n {
		return s
	}
	head := string([]rune(s)[:n+1]) // one more: a boundary may sit just past n
	for _, marks := range [][]string{{". ", "; ", "? ", "! "}, {", ", ": ", " - ", " (", " / "}, {" "}} {
		at := -1
		for _, m := range marks {
			if i := strings.LastIndex(head, m); i > at {
				at = i
			}
		}
		if at < 0 || len([]rune(head[:at])) < n/2 {
			continue
		}
		cut := strings.TrimRight(head[:at], " ,;:-(/")
		if marks[0] != " " {
			cut = strings.TrimRight(strings.TrimSuffix(cut, "."), " ")
		}
		for {
			i := strings.LastIndex(cut, " ")
			if i < 0 || !joiningWords[strings.ToLower(cut[i+1:])] || len([]rune(cut[:i])) < n/2 {
				break
			}
			cut = strings.TrimRight(cut[:i], " ,;:")
		}
		return cut
	}
	return string([]rune(s)[:n])
}

// joiningWords end a clause that goes on: a cut text does not end on
// one.
var joiningWords = map[string]bool{"a": true, "an": true, "and": true, "or": true, "of": true, "the": true, "to": true, "for": true, "in": true, "on": true, "with": true, "by": true, "at": true, "from": true}

// Slug reduces free text to the Slug pattern: lowercase, every run of
// anything else a single hyphen, trimmed, and short enough to read. It
// matches the interface's own slug helper so an id generated on either
// side of the wire comes out the same.
func Slug(s string) string {
	var b strings.Builder
	lastHyphen := true // leading hyphens are not allowed, so start as if one was just written
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastHyphen = false
		case !lastHyphen:
			b.WriteByte('-')
			lastHyphen = true
		}
	}
	out := strings.Trim(b.String(), "-")
	// An id is derived from an item's text only when that text is a name.
	// A field that holds a sentence — an objective, a problem — slugs into
	// something long and cut off mid-word ("catch-faults-at-intake-by-
	// checking-every-deliver"), which is worse than a plain numbered id at
	// the one job an id has: being recognisable. Those fall back instead.
	// The Slug pattern also needs two characters, and a one-character id
	// is not a name anybody would read.
	if len(out) < 2 || len(out) > 32 {
		return ""
	}
	return out
}

// Number reads a JSON or YAML value as a number, whichever numeric type
// its decoder gave it.
func Number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case uint64:
		return float64(n), true
	}
	return 0, false
}
