// Package manifestmeta reads metadata.name and metadata.labels out of a
// manifest's text, for the store adapters that index both beside each
// version. It lives beside the adapters, not in the port: a port names
// no syntax, an adapter may use a library.
//
// It parses with the YAML library because every text a shipped codec
// produces is readable as YAML (JSON is a subset of YAML 1.2). A codec
// whose text is not would give the adapter a decoder of its own
// instead; none exists yet.
package manifestmeta

import "go.yaml.in/yaml/v3"

type manifestMeta struct {
	Metadata struct {
		ID     string            `yaml:"id"`
		Name   string            `yaml:"name"`
		Labels map[string]string `yaml:"labels"`
	} `yaml:"metadata"`
}

// Name extracts metadata.name. The adapter is expected to have already
// accepted this text via a successful engine validation, so a parse
// failure yields an empty name rather than an error.
func Name(text []byte) string {
	var m manifestMeta
	if err := yaml.Unmarshal(text, &m); err != nil {
		return ""
	}
	return m.Metadata.Name
}

// Labels extracts metadata.labels, which a list carries so a register of
// a hundred KPIs can be grouped and filtered without fetching every
// manifest behind it. Nil when there are none.
func Labels(text []byte) map[string]string {
	var m manifestMeta
	if err := yaml.Unmarshal(text, &m); err != nil {
		return nil
	}
	if len(m.Metadata.Labels) == 0 {
		return nil
	}
	return m.Metadata.Labels
}
