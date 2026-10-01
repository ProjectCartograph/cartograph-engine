package codec_test

import (
	"testing"

	"github.com/ProjectCartograph/cartograph-engine/internal/codec/conformance"
	"github.com/ProjectCartograph/cartograph-engine/internal/codec/json"
	"github.com/ProjectCartograph/cartograph-engine/internal/codec/yaml"
)

func TestYAMLConformance(t *testing.T) { conformance.Run(t, yaml.New()) }
func TestJSONConformance(t *testing.T) { conformance.Run(t, json.New()) }
