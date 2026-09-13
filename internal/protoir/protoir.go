// Package protoir defines the intermediate representation that sits between a
// schema dialect and emitted protobuf source.
//
// It is the shared language of the application: schema readers produce a
// []File, and emitters consume one. It holds plain data only and imports
// nothing from the rest of the application, so a new input dialect or a new
// output form can be added without either learning about the other.
package protoir

// Scalar values. ScalarTimestamp, ScalarDate, and ScalarStruct name well-known
// messages rather than protobuf scalars; the emitter resolves them to
// google.protobuf.Timestamp, google.type.Date, and google.protobuf.Struct and
// adds the corresponding import.
const (
	ScalarString Scalar = iota
	ScalarInt32
	ScalarDouble
	ScalarBool
	ScalarTimestamp
	ScalarDate
	ScalarStruct
)

// TypeKind values.
const (
	KindScalar TypeKind = iota
	KindMessage
	KindEnum
)

// Scalar identifies a protobuf leaf type: either a true scalar or one of the
// well-known message types this IR treats as a leaf, because callers choose
// them the same way they choose an int32 and never name their fields.
type Scalar uint8

// TypeKind discriminates the FieldType union.
type TypeKind uint8

// FieldType is the type of a single field. Kind selects which other member
// carries meaning: KindScalar reads Scalar, KindMessage and KindEnum read Ref.
type FieldType struct {
	// Ref names a Message or Enum declared somewhere in the same []File. It is
	// the bare name, not package-qualified; the emitter resolves it.
	Ref    string
	Kind   TypeKind
	Scalar Scalar
}

// SourceForm describes the JSON shape a field had in the source schema, for a
// field whose protobuf type serializes differently.
//
// It records what the source said, not how to express it. Choosing a way to
// carry that into generated documentation is the emitter's job, which keeps
// this package free of any output dialect.
//
// A SourceForm documents the source contract, not the protobuf wire format.
// Protobuf JSON serialization ignores it entirely, so it is only truthful when
// something translates between the two at the edge.
//
// It describes one field and holds only the keywords needed to document that
// field. It is deliberately not a general schema type: a reader that needs to
// express more should narrow it to these terms rather than widen this struct,
// which would end in a second implementation of JSON Schema living here.
type SourceForm struct {
	// Type is the source's JSON type, such as "string".
	Type string
	// Format is the source's format keyword, such as "date".
	Format string
	// Pattern is the source's regular expression constraint.
	Pattern string
	// Enum is the permitted values, spelled as the source spelled them.
	Enum []string
	// AnyOf holds alternative shapes, for a source that described a union. A
	// value satisfying any one of them satisfies the field.
	AnyOf []SourceForm
}

// Field is one field of a Message.
type Field struct {
	// Name is already in protobuf's lower_snake_case form.
	Name string
	// Comment is the leading comment, unwrapped and without a "//" prefix.
	Comment string
	// SourceForm is set when the protobuf type does not serialize the way the
	// source schema described. Nil means the two already agree.
	SourceForm *SourceForm
	Type       FieldType
	// SourceRequired records that the source schema listed this field among its
	// parent's required members.
	//
	// Proto3 has no required, so this cannot be expressed as a field label. It
	// is kept because a reader of the generated documentation still needs to
	// know, and an emitter can carry it as a validation annotation.
	SourceRequired bool
	// Number is the field's wire tag; it must be unique within its Message.
	Number int32
	// Repeated and Optional are mutually exclusive: protobuf has no
	// "optional repeated", because an empty list already encodes absence.
	Repeated bool
	// Optional requests explicit presence tracking (proto3 optional).
	Optional bool
}

// EnumValue is one member of an Enum.
type EnumValue struct {
	Name    string
	Comment string
	Number  int32
}

// Enum is a top-level protobuf enum. Values must not include the zero value;
// the emitter synthesizes it, since protobuf requires the first value be zero
// and every dialect spells that differently.
type Enum struct {
	Name string
	// ZeroName is the name of the synthesized zero value.
	ZeroName    string
	ZeroComment string
	Comment     string
	Values      []EnumValue
}

// Message is a top-level protobuf message.
type Message struct {
	Name    string
	Comment string
	Fields  []Field
}

// File is one emitted .proto file.
type File struct {
	// Path is the file's path relative to the protobuf module root, which is
	// also how other files import it: "oneroster/v1p2/v1/demographics.proto".
	Path string
	// Package is the protobuf package, shared by every File in one run.
	Package string
	// GoPackage becomes option go_package.
	GoPackage string
	// Header is a leading comment placed at the top of the file.
	Header   string
	Messages []Message
	Enums    []Enum
}

// SourceFile is emitted protobuf source together with the path it belongs at.
type SourceFile struct {
	Path   string
	Source string
}

// IsMessageBacked reports whether a Scalar is really a message type. The
// distinction matters for presence: a message-typed field already tells unset
// apart from zero, so marking one optional adds a wrapper that means nothing.
func (s Scalar) IsMessageBacked() bool {
	switch s {
	case ScalarTimestamp, ScalarDate, ScalarStruct:
		return true
	case ScalarString, ScalarInt32, ScalarDouble, ScalarBool:
		return false
	default:
		return false
	}
}

// HasImplicitPresence reports whether the type distinguishes unset from zero on
// its own. Only a true scalar needs an explicit presence marker.
func (t FieldType) HasImplicitPresence() bool {
	switch t.Kind {
	case KindMessage:
		return true
	case KindScalar:
		return t.Scalar.IsMessageBacked()
	case KindEnum:
		return false
	default:
		return false
	}
}
