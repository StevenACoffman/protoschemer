package roundtrip_test

import (
	"bytes"
	"encoding/json"
	"testing"

	jsonschemavalidator "github.com/santhosh-tekuri/jsonschema/v6"
)

// The tests here compare the documents a source schema accepts against the
// documents its round-tripped form accepts.
//
// The aspect table checks each property's own shape, which cannot see anything
// stated about a property by its parent. "required" is exactly that, and it was
// silently dropped for every field until a differential check found it. Shape
// assertions could not have caught it, and neither could a passing suite.

// compileSchema builds a validator for a complete JSON Schema document.
func compileSchema(t *testing.T, document []byte) *jsonschemavalidator.Schema {
	t.Helper()
	parsed, err := jsonschemavalidator.UnmarshalJSON(bytes.NewReader(document))
	ok(t, err)
	compiler := jsonschemavalidator.NewCompiler()
	ok(t, compiler.AddResource("schema.json", parsed))
	schema, err := compiler.Compile("schema.json")
	ok(t, err)
	return schema
}

// recoveredSchema returns the round-tripped root message as a standalone
// document, carrying the definitions so any $ref inside it resolves.
func recoveredSchema(t *testing.T, sourceSchema string) *jsonschemavalidator.Schema {
	t.Helper()

	doc := trip(t, sourceSchema)
	var parsed struct {
		Defs map[string]any `json:"$defs"`
	}
	ok(t, json.Unmarshal(doc.Raw, &parsed))

	root, found := parsed.Defs[protoPackage+"."+rootMessage].(map[string]any)
	if !found {
		t.Fatalf("round trip produced no %q", rootMessage)
	}
	standalone := map[string]any{"$defs": parsed.Defs}
	for keyword, value := range root {
		standalone[keyword] = value
	}
	encoded, err := json.Marshal(standalone)
	ok(t, err)
	return compileSchema(t, encoded)
}

// TestDifferentialRequired checks that a property the source required is still
// required after the round trip.
//
// Proto3 has no required, so this holds only because protoschemer emits a
// protovalidate rule the converter reads back. Without it every required
// property silently becomes optional.
func TestDifferentialRequired(t *testing.T) {
	t.Parallel()

	const source = `{"type":"object","required":["name","when","terms"],"properties":{
		"name":{"type":"string"},
		"when":{"type":"string","format":"date"},
		"terms":{"type":"array","items":{"type":"string"}},
		"note":{"type":"string"}}}`

	recovered := recoveredSchema(t, source)

	cases := map[string]struct {
		instance string
		valid    bool
	}{
		"all required present":   {`{"name":"n","when":"2024-01-15","terms":["t"]}`, true},
		"optional absent is ok":  {`{"name":"n","when":"2024-01-15","terms":["t"]}`, true},
		"required scalar absent": {`{"when":"2024-01-15","terms":["t"]}`, false},
		"required date absent":   {`{"name":"n","terms":["t"]}`, false},
		"required list absent":   {`{"name":"n","when":"2024-01-15"}`, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var instance any
			ok(t, json.Unmarshal([]byte(tc.instance), &instance))

			err := recovered.Validate(instance)
			if tc.valid && err != nil {
				t.Fatalf("%s should validate: %s", tc.instance, err)
			}
			if !tc.valid && err == nil {
				t.Fatalf("%s should not validate: a required property is missing", tc.instance)
			}
		})
	}
}

// TestDifferentialOptionalAdmitsNull documents a difference the round trip does
// not close.
//
// JSON Schema tells absent apart from present-and-null. Protobuf presence does
// not, and the converter renders an optional scalar as a nullable type, so the
// recovered schema accepts an explicit null the source rejects. Nothing on the
// field changes that rendering, so this is pinned rather than fixed: if it ever
// starts matching, the rendering changed and the note below is stale.
func TestDifferentialOptionalAdmitsNull(t *testing.T) {
	t.Parallel()

	const source = `{"type":"object","properties":{"email":{"type":"string"}}}`

	sourceSchema := compileSchema(t, []byte(source))
	recovered := recoveredSchema(t, source)

	var explicitNull any
	ok(t, json.Unmarshal([]byte(`{"email":null}`), &explicitNull))

	if err := sourceSchema.Validate(explicitNull); err == nil {
		t.Fatal("source schema should reject an explicit null for a string property")
	}
	if err := recovered.Validate(explicitNull); err != nil {
		t.Fatalf("recovered schema unexpectedly rejects an explicit null: %s", err)
	}
}
