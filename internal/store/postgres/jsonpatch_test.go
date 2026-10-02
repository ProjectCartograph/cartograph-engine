package postgres

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

// Applying the patch from a to b gives b, for the shapes manifests take
// and for random documents.
func TestPatchRoundTrip(t *testing.T) {
	cases := [][2]string{
		{`{}`, `{}`},
		{`{"spec":{"readings":[{"period":"2026-01","value":41}]}}`, `{"spec":{"readings":[{"period":"2026-01","value":41},{"period":"2026-02","value":42.5}]}}`},
		{`{"spec":{"readings":[{"period":"2026-01","value":41,"provisional":true}]}}`, `{"spec":{"readings":[{"period":"2026-01","value":40}]}}`},
		{`{"a":[1,2,3,4]}`, `{"a":[1]}`},
		{`{"a":[1,2]}`, `{"a":{"b":1}}`},
		{`{"a/b":{"c~d":1}}`, `{"a/b":{"c~d":2,"e":null}}`},
		{`[1,[2,3]]`, `[1,[2,3,4],5]`},
		{`{"n":12345678901234567890}`, `{"n":12345678901234567891}`},
		{`"x"`, `{"y":1}`},
	}
	for _, c := range cases {
		check(t, []byte(c[0]), []byte(c[1]))
	}
	r := rand.New(rand.NewSource(1))
	for i := 0; i < 2000; i++ {
		a := randomDoc(r, 3)
		b := mutate(r, deepCopy(a), 3)
		ab, _ := json.Marshal(a)
		bb, _ := json.Marshal(b)
		check(t, ab, bb)
	}
}

func check(t *testing.T, a, b []byte) {
	t.Helper()
	p, err := diffJSON(a, b)
	if err != nil {
		t.Fatalf("diff %s -> %s: %v", a, b, err)
	}
	got, err := applyPatch(a, p)
	if err != nil {
		t.Fatalf("apply %s to %s: %v", p, a, err)
	}
	want, _ := decodeJSON(b)
	have, _ := decodeJSON(got)
	if !reflect.DeepEqual(want, have) {
		t.Fatalf("%s + %s = %s, want %s", a, p, got, b)
	}
}

// A reading appended to a long series is one small operation.
func TestPatchOfAnAppendIsSmall(t *testing.T) {
	series := func(n int) []byte {
		rs := []map[string]any{}
		for i := 0; i < n; i++ {
			rs = append(rs, map[string]any{"period": fmt.Sprintf("%04d-%02d", 2016+i/12, 1+i%12), "value": i})
		}
		b, _ := json.Marshal(map[string]any{"spec": map[string]any{"kpi": "k", "readings": rs}})
		return b
	}
	p, err := diffJSON(series(119), series(120))
	if err != nil {
		t.Fatal(err)
	}
	if len(p) > 100 {
		t.Fatalf("the patch for one more reading is %d bytes: %s", len(p), p)
	}
}

func randomDoc(r *rand.Rand, depth int) any {
	switch k := r.Intn(6); {
	case depth == 0 || k < 2:
		return []any{"s", json.Number("1"), true, nil}[r.Intn(4)]
	case k < 4:
		m := map[string]any{}
		for i := r.Intn(4); i > 0; i-- {
			m[[]string{"a", "b", "c/d", "e~f"}[r.Intn(4)]] = randomDoc(r, depth-1)
		}
		return m
	default:
		a := []any{}
		for i := r.Intn(4); i > 0; i-- {
			a = append(a, randomDoc(r, depth-1))
		}
		return a
	}
}

func mutate(r *rand.Rand, v any, depth int) any {
	if r.Intn(4) == 0 || depth == 0 {
		return randomDoc(r, depth)
	}
	switch c := v.(type) {
	case map[string]any:
		for k := range c {
			if r.Intn(3) == 0 {
				delete(c, k)
			} else {
				c[k] = mutate(r, c[k], depth-1)
			}
		}
		if r.Intn(2) == 0 {
			c["new"] = randomDoc(r, depth-1)
		}
		return c
	case []any:
		for i := range c {
			c[i] = mutate(r, c[i], depth-1)
		}
		switch r.Intn(3) {
		case 0:
			return append(c, randomDoc(r, depth-1))
		case 1:
			if len(c) > 0 {
				return c[:r.Intn(len(c))]
			}
		}
		return c
	}
	return randomDoc(r, depth)
}

func deepCopy(v any) any {
	b, _ := json.Marshal(v)
	out, _ := decodeJSON(b)
	return out
}
