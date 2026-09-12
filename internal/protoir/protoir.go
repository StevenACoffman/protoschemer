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

// Field is one field of a Message.
type Field struct {
	// Name is already in protobuf's lower_snake_case form.
	Name string
	// Comment is the leading comment, unwrapped and without a "//" prefix.
	Comment string
	Type    FieldType
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
