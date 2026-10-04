package contract_test

import (
	"encoding/json"
	"io/fs"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/ProjectCartograph/cartograph-engine/v2/internal/contract"
)

// kindSchemas maps each manifest kind to its schema file: the schemas
// whose kind is a constant.
func kindSchemas(t *testing.T) map[string]string {
	t.Helper()
	entries, err := fs.ReadDir(contract.Schemas, "schemas")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, e := range entries {
		doc := readSchema(t, "schemas/"+e.Name())
		props, _ := doc["properties"].(map[string]any)
		kind, _ := props["kind"].(map[string]any)
		// A vault's own descriptor is not something a person defines.
		if c, ok := kind["const"].(string); ok && c != "Flow" && c != "Vault" {
			out[c] = e.Name()
		}
	}
	return out
}

// resolve follows a JSON pointer into a schema, through $ref into this
// file or common.schema.json, and through allOf, anyOf and oneOf; list
// items are "-" or a number.
func resolve(t *testing.T, file string, node map[string]any, tokens []string) bool {
	t.Helper()
	if ref, ok := node["$ref"].(string); ok {
		target, frag, _ := strings.Cut(ref, "#")
		if target == "" {
			target = file
		}
		doc := readSchema(t, "schemas/"+target)
		var cur any = doc
		for _, p := range strings.Split(strings.TrimPrefix(frag, "/"), "/") {
			if p == "" {
				continue
			}
			m, _ := cur.(map[string]any)
			cur = m[p]
		}
		next, ok := cur.(map[string]any)
		if !ok {
			return false
		}
		return resolve(t, target, next, tokens)
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		if list, ok := node[key].([]any); ok {
			for _, sub := range list {
				if m, ok := sub.(map[string]any); ok && resolve(t, file, m, tokens) {
					return true
				}
			}
		}
	}
	if len(tokens) == 0 {
		return true
	}
	tok := tokens[0]
	if tok == "-" || strings.Trim(tok, "0123456789") == "" {
		items, ok := node["items"].(map[string]any)
		return ok && resolve(t, file, items, tokens[1:])
	}
	props, _ := node["properties"].(map[string]any)
	next, ok := props[tok].(map[string]any)
	return ok && resolve(t, file, next, tokens[1:])
}

func pointer(path string) []string { return strings.Split(strings.TrimPrefix(path, "/"), "/") }

func compile(t *testing.T, fsys fs.FS, path string) *jsonschema.Schema {
	t.Helper()
	b, err := fs.ReadFile(fsys, path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(string(b)))
	if err != nil {
		t.Fatal(err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(path, doc); err != nil {
		t.Fatal(err)
	}
	s, err := c.Compile(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func readJSON(t *testing.T, fsys fs.FS, path string) (any, map[string]any) {
	t.Helper()
	b, err := fs.ReadFile(fsys, path)
	if err != nil {
		t.Errorf("missing: %v", err)
		return nil, nil
	}
	raw, err := jsonschema.UnmarshalJSON(strings.NewReader(string(b)))
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return raw, m
}

// Every kind has a flow and English guidance, so a person and an agent
// are walked through any kind the same way; each is valid, names only
// fields its kind has, and its links match its flow's.
func TestEveryKindIsGuided(t *testing.T) {
	goal := readSchema(t, "schemas/goal.schema.json")
	if resolve(t, "goal.schema.json", goal, pointer("/spec/nope")) || !resolve(t, "goal.schema.json", goal, pointer("/spec/keyResults/-/target/value")) {
		t.Fatal("the field resolver does not tell a field from none")
	}
	flowSchema := compile(t, contract.Flows, "flows/flow.schema.json")
	guideSchema := compile(t, contract.Guidance, "guidance/guidance.schema.json")
	for kind, file := range kindSchemas(t) {
		schema := readSchema(t, "schemas/"+file)
		name := strings.ToLower(kind)

		flowPath := "flows/" + name + ".flow.json"
		raw, flow := readJSON(t, contract.Flows, flowPath)
		if flow == nil {
			continue
		}
		if err := flowSchema.Validate(raw); err != nil {
			t.Errorf("%s: %v", flowPath, err)
			continue
		}
		flowLinks := map[string]bool{}
		var fields func(prefix string, list []any)
		fields = func(prefix string, list []any) {
			for _, f := range list {
				fm, _ := f.(map[string]any)
				path := prefix + fm["path"].(string)
				if !resolve(t, file, schema, pointer(path)) {
					t.Errorf("%s: field %s is not in %s", flowPath, path, file)
				}
				if sub, ok := fm["fields"].([]any); ok {
					fields(path+"/-", sub)
				}
			}
		}
		spec, _ := flow["spec"].(map[string]any)
		steps, _ := spec["steps"].([]any)
		for _, s := range steps {
			sm, _ := s.(map[string]any)
			list, _ := sm["fields"].([]any)
			fields("", list)
			links, _ := sm["links"].([]any)
			for _, l := range links {
				lm, _ := l.(map[string]any)
				flowLinks[lm["kind"].(string)+":"+lm["path"].(string)] = true
			}
		}

		// A flow is how an interface renders the kind, so it asks for every
		// field a person can set; one left out could not be set at all.
		// Settings' examples is a map no control holds, set in the file.
		asked := map[string]bool{}
		for _, s := range steps {
			sm, _ := s.(map[string]any)
			list, _ := sm["fields"].([]any)
			for _, f := range list {
				fm, _ := f.(map[string]any)
				asked[fm["path"].(string)] = true
			}
		}
		specProps, _ := schema["properties"].(map[string]any)["spec"].(map[string]any)["properties"].(map[string]any)
		want := []string{"/metadata/name"}
		for p, def := range specProps {
			// A deprecated property is read, never asked for (spec.name,
			// folded into metadata.name).
			desc, _ := def.(map[string]any)["description"].(string)
			if !(kind == "Settings" && p == "examples") && !strings.HasPrefix(desc, "Deprecated") {
				want = append(want, "/spec/"+p)
			}
		}
		for _, p := range want {
			found := asked[p]
			for a := range asked {
				found = found || strings.HasPrefix(a, p+"/")
			}
			if !found {
				t.Errorf("%s: the flow never asks for %s", flowPath, p)
			}
		}

		guidePath := "guidance/en/" + name + ".guidance.json"
		raw, guide := readJSON(t, contract.Guidance, guidePath)
		if guide == nil {
			continue
		}
		if err := guideSchema.Validate(raw); err != nil {
			t.Errorf("%s: %v", guidePath, err)
			continue
		}
		if guide["kind"] != kind {
			t.Errorf("%s: kind %v, want %s", guidePath, guide["kind"], kind)
		}
		gf, _ := guide["fields"].(map[string]any)
		for path := range gf {
			if !resolve(t, file, schema, pointer(path)) {
				t.Errorf("%s: field %s is not in %s", guidePath, path, file)
			}
		}
		gl, _ := guide["links"].(map[string]any)
		for key := range gl {
			if !flowLinks[key] {
				t.Errorf("%s: link %s is not one of the flow's", guidePath, key)
			}
		}
		for key := range flowLinks {
			if _, ok := gl[key]; !ok {
				t.Errorf("%s: the flow's link %s has no words", guidePath, key)
			}
		}
	}
}

// A flow's stages are the walk a person sees: read in order they give every
// step once, in the order of the steps, so grouping never reorders the
// work or drops a step from it.
func TestStagesWalkEveryStepOnce(t *testing.T) {
	staged := 0
	for kind := range kindSchemas(t) {
		path := "flows/" + strings.ToLower(kind) + ".flow.json"
		raw, _ := readJSON(t, contract.Flows, path)
		if raw == nil {
			continue
		}
		var flow struct {
			Spec struct {
				Steps []struct {
					Key string `json:"key"`
				} `json:"steps"`
				Stages []struct {
					Key   string   `json:"key"`
					Steps []string `json:"steps"`
				} `json:"stages"`
			} `json:"spec"`
		}
		b, _ := json.Marshal(raw)
		if err := json.Unmarshal(b, &flow); err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		if len(flow.Spec.Stages) == 0 {
			continue
		}
		staged++
		var walked, want []string
		for _, st := range flow.Spec.Stages {
			walked = append(walked, st.Steps...)
		}
		for _, s := range flow.Spec.Steps {
			want = append(want, s.Key)
		}
		if strings.Join(walked, " ") != strings.Join(want, " ") {
			t.Errorf("%s: the stages walk %v, the steps are %v", path, walked, want)
		}
	}
	if staged == 0 {
		t.Fatal("no flow has stages")
	}
}
