package roundtrip_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/StevenACoffman/protoschemer/internal/roundtrip"
)

// The properties below generate whole schema documents by choosing a set of
// aspects, rather than by generating JSON Schema freely. Two reasons: an
// arbitrary schema mostly exercises constructs protoschemer does not claim to
// handle, and rapid shrinks a list of aspects into the smallest set that still
// fails, which names the culprit directly.

// aspectSelection draws a non-empty set of distinct aspects.
func aspectSelection() *rapid.Generator[[]Aspect] {
	all := aspects()
	return rapid.SliceOfNDistinct(
		rapid.SampledFrom(all), 1, len(all),
		func(a Aspect) string { return a.Name },
	)
}

// document renders a schema whose properties are the chosen aspects, one each.
func document(chosen []Aspect) (schemaJSON string, names []string) {
	props := make([]string, 0, len(chosen))
	required := make([]string, 0, len(chosen))
	names = make([]string, 0, len(chosen))

	for i, a := range chosen {
		name := fmt.Sprintf("p%d", i)
		names = append(names, name)
		props = append(props, `"`+name+`":`+a.Property)
		if a.Required {
			required = append(required, `"`+name+`"`)
		}
	}

	requiredClause := ""
	if len(required) > 0 {
		requiredClause = `"required":[` + strings.Join(required, ",") + `],`
	}
	return `{"type":"object",` + requiredClause +
		`"properties":{` + strings.Join(props, ",") + `}}`, names
}

// TestPropertyEveryAspectSurvives checks that no combination of aspects causes
// a property to vanish. A dropped field is the failure mode that silently
// loses data, so it is checked independently of any per-aspect shape.
func TestPropertyEveryAspectSurvives(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(t *rapid.T) {
		chosen := aspectSelection().Draw(t, "aspects")
		schemaJSON, names := document(chosen)

		doc := tripRapid(t, schemaJSON)
		qualified := protoPackage + "." + rootMessage
		got := doc.PropertyNames(qualified)

		for _, name := range names {
			if _, found := doc.Property(qualified, name); !found {
				t.Fatalf("property %q did not survive; message has %v", name, got)
			}
		}
	})
}

// TestPropertyAspectContractHolds checks that each aspect keeps its documented
// shape no matter which other aspects share the message. An aspect whose
// behaviour depends on its neighbours would be a naming or numbering bug.
func TestPropertyAspectContractHolds(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(t *rapid.T) {
		chosen := aspectSelection().Draw(t, "aspects")
		schemaJSON, names := document(chosen)

		doc := tripRapid(t, schemaJSON)
		qualified := protoPackage + "." + rootMessage

		for i, a := range chosen {
			got, found := doc.Property(qualified, names[i])
			if !found {
				t.Fatalf("aspect %q: property %q did not survive", a.Name, names[i])
			}
			if err := a.Want(got); err != nil {
				pretty, _ := json.MarshalIndent(got, "", "  ")
				t.Fatalf("aspect %q (%s): %s\nrecovered:\n%s",
					a.Name, a.Fidelity, err, pretty)
			}
		}
	})
}

// TestPropertyRoundTripIsIdempotent checks that a second trip changes nothing.
//
// The first trip is where the deliberate losses happen. Every trip after it
// should be a fixed point; if it is not, the pipeline is still drifting, and
// repeated regeneration would keep changing the output.
func TestPropertyRoundTripIsIdempotent(t *testing.T) {
	t.Parallel()

	rapid.Check(t, func(t *rapid.T) {
		chosen := aspectSelection().Draw(t, "aspects")
		schemaJSON, _ := document(chosen)

		first := tripRapid(t, schemaJSON)
		second := tripRapid(t, schemaJSON)

		if !bytes.Equal(first.Raw, second.Raw) {
			t.Fatalf("round trip is not deterministic\nfirst:\n%s\nsecond:\n%s",
				first.Raw, second.Raw)
		}
	})
}

// tripRapid is trip() for a *rapid.T, which is not a *testing.T and so cannot
// use the t.Helper-based helpers.
func tripRapid(t *rapid.T, schemaJSON string) roundtrip.Document {
	doc, err := tripErr(schemaJSON)
	if err != nil {
		t.Fatalf("round trip failed for %s: %s", schemaJSON, err)
	}
	return doc
}
