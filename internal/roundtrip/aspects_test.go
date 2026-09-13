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
//
// Two limits on how far a rating here can be read.
//
// It describes the schema round trip only. Several aspects reach Faithful
// because protoschemer annotates the field with the shape its source declared,
// and protobuf JSON serialization ignores those annotations entirely. A
// Faithful rating says the generated document matches the source contract,
// never that a protojson service emits that shape.
//
// It also describes a property's own schema, not what its parent says about it.
// Requiredness lives on the parent, and the differential tests cover it
// separately; a rating here is silent on whether a property survived as
// required.
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
			// Protobuf enum values must be prefixed identifiers, so the
			// generated enum spells them ORG_STATUS_ACTIVE. The annotation
			// restores the source vocabulary for the document.
			Name:     "closed enum",
			Property: `{"type":"string","enum":["active","tobedeleted"]}`,
			Required: true,
			Fidelity: Faithful,
			Want:     both(hasType("string"), hasEnumValues("active", "tobedeleted")),
		},
		{
			// The field is a real bool so generated Go reads `if x.Active`.
			// The annotation restores the two strings the source declared.
			Name:     "boolean-valued enum",
			Property: `{"type":"string","enum":["true","false"]}`,
			Required: true,
			Fidelity: Faithful,
			Want:     both(hasType("string"), hasEnumValues("true", "false")),
		},
		{
			// The field is a google.type.Date, whose protojson form is an
			// object. The annotation restores the ISO string the source
			// declared, so the document describes the source contract.
			Name:     "format date",
			Property: `{"type":"string","format":"date"}`,
			Required: true,
			Fidelity: Faithful,
			Want:     both(hasType("string"), hasKeyword("format", "date")),
		},
		{
			// Protobuf enums are closed, so modelling the vocabulary as one
			// would need a second field for the escape hatch. A string keeps
			// the property whole and the annotation carries both alternatives.
			Name: "open enum",
			Property: `{"anyOf":[{"type":"string","enum":["school","district"]},` +
				`{"type":"string","pattern":"(ext:)[a-z]+"}]}`,
			Required: true,
			Fidelity: Faithful,
			Want:     hasAnyOfBranches([]string{"school", "district"}, "ext:"),
		},
	}
}
