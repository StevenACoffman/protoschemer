package roundtrip_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

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

	sources, err := protoemit.Render(files)
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

// TestExtraProperties checks the one aspect that adds a field. An open
// enumeration has no protobuf equivalent, so protoschemer splits it; if that
// companion field ever stops being emitted, extension values are silently
// dropped and nothing else would notice.
func TestExtraProperties(t *testing.T) {
	t.Parallel()

	for _, a := range aspects() {
		if len(a.ExtraProperties) == 0 {
			continue
		}
		t.Run(a.Name, func(t *testing.T) {
			t.Parallel()
			doc := trip(t, schemaWith(&a))
			qualified := protoPackage + "." + rootMessage
			names := doc.PropertyNames(qualified)
			for _, suffix := range a.ExtraProperties {
				if !anyHasSuffix(names, suffix) {
					t.Fatalf("expected a property ending in %q, got %v", suffix, names)
				}
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
		if len(a.ExtraProperties) > 0 {
			return fmt.Errorf("marked %s but adds properties %v", a.Fidelity, a.ExtraProperties)
		}
		return nil
	default:
		return fmt.Errorf("unknown fidelity %d", a.Fidelity)
	}
}
