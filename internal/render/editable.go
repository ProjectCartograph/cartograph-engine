package render

import "strings"

// An editable charter: every value it prints carries the field it comes
// from, so an interface edits the document where it is read, as a shared
// document is edited, rather than in a form beside it (cartograph-ui#75).
// A value the charter works out (a start date from the milestones, a
// budget's total) carries the field it is worked out from instead. Only
// CharterParts asks for it; the charter's page and its PDF print as
// before, with nothing marked and nothing empty shown.

// editMark opens a marked value: the mark, f (a field) or s (a source),
// the JSON pointer, the mark again, then the value.
const editMark = "\x1e"

// at is value as the field at path holds it. In an editable document an
// empty value still prints, as the place to type it.
func (d *doc) at(value, path string) string {
	if !d.editable || path == "" {
		return value
	}
	return editMark + "f" + path + editMark + value
}

// from is a value worked out from the field at path.
func (d *doc) from(value, path string) string {
	if !d.editable || path == "" || strings.TrimSpace(value) == "" {
		return value
	}
	return editMark + "s" + path + editMark + value
}

// marked reads a marked value: whether it names a field or a source, the
// pointer, and the value.
func marked(s string) (source bool, path, value string, ok bool) {
	if !strings.HasPrefix(s, editMark) || len(s) < 2 {
		return false, "", "", false
	}
	rest := s[2:]
	i := strings.Index(rest, editMark)
	if i < 0 {
		return false, "", "", false
	}
	return s[1] == 's', rest[:i], rest[i+len(editMark):], true
}

// valueHTML is a value as the charter prints it: escaped, a chip as a
// chip, and a marked value inside a span that names its field or source.
func valueHTML(s string) string {
	if source, path, v, ok := marked(s); ok {
		attr := "data-field"
		if source {
			attr = "data-source"
		}
		empty := ""
		if strings.TrimSpace(v) == "" {
			empty = ` data-empty=""`
		}
		return `<span ` + attr + `="` + esc(path) + `"` + empty + `>` + valueHTML(v) + `</span>`
	}
	if group, value, ok := isChip(s); ok {
		return chipHTML(group, value)
	}
	return esc(s)
}

// addRow is, in an editable document, where a list gains an item: what is
// added under it, by the list's pointer.
func (d *doc) addRow(path, what string) {
	if !d.editable {
		return
	}
	d.flush()
	d.b.WriteString(`<p data-add="` + esc(path) + `" data-add-label="` + esc(what) + `"></p>` + "\n")
}
