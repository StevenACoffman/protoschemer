package roundtrip_test

// This file is the contract. Each Aspect records one JSON Schema construct,
// what protoschemer turns it into, and what survives the trip back through
// protoc-gen-connect-openapi.
//
// Fidelity is stated per aspect rather than asserted globally, because the
// round trip is deliberately lossy in three places. Writing "equal" would hide
// exactly the facts this suite exists to keep visible.
const (
	// Faithful means the recovered schema says the same thing as the original,
	// allowing for $ref indirection and added titles.
	Faithful Fidelity = iota
	// Equivalent means the recovered schema accepts the same JSON documents,
	// while spelling the constraint differently.
	Equivalent
	// Lossy means the recovered schema accepts different JSON. The Loses field
	// says what changed and why the trade was taken.
	Lossy
)

// Fidelity classifies how much of an aspect survives a round trip.
type Fidelity int

// Aspect is one JSON Schema construct under test.
type Aspect struct {
	// Name identifies the aspect in failure output.
	Name string
	// Property is the JSON Schema fragment for a single property, as it would
	// appear inside "properties".
	Property string
	// Required marks the property as required in the source schema.
	Required bool
	// Want checks the recovered property schema. It receives the schema with
	// $ref and allOf indirection already resolved.
	Want func(got map[string]any) error
	// Fidelity records how much survives.
	Fidelity Fidelity
	// Loses explains the difference. Required when Fidelity is Lossy.
	Loses string
	// ExtraProperties are property names the round trip adds beyond the one
	// named by the aspect, which only an open enumeration does.
	ExtraProperties []string
}

func (f Fidelity) String() string {
	switch f {
	case Faithful:
		return "faithful"
	case Equivalent:
		return "equivalent"
	case Lossy:
		return "lossy"
	default:
		return "unknown"
	}
}

// aspects is the full set of JSON Schema constructs protoschemer claims to
// handle. The corpus test asserts that the real OneRoster schema uses nothing
// outside this list.
func aspects() []Aspect {
	return []Aspect{
		{
			Name:     "string",
			Property: `{"type":"string"}`,
			Required: true,
			Fidelity: Faithful,
			Want:     hasType("string"),
		},
		{
			Name:     "integer",
			Property: `{"type":"integer"}`,
			Required: true,
			Fidelity: Faithful,
			Want:     hasType("integer"),
		},
		{
			Name:     "number",
			Property: `{"type":"number"}`,
			Required: true,
			Fidelity: Faithful,
			Want:     hasType("number"),
		},
		{
			Name:     "array of string",
			Property: `{"type":"array","items":{"type":"string"}}`,
			Required: true,
			Fidelity: Faithful,
			Want:     isArrayOf("string"),
		},
		{
			Name:     "description",
			Property: `{"type":"string","description":"A described field."}`,
			Required: true,
			Fidelity: Faithful,
			Want:     hasKeyword("description", "A described field."),
		},
		{
			Name:     "format date-time",
			Property: `{"type":"string","format":"date-time"}`,
			Required: true,
			Fidelity: Faithful,
			Want:     both(hasType("string"), hasKeyword("format", "date-time")),
		},
		{
			// The source says additionalProperties:true; the recovered schema
			// says additionalProperties:{any JSON value}. Both accept any
			// object, so the constraint is the same one worded differently.
			Name:     "free-form object",
			Property: `{"type":"object","additionalProperties":true}`,
			Required: true,
			Fidelity: Equivalent,
			Want:     both(hasType("object"), hasAdditionalProperties()),
		},
		{
			// Optionality survives, but as a nullable type rather than by
			// omission from "required". A consumer checking presence gets the
			// same answer; a consumer reading "required" does not.
			Name:     "optional scalar",
			Property: `{"type":"string"}`,
			Required: false,
			Fidelity: Equivalent,
			Want:     isNullable("string"),
		},
		{
			// Protobuf enum values must be identifiers unique within their
			// package, so "active" becomes ORG_STATUS_ACTIVE, and protobuf
			// requires a zero value that the source schema never had.
			Name:     "closed enum",
			Property: `{"type":"string","enum":["active","tobedeleted"]}`,
			Required: true,
			Fidelity: Lossy,
			Loses: "enum values are renamed to protobuf identifiers and a zero " +
				"value is added; the original spellings are not recoverable",
			Want: both(hasType("string"), hasEnumSuffixes("ACTIVE", "TOBEDELETED")),
		},
		{
			// protoschemer maps this to a real bool on purpose: a two-member
			// string enum is how a schema spells a boolean for a binding that
			// lacks one, and callers want `if x.Active`.
			Name:     "boolean-valued enum",
			Property: `{"type":"string","enum":["true","false"]}`,
			Required: true,
			Fidelity: Lossy,
			Loses: "becomes a JSON boolean rather than the strings \"true\" and " +
				"\"false\"; chosen so generated Go exposes a bool",
			Want: hasType("boolean"),
		},
		{
			// google.type.Date is semantically right for a calendar date, but
			// its JSON form is an object, not an ISO string.
			Name:     "format date",
			Property: `{"type":"string","format":"date"}`,
			Required: true,
			Fidelity: Lossy,
			Loses: `becomes {"year","month","day"} rather than "YYYY-MM-DD"; ` +
				"chosen because google.type.Date models a calendar date exactly",
			Want: both(hasType("object"), hasProperties("year", "month", "day")),
		},
		{
			// Protobuf has no open enumeration, so the vocabulary and the
			// escape hatch become two fields. No value is lost, but the shape
			// a consumer reads is different.
			Name: "open enum",
			Property: `{"anyOf":[{"type":"string","enum":["school","district"]},` +
				`{"type":"string","pattern":"(ext:)[a-z]+"}]}`,
			Required:        true,
			Fidelity:        Lossy,
			Loses:           "splits into a closed enum plus a companion <name>Ext string",
			ExtraProperties: []string{"Ext"},
			Want:            both(hasType("string"), hasEnumSuffixes("SCHOOL", "DISTRICT")),
		},
	}
}
