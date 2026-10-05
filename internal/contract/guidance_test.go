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

// What a walk prepares is what it picks from: every kind its fields
// reference (but its own), each after every kind it needs itself, so the
// preparation can be done top to bottom (TAXONOMY.md D34).
func TestPrepareListsWhatTheWalkPicksFrom(t *testing.T) {
	files := kindSchemas(t)
	prepared := 0
	for kind, file := range files {
		raw, _ := readJSON(t, contract.Flows, "flows/"+strings.ToLower(kind)+".flow.json")
		if raw == nil {
			continue
		}
		b, _ := json.Marshal(raw)
		var flow struct {
			Spec struct {
				Steps []struct {
					Fields []struct {
						Path string `json:"path"`
					} `json:"fields"`
				} `json:"steps"`
				Prepare []struct {
					Kind string `json:"kind"`
				} `json:"prepare"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(b, &flow); err != nil {
			t.Fatal(err)
		}
		if len(flow.Spec.Prepare) == 0 {
			continue
		}
		prepared++
		at := map[string]int{}
		for i, p := range flow.Spec.Prepare {
			at[p.Kind] = i
		}
		schema := readSchema(t, "schemas/"+file)
		for _, s := range flow.Spec.Steps {
			for _, f := range s.Fields {
				for _, named := range refsUnder(file, schema, pointer(f.Path)) {
					if _, ok := at[named]; !ok && named != "*" && named != kind {
						t.Errorf("%s: the walk picks a %s at %s, which prepare does not list", kind, named, f.Path)
					}
				}
			}
		}
		// Whatever a prepared kind may name, among the prepared, comes
		// before it, read from its own schema.
		for _, p := range flow.Spec.Prepare {
			for _, named := range refsUnder(files[p.Kind], readSchema(t, "schemas/"+files[p.Kind]), []string{"spec"}) {
				if i, ok := at[named]; ok && named != p.Kind && i > at[p.Kind] {
					t.Errorf("%s: %s is prepared before %s, which it may name", kind, p.Kind, named)
				}
			}
		}
	}
	if prepared < 3 {
		t.Fatalf("only %d flows say what to prepare", prepared)
	}
}

// refsUnder is every kind the schema references at or below path:
// x-cartograph-ref, followed through $ref and combinators.
func refsUnder(file string, schema map[string]any, tokens []string) []string {
	var out []string
	seen := map[string]bool{}
	var collect func(file string, node any, depth int)
	collect = func(file string, node any, depth int) {
		m, ok := node.(map[string]any)
		if !ok || depth > 12 {
			return
		}
		if r, ok := m["x-cartograph-ref"].(string); ok && !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
		if ref, ok := m["$ref"].(string); ok {
			target, frag, _ := strings.Cut(ref, "#")
			if target == "" {
				target = file
			}
			b, err := fs.ReadFile(contract.Schemas, "schemas/"+target)
			if err == nil {
				var doc any
				_ = json.Unmarshal(b, &doc)
				for _, p := range strings.Split(strings.TrimPrefix(frag, "/"), "/") {
					if p != "" {
						dm, _ := doc.(map[string]any)
						doc = dm[p]
					}
				}
				collect(target, doc, depth+1)
			}
		}
		for _, v := range m {
			switch x := v.(type) {
			case map[string]any:
				collect(file, x, depth+1)
			case []any:
				for _, y := range x {
					collect(file, y, depth+1)
				}
			}
		}
	}
	var at any = schema
	for _, tok := range tokens {
		m, _ := at.(map[string]any)
		if tok == "-" {
			at = m["items"]
			continue
		}
		props, _ := m["properties"].(map[string]any)
		at = props[tok]
	}
	collect(file, at, 0)
	return out
}
