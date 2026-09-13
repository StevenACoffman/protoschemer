package jsonschema

import (
	"strings"

	"github.com/ettle/strcase"
)

// unspecifiedSuffix is the protobuf convention for an enum's zero value.
const unspecifiedSuffix = "_UNSPECIFIED"

// messageName converts a JSON Schema definition name into a protobuf message
// name, dropping the dialect's type suffix when one was configured
// ("AcademicSessionDType" -> "AcademicSession").
//
// A name that is already PascalCase is kept verbatim. Re-casing it would lose
// information the author encoded: case conversion cannot tell an acronym from a
// word, so "AcadSessionGUIDRef" would come back as "AcadSessionGuidRef".
func (o *Options) messageName(definition string) string {
	trimmed := definition
	if o.stripSuffix != "" {
		trimmed = strings.TrimSuffix(definition, o.stripSuffix)
		if trimmed == "" {
			trimmed = definition
		}
	}
	return pascal(trimmed)
}

// pascal converts s to PascalCase, keeping it verbatim when it already is.
// Case conversion cannot tell an acronym from a word, so re-casing a name the
// author already capitalised would turn "AcadSessionGUIDRef" into
// "AcadSessionGuidRef".
func pascal(s string) string {
	if isPascalCase(s) {
		return s
	}
	return strcase.ToPascal(s)
}

// isPascalCase reports whether s is already a usable protobuf type name: an
// upper-case letter followed by letters and digits only.
func isPascalCase(s string) bool {
	if s == "" || s[0] < 'A' || s[0] > 'Z' {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}

// filePath is where a message's file lives, and equally how other files import
// it, since protobuf resolves imports from the module root.
func (o *Options) filePath(message string) string {
	return o.importPrefix + strcase.ToSnake(message) + ".proto"
}

// fieldName converts a JSON property name to protobuf's lower_snake_case.
func fieldName(property string) string { return strcase.ToSnake(property) }

// enumName names the enum generated for one property of one message. It is
// qualified by its owner because protobuf enums are file-scoped, so a bare
// "Status" would collide across messages.
func enumName(owner, property string) string {
	return pascal(owner) + pascal(property)
}

// enumValueName prefixes a value with its enum's name, which protobuf style
// requires because enum values share their enclosing file's scope.
func enumValueName(enum, value string) string {
	return strcase.ToSNAKE(enum) + "_" + strcase.ToSNAKE(value)
}

// enumZeroName is the name of the synthesized zero value.
func enumZeroName(enum string) string { return strcase.ToSNAKE(enum) + unspecifiedSuffix }
