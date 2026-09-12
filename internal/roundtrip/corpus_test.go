package roundtrip_test

import (
	"encoding/json"
	"flag"
	"os"
	"sort"
	"strings"
	"testing"

	schemalib "github.com/swaggest/jsonschema-go"

	"github.com/StevenACoffman/protoschemer/internal/jsonschema"
	"github.com/StevenACoffman/protoschemer/internal/protoemit"
	"github.com/StevenACoffman/protoschemer/internal/roundtrip"
)

// corpus points at a real JSON Schema document to check for coverage. It
// defaults to the OneRoster v1.2 getRoster payload vendored under testdata.
//
// The point of the corpus test is not that this one file round-trips — the
// aspect table already says what each construct becomes. It is that the file
// uses nothing the table has not classified. A schema keyword nobody has
// thought about is the failure worth catching, and it is invisible to a suite
// that only tests constructs someone remembered to write down.
var corpus = flag.String("corpus", "testdata/oneroster-getroster.json",
	"JSON Schema document to check for aspect coverage")

// constructs enumerates the JSON Schema constructs a property can use. Each
// maps to the aspect name that classifies it.
//
// Names match the aspect table so that a gap reads as "this construct appears
// in the corpus and no aspect covers it".
func constructOf(prop map[string]any) string {
	if _, ok := prop["anyOf"]; ok {
		return "open enum"
	}
	if _, ok := prop["$ref"]; ok {
		return "$ref"
	}
	if enum, ok := prop["enum"].([]any); ok {
		if isBoolPair(enum) {
			return "boolean-valued enum"
		}
		return "closed enum"
	}
	if format, ok := prop["format"].(string); ok {
		switch format {
		case "date-time":
			return "format date-time"
		case "date":
			return "format date"
		}
	}
	switch typeOf(prop) {
	case "array":
		return "array of string"
	case "integer":
		return "integer"
	case "number":
		return "number"
	case "object":
		return "free-form object"
	case "string":
		return "string"
	default:
		return "unclassified:" + typeOf(prop)
	}
}

// TestCorpusAspectCoverage asserts that every construct the real schema uses is
// classified by the aspect table.
//
// A new keyword upstream fails here by name, which is the finding worth having:
// it means protoschemer is silently doing something nobody has decided on.
func TestCorpusAspectCoverage(t *testing.T) {
	t.Parallel()

	doc := loadCorpus(t)

	known := map[string]bool{"$ref": true} // $ref is structural, not an aspect
	for _, a := range aspects() {
		known[a.Name] = true
	}

	missing := map[string][]string{}
	for defName, def := range doc.Definitions {
		if def.TypeObject == nil {
			continue
		}
		for propName, prop := range def.TypeObject.Properties {
			raw := rawSchema(t, prop)
			construct := constructOf(raw)
			if !known[construct] {
				where := defName + "." + propName
				missing[construct] = append(missing[construct], where)
			}
		}
	}

	if len(missing) > 0 {
		var b strings.Builder
		for _, construct := range sortedKeys(missing) {
			sites := missing[construct]
			sort.Strings(sites)
			b.WriteString("\n  " + construct + " at " + strings.Join(sites, ", "))
		}
		t.Fatalf("corpus uses constructs no aspect classifies:%s", b.String())
	}
}

// TestCorpusRoundTrips checks that the real schema survives the whole pipeline
// and that every definition it declares comes back as a message.
//
// This is a survival check, not an equality check: the aspect table already
// records where the trip is lossy.
func TestCorpusRoundTrips(t *testing.T) {
	t.Parallel()

	source := loadCorpus(t)

	opts, err := jsonschema.NewOptions(protoPackage, "example.com/gen", "", "DType", "")
	ok(t, err)

	files, err := jsonschema.Convert([]jsonschema.Document{{Schema: source}}, opts)
	ok(t, err)

	sources, err := protoemit.Render(files)
	ok(t, err)

	recovered, err := roundtrip.Through(sources)
	ok(t, err)

	for defName, def := range source.Definitions {
		if def.TypeObject == nil || isFreeForm(def.TypeObject) {
			continue // free-form definitions become google.protobuf.Struct
		}
		message := protoPackage + "." + strings.TrimSuffix(defName, "DType")
		if _, found := recovered.Def(message); !found {
			t.Errorf("definition %q did not survive as %q", defName, message)
		}
	}
}

// loadCorpus reads and parses the corpus document.
func loadCorpus(t *testing.T) *schemalib.Schema {
	t.Helper()
	raw, err := os.ReadFile(*corpus)
	if err != nil {
		t.Skipf("corpus unavailable (%s); pass -corpus to point at one", err)
	}
	var schema schemalib.Schema
	ok(t, json.Unmarshal(raw, &schema))
	return &schema
}

// rawSchema re-encodes a parsed property so the construct classifier can read
// it as plain JSON, which is how the aspect table describes constructs.
func rawSchema(t *testing.T, prop schemalib.SchemaOrBool) map[string]any {
	t.Helper()
	if prop.TypeObject == nil {
		return map[string]any{}
	}
	encoded, err := json.Marshal(prop.TypeObject)
	ok(t, err)
	var out map[string]any
	ok(t, json.Unmarshal(encoded, &out))
	return out
}

func isFreeForm(s *schemalib.Schema) bool {
	if len(s.Properties) > 0 || s.AdditionalProperties == nil {
		return false
	}
	return s.AdditionalProperties.TypeBoolean != nil && *s.AdditionalProperties.TypeBoolean
}

func isBoolPair(enum []any) bool {
	if len(enum) != 2 {
		return false
	}
	var sawTrue, sawFalse bool
	for _, v := range enum {
		switch strings.ToLower(strings.Trim(toString(v), `"`)) {
		case "true":
			sawTrue = true
		case "false":
			sawFalse = true
		}
	}
	return sawTrue && sawFalse
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func typeOf(prop map[string]any) string {
	switch t := prop["type"].(type) {
	case string:
		return t
	case []any:
		for _, v := range t {
			if s, ok := v.(string); ok && s != "null" {
				return s
			}
		}
	}
	return ""
}

func sortedKeys(m map[string][]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
