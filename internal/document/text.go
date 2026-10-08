package document

import "strings"

// nearName reports whether two names say the same thing: most of their
// words shared, once case, punctuation and short words are set aside.
func nearName(a, b string) bool {
	wa, wb := nameWords(a), nameWords(b)
	if len(wa) == 0 || len(wb) == 0 {
		return false
	}
	shared := 0
	for w := range wa {
		if wb[w] {
			shared++
		}
	}
	small := len(wa)
	if len(wb) < small {
		small = len(wb)
	}
	return float64(shared)/float64(small) >= 0.75 && shared >= 2
}

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

// Clip shortens text to n characters, whole words where it can.
func Clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) <= n {
		return s
	}
	r := []rune(s)[:n]
	if i := strings.LastIndex(string(r), " "); i > n/2 {
		return string(r[:len([]rune(string(r)[:i]))])
	}
	return string(r)
}

func nameWords(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if len(w) > 2 && w != "and" && w != "the" && w != "all" {
			out[strings.TrimSuffix(w, "s")] = true
		}
	}
	return out
}

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
