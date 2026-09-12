package jsonschema

import (
	"fmt"
	"strconv"
	"strings"

	schemalib "github.com/swaggest/jsonschema-go"

	"github.com/StevenACoffman/protoschemer/internal/protoir"
)

// extMarker is the prefix JSON Schema dialects conventionally use to allow a
// value outside a closed enumeration. A branch whose pattern mentions it marks
// the enumeration as open rather than closed.
const extMarker = "ext:"

// openEnum reports the closed values of an extensible enumeration.
//
// The shape is anyOf[{enum: [...]}, {pattern: "(ext:)..."}]: a fixed vocabulary
// plus an escape hatch for values the publisher did not anticipate. Protobuf
// has no equivalent, so the caller emits both an enum and a string field;
// modelling it as the enum alone would silently drop every extension value.
func openEnum(s *schemalib.Schema) ([]string, bool) {
	if len(s.AnyOf) == 0 {
		return nil, false
	}
	var values []string
	extensible := false
	for _, branch := range s.AnyOf {
		b := branch.TypeObject
		if b == nil {
			continue
		}
		if len(b.Enum) > 0 {
			values = enumStrings(b.Enum)
		}
		if b.Pattern != nil && strings.Contains(*b.Pattern, extMarker) {
			extensible = true
		}
	}
	if !extensible || len(values) == 0 {
		return nil, false
	}
	return values, true
}

// closedEnum reports the values of a plain enumeration.
func closedEnum(s *schemalib.Schema) ([]string, bool) {
	if len(s.Enum) == 0 {
		return nil, false
	}
	return enumStrings(s.Enum), true
}

// isBoolEnum reports whether values are a dialect's spelling of a boolean.
// Emitting `bool` rather than a two-member enum keeps the generated Go usable:
// `if d.Asian` instead of comparing against a generated constant.
func isBoolEnum(values []string) bool {
	const pair = 2
	if len(values) != pair {
		return false
	}
	var sawTrue, sawFalse bool
	for _, v := range values {
		switch strings.ToLower(v) {
		case "true":
			sawTrue = true
		case "false":
			sawFalse = true
		default:
			return false
		}
	}
	return sawTrue && sawFalse
}

// formatScalar maps JSON Schema's `format` onto the well-known types that carry
// the same meaning, so consumers get a real instant or calendar date rather
// than a string they must parse.
func formatScalar(s *schemalib.Schema) (protoir.Scalar, bool) {
	if s.Format == nil {
		return protoir.ScalarString, false
	}
	switch *s.Format {
	case "date-time":
		return protoir.ScalarTimestamp, true
	case "date":
		return protoir.ScalarDate, true
	default:
		return protoir.ScalarString, false
	}
}

// isFreeForm reports whether a schema is an open bag of arbitrary JSON: it
// permits additional properties and declares none of its own. Such a schema
// becomes google.protobuf.Struct, because emitting an empty message would
// discard every value it is meant to carry.
func isFreeForm(s *schemalib.Schema) bool {
	if len(s.Properties) > 0 {
		return false
	}
	if s.AdditionalProperties == nil {
		return false
	}
	return s.AdditionalProperties.TypeBoolean != nil && *s.AdditionalProperties.TypeBoolean
}

// plainScalar maps a JSON primitive type onto a protobuf scalar. An unknown or
// absent type becomes a string, which is the representation least likely to
// lose information.
func plainScalar(s *schemalib.Schema) protoir.Scalar {
	if s.Type == nil || s.Type.SimpleTypes == nil {
		return protoir.ScalarString
	}
	switch *s.Type.SimpleTypes {
	case schemalib.Integer:
		return protoir.ScalarInt32
	case schemalib.Number:
		return protoir.ScalarDouble
	case schemalib.Boolean:
		return protoir.ScalarBool
	case schemalib.String, schemalib.Array, schemalib.Object, schemalib.Null:
		return protoir.ScalarString
	default:
		return protoir.ScalarString
	}
}

// refName returns the definition name a $ref points at.
func refName(s *schemalib.Schema) (string, bool) {
	if s.Ref == nil {
		return "", false
	}
	const prefix = "#/definitions/"
	if !strings.HasPrefix(*s.Ref, prefix) {
		return "", false
	}
	name := strings.TrimPrefix(*s.Ref, prefix)
	if name == "" || strings.Contains(name, "/") {
		return "", false
	}
	return name, true
}

// isArray reports whether the schema describes a list.
func isArray(s *schemalib.Schema) bool {
	return s.Type != nil && s.Type.SimpleTypes != nil && *s.Type.SimpleTypes == schemalib.Array
}

// itemSchema returns the schema of an array's elements.
func itemSchema(s *schemalib.Schema) *schemalib.Schema {
	if s.Items == nil || s.Items.SchemaOrBool == nil {
		return nil
	}
	return s.Items.SchemaOrBool.TypeObject
}

// enumStrings renders enum members as strings. JSON Schema permits any JSON
// value, but protobuf enum values are identifiers, so every member is named by
// its textual form.
func enumStrings(values []any) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, stringify(v))
	}
	return out
}

// stringify renders one decoded JSON value as the text an identifier is derived
// from. encoding/json decodes every number as float64, so integers are printed
// without a fractional part to keep "1" from becoming "1_0".
func stringify(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case nil:
		return "null"
	default:
		return fmt.Sprint(t)
	}
}
