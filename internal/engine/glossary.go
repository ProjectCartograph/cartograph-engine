package engine

import "sort"

// The glossary (TAXONOMY.md D29). A person meeting Cartograph for the
// first time reads its words the way they read a dictionary: the word,
// one plain sentence saying what it is, and an example. The guidance
// holds both for every kind and level; the glossary lists them in the
// order of work, the order a person meets them in, and every interface
// shows the same definition wherever the word appears.

// GlossaryEntry is one word of the taxonomy: a stage of the order of work
// (a kind, or a Goal at a level) or a register.
type GlossaryEntry struct {
	// Key is the stage's key, or for a register its kind.
	Key   string `json:"key"`
	Kind  string `json:"kind"`
	Level string `json:"level,omitempty"`
	// Summary says what it is, in one plain sentence; Example is one
	// instance of it.
	Summary string `json:"summary"`
	Example string `json:"example,omitempty"`
	// After are the stages it comes after in the order of work.
	After    []string `json:"after,omitempty"`
	Register bool     `json:"register,omitempty"`
}

// afterOf is what each kind that holds a stage's data comes after.
var afterOf = map[string][]string{"KPIReadings": {"kpi"}, "PortfolioDecisions": {"portfolio"}}

// Glossary is every word of the taxonomy in locale, in the order of work:
// the stages from the purpose down, then the registers, then what holds a
// stage's data.
func (e *Engine) Glossary(locale string) ([]GlossaryEntry, error) {
	var out []GlossaryEntry
	add := func(key, kind, level string, after []string, register bool) error {
		b, ok, err := GuideBundleFor(kind, locale)
		if err != nil || !ok {
			return err
		}
		entry := GlossaryEntry{Key: key, Kind: kind, Level: level, Summary: b.Summary, Example: b.Example, After: after, Register: register}
		if level != "" {
			if s := b.Levels[level]; s != "" {
				entry.Summary = s
			}
			entry.Example = b.LevelExamples[level]
		}
		out = append(out, entry)
		// The other words the kind holds, after it, in a stable order.
		if level == "" || level == goalLevels[0] {
			names := make([]string, 0, len(b.Terms))
			for name := range b.Terms {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				t := b.Terms[name]
				out = append(out, GlossaryEntry{Key: name, Kind: kind, Summary: t.Summary, Example: t.Example, After: []string{key}, Register: register})
			}
		}
		return nil
	}
	for _, s := range stages {
		if err := add(s.Key, s.Kind, s.Level, s.After, false); err != nil {
			return nil, err
		}
	}
	for _, k := range registers {
		if err := add(k, k, "", nil, true); err != nil {
			return nil, err
		}
	}
	for _, k := range afterStages {
		if err := add(k, k, "", afterOf[k], false); err != nil {
			return nil, err
		}
	}
	return out, nil
}
