// Package sentence composes the sentences Cartograph stores in parts.
//
// A problem, a change and a programme's aim are written in the interface
// as two or three fields under their own headings, and the manifest holds
// exactly those fields. The sentence is an output: `<the groups>
// <situation>, because <cause>`. Nothing stores it, and everything that
// shows one — this server's renderer, the web interface, whatever the CLI
// grows — composes it the same way from the same inputs.
//
// The rules are three, and each was a defect first:
//
//   - A part is a fragment under a heading; the sentence it builds starts
//     with a capital and ends with a stop.
//   - A register's name reads as a phrase mid-clause ("so depot staff
//     learn the same day"), not as the heading it is catalogued under.
//   - A clause never joins onto a full stop.
//
// The composition also tolerates a part that names its own subject, which
// is what text written before the parts existed looks like: a situation
// already beginning with the group's name is not given a second one.
package sentence

import "strings"

const (
	because = ", because "
	so      = ", so "
	soThat  = ", so that "
)

// Finish returns a built sentence with a capital at the front and a stop
// at the end. A first word that is an acronym or carries an internal
// capital is left as written, since that is somebody's spelling rather
// than a fragment's, and a question or exclamation mark is deliberate.
func Finish(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return ""
	}
	r := []rune(t)
	if r[0] >= 'a' && r[0] <= 'z' && !(len(r) > 1 && r[1] >= 'A' && r[1] <= 'Z') {
		r[0] = r[0] - 'a' + 'A'
		t = string(r)
	}
	switch t[len(t)-1] {
	case '.', '?', '!':
		return t
	}
	return t + "."
}

// MidCase returns a name as it reads inside a clause: an ordinary
// capitalised word is lowered, an acronym or a name with an internal
// capital is left alone.
func MidCase(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return name
	}
	first := strings.Fields(trimmed)[0]
	if first == strings.ToUpper(first) {
		return name
	}
	if strings.ToLower(first[1:]) != first[1:] {
		return name
	}
	return strings.ToLower(trimmed[:1]) + trimmed[1:]
}

// WhoPhrase is the subject a set of group names makes. Only the first
// name leads; the rest sit inside the phrase, where a catalogue's heading
// case reads as a defined term rather than as English.
func WhoPhrase(names []string) string {
	kept := make([]string, 0, len(names))
	for _, n := range names {
		if t := strings.TrimSpace(n); t != "" {
			kept = append(kept, t)
		}
	}
	switch len(kept) {
	case 0:
		return ""
	case 1:
		return kept[0]
	}
	inside := make([]string, 0, len(kept)-1)
	for _, n := range kept[1:] {
		inside = append(inside, MidCase(n))
	}
	head := append([]string{kept[0]}, inside[:len(inside)-1]...)
	return strings.Join(head, ", ") + " and " + inside[len(inside)-1]
}

// stripStop drops a terminal full stop before a clause is joined on. Only
// a stop: a question or an exclamation is somebody's wording.
func stripStop(s string) string {
	return strings.TrimSpace(strings.TrimRight(strings.TrimSpace(s), "."))
}

// lead puts a subject in front of a clause, unless the clause already
// names it. Text written before the parts existed carries its own
// subject, and a second one would read "Depot staff Depot staff wait".
func lead(who, clause string) string {
	c := strings.TrimSpace(clause)
	if who == "" || c == "" {
		return strings.TrimSpace(who + " " + c)
	}
	if strings.HasPrefix(strings.ToLower(c), strings.ToLower(who)) {
		return c
	}
	return who + " " + c
}

// Problem composes "<who> <situation>, because <cause>". A subject with
// nothing said about it is not a clause, so a problem with no situation
// and no cause reads as nothing rather than as the group's name.
func Problem(who, situation, cause string) string {
	if strings.TrimSpace(situation) == "" && strings.TrimSpace(cause) == "" {
		return ""
	}
	head := lead(who, situation)
	if strings.TrimSpace(cause) == "" {
		return Finish(head)
	}
	if head == "" {
		return Finish(strings.TrimSpace(cause))
	}
	return Finish(stripStop(head) + because + strings.TrimSpace(cause))
}

// Change composes "<what>, so <who> <gain>". Until the gain is written
// the sentence stops at what will be true, rather than trailing off on a
// group name.
func Change(what, who, gain string) string {
	if strings.TrimSpace(gain) == "" {
		return Finish(what)
	}
	tail := lead(MidCase(who), gain)
	if strings.TrimSpace(what) == "" {
		return Finish(tail)
	}
	return Finish(stripStop(what) + so + tail)
}

// Aim composes "<change>, so that <gain>".
func Aim(change, gain string) string {
	if strings.TrimSpace(gain) == "" {
		return Finish(change)
	}
	if strings.TrimSpace(change) == "" {
		return Finish(gain)
	}
	return Finish(stripStop(change) + soThat + strings.TrimSpace(gain))
}

// SplitProblem and SplitChange recover the parts from a sentence stored
// before the parts existed. They are used once, by the legacy rewrite on
// read, and never by anything that edits: the subject cannot be separated
// from the situation without the group names, so it stays where it was
// written and the composition above leaves it alone.
func SplitProblem(s string) (situation, cause string) {
	if at := strings.Index(strings.ToLower(s), because); at >= 0 {
		return strings.TrimSpace(s[:at]), stripStop(s[at+len(because):])
	}
	return stripStop(s), ""
}

func SplitChange(s string) (what, gain string) {
	if at := strings.Index(strings.ToLower(s), so); at >= 0 {
		return strings.TrimSpace(s[:at]), stripStop(s[at+len(so):])
	}
	return stripStop(s), ""
}

func SplitAim(s string) (change, gain string) {
	if at := strings.Index(strings.ToLower(s), soThat); at >= 0 {
		return strings.TrimSpace(s[:at]), stripStop(s[at+len(soThat):])
	}
	return stripStop(s), ""
}
