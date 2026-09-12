// Package jsonschema converts JSON Schema documents into the protoir
// intermediate representation.
//
// Parsing JSON Schema itself is delegated to github.com/swaggest/jsonschema-go.
// What lives here is the part no library can supply: the choice of how a schema
// keyword should be represented in protobuf's type system.
package jsonschema

import (
	"strings"

	schemalib "github.com/swaggest/jsonschema-go"

	"github.com/StevenACoffman/protoschemer/internal/protoir"
)

// Options are the settings a conversion needs that the schema cannot supply.
//
// Construct one with NewOptions: a value obtained that way is already valid, so
// Convert carries no validation branches of its own.
type Options struct {
	pkg          string
	goPackage    string
	importPrefix string
	stripSuffix  string
	header       string
}

// Document is one parsed JSON Schema document awaiting conversion.
type Document struct {
	// Schema is the parsed document.
	Schema *schemalib.Schema
	// RootMessage names the message built from the document's root schema.
	// Empty means the document contributes only its definitions, which is the
	// common case for a document that exists to be referenced.
	RootMessage string
}

// NewOptions validates conversion settings and returns them as a value Convert
// can consume without further checking.
//
// Requires: pkg is a dotted protobuf package name.
// Ensures: returns EINVALID naming the offending setting, or an Options whose
// every field is usable as-is.
func NewOptions(pkg, goPackage, importPrefix, stripSuffix, header string) (*Options, error) {
	if pkg == "" {
		return nil, protoir.Errorf(protoir.EINVALID, "proto package is required")
	}
	for _, segment := range strings.Split(pkg, ".") {
		if !isIdentifier(segment) {
			return nil, protoir.Errorf(protoir.EINVALID,
				"proto package %q has invalid segment %q", pkg, segment)
		}
	}
	if stripSuffix != "" && !isIdentifier(stripSuffix) {
		return nil, protoir.Errorf(protoir.EINVALID,
			"strip suffix %q is not an identifier", stripSuffix)
	}
	if importPrefix != "" && !strings.HasSuffix(importPrefix, "/") {
		importPrefix += "/"
	}
	return &Options{
		pkg:          pkg,
		goPackage:    goPackage,
		importPrefix: importPrefix,
		stripSuffix:  stripSuffix,
		header:       header,
	}, nil
}

// isIdentifier reports whether s is a non-empty run of letters, digits, and
// underscores that does not start with a digit.
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case r >= '0' && r <= '9' && i > 0:
		default:
			return false
		}
	}
	return true
}
