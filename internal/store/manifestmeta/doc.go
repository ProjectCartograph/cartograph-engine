package manifestmeta

import (
	"encoding/json"

	"go.yaml.in/yaml/v3"
)

// Doc is a manifest's text as a JSON document, for an adapter that keeps
// documents and was given text alone (a writer that sets no
// store.Version.Doc). Nil when the text does not parse or the result
// cannot be written as JSON.
func Doc(text []byte) []byte {
	var doc map[string]any
	if err := yaml.Unmarshal(text, &doc); err != nil || doc == nil {
		return nil
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return nil
	}
	return b
}
