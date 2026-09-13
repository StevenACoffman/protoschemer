package roundtrip_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	jsonschemavalidator "github.com/santhosh-tekuri/jsonschema/v6"
	schemalib "github.com/swaggest/jsonschema-go"

	"github.com/StevenACoffman/protoschemer/internal/jsonschema"
	"github.com/StevenACoffman/protoschemer/internal/protoemit"
	"github.com/StevenACoffman/protoschemer/internal/roundtrip"
)

// protoPackage is the package every generated schema is emitted into, so a
// message Subject is recovered as "roundtrip.v1.Subject".
const protoPackage = "roundtrip.v1"

// rootMessage is the message each generated schema document produces.
const rootMessage = "Subject"

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

// tripErr sends a JSON Schema document all the way around:
// JSON Schema -> protoir -> .proto source -> descriptors -> JSON Schema.
//
// It returns an error rather than failing a test, because both *testing.T and
// *rapid.T need this and neither can accept the other's failure calls.
func tripErr(schemaJSON string) (roundtrip.Document, error) {
	var schema schemalib.Schema
	if err := json.Unmarshal([]byte(schemaJSON), &schema); err != nil {
		return roundtrip.Document{}, fmt.Errorf("parse source schema: %w", err)
	}

	opts, err := jsonschema.NewOptions(protoPackage, "example.com/gen", "", "DType", "")
	if err != nil {
		return roundtrip.Document{}, fmt.Errorf("options: %w", err)
	}

	files, err := jsonschema.Convert(
		[]jsonschema.Document{{RootMessage: rootMessage, Schema: &schema}}, opts)
	if err != nil {
		return roundtrip.Document{}, fmt.Errorf("convert: %w", err)
	}

	sources, err := protoemit.Render(context.Background(), files)
	if err != nil {
		return roundtrip.Document{}, fmt.Errorf("render: %w", err)
	}

	doc, err := roundtrip.Through(sources)
	if err != nil {
		return roundtrip.Document{}, fmt.Errorf("through: %w", err)
	}
	return doc, nil
}

// trip is tripErr for an ordinary test.
func trip(t *testing.T, schemaJSON string) roundtrip.Document {
	t.Helper()
	doc, err := tripErr(schemaJSON)
	ok(t, err)
	return doc
}

// schemaWith builds a one-property schema document exercising a single aspect.
func schemaWith(a *Aspect) string {
	required := ""
	if a.Required {
		required = `"required":["subject"],`
	}
	return `{"type":"object",` + required +
		`"properties":{"subject":` + a.Property + `}}`
}

// TestAspectContract checks that every aspect lands in the shape the table
// claims. This is the round trip's contract, stated one construct at a time.
func TestAspectContract(t *testing.T) {
	t.Parallel()

	for _, a := range aspects() {
		t.Run(a.Name, func(t *testing.T) {
			t.Parallel()

			doc := trip(t, schemaWith(&a))
			qualified := protoPackage + "." + rootMessage

			got, found := doc.Property(qualified, "subject")
			if !found {
				t.Fatalf("property %q did not survive; message has %v",
					"subject", doc.PropertyNames(qualified))
			}
			if err := a.Want(got); err != nil {
				pretty, _ := json.MarshalIndent(got, "", "  ")
				t.Fatalf("aspect %q (%s): %s\nrecovered:\n%s",
					a.Name, a.Fidelity, err, pretty)
			}
		})
	}
}

// TestLossyAspectsAreDocumented keeps the table honest: an aspect may only be
// marked Lossy with an explanation, and may only claim extra properties if it
// is Lossy. Without this, a future change could quietly downgrade fidelity.
func TestLossyAspectsAreDocumented(t *testing.T) {
	t.Parallel()

	for _, a := range aspects() {
		t.Run(a.Name, func(t *testing.T) {
			t.Parallel()
			if err := checkDocumented(&a); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestAspectMatrix prints the fidelity of every aspect. It asserts nothing; it
// exists so `go test -v -run TestAspectMatrix` answers "what survives?" without
// anyone reading the table by hand.
func TestAspectMatrix(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	fmt.Fprintf(&b, "\n%-24s %-12s %s\n", "ASPECT", "FIDELITY", "NOTES")
	for _, a := range aspects() {
		fmt.Fprintf(&b, "%-24s %-12s %s\n", a.Name, a.Fidelity, a.Loses)
	}
	t.Log(b.String())
}

// checkDocumented states the table's own invariant: only a Lossy aspect may
// explain a loss or add properties, and it must explain one.
func checkDocumented(a *Aspect) error {
	switch a.Fidelity {
	case Lossy:
		if strings.TrimSpace(a.Loses) == "" {
			return errors.New("marked Lossy but does not say what it loses")
		}
		return nil
	case Faithful, Equivalent:
		if a.Loses != "" {
			return fmt.Errorf("marked %s but claims to lose %q", a.Fidelity, a.Loses)
		}
		return nil
	default:
		return fmt.Errorf("unknown fidelity %d", a.Fidelity)
	}
}

// TestOpenEnumAcceptsExtensions validates documents against the recovered
// schema for an open enumeration.
//
// Shape alone cannot answer the question a consumer actually has: does a
// provider-specific "ext:" value still validate? That escape hatch is the whole
// reason the construct exists, and it is what a partial implementation drops.
func TestOpenEnumAcceptsExtensions(t *testing.T) {
	t.Parallel()

	doc := trip(t, `{"type":"object","required":["role"],"properties":{"role":{"anyOf":[
		{"type":"string","enum":["student","teacher"]},
		{"type":"string","pattern":"(ext:)[a-zA-Z0-9_]+"}]}}}`)

	schema := compileProperty(t, doc, protoPackage+"."+rootMessage, "role")

	cases := map[string]struct {
		value string
		valid bool
	}{
		"vocabulary value":     {`"student"`, true},
		"other vocabulary":     {`"teacher"`, true},
		"extension value":      {`"ext:paraprofessional"`, true},
		"non-conforming value": {`"bogus"`, false},
		"wrong type":           {`123`, false},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var instance any
			ok(t, json.Unmarshal([]byte(tc.value), &instance))
			err := schema.Validate(instance)
			if tc.valid && err != nil {
				t.Fatalf("%s should validate: %s", tc.value, err)
			}
			if !tc.valid && err == nil {
				t.Fatalf("%s should not validate", tc.value)
			}
		})
	}
}

// compileProperty builds a validator for one recovered property, carrying the
// document's definitions so any $ref inside it still resolves.
func compileProperty(
	t *testing.T,
	doc roundtrip.Document,
	message, property string,
) *jsonschemavalidator.Schema {
	t.Helper()

	prop, found := doc.Property(message, property)
	if !found {
		t.Fatalf("property %q not found on %q", property, message)
	}
	standalone := map[string]any{"$defs": rawDefs(t, doc)}
	for k, v := range prop {
		standalone[k] = v
	}

	encoded, err := json.Marshal(standalone)
	ok(t, err)
	parsed, err := jsonschemavalidator.UnmarshalJSON(bytes.NewReader(encoded))
	ok(t, err)

	compiler := jsonschemavalidator.NewCompiler()
	ok(t, compiler.AddResource("schema.json", parsed))
	schema, err := compiler.Compile("schema.json")
	ok(t, err)
	return schema
}

// rawDefs re-decodes the document's definitions as plain JSON values.
func rawDefs(t *testing.T, doc roundtrip.Document) map[string]any {
	t.Helper()
	var parsed struct {
		Defs map[string]any `json:"$defs"`
	}
	ok(t, json.Unmarshal(doc.Raw, &parsed))
	return parsed.Defs
}
