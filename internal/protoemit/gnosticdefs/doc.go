// Package gnosticdefs embeds the OpenAPI v3 annotation schema published as the
// buf module buf.build/gnostic/gnostic.
//
// The files are embedded rather than imported from a Go module because both
// github.com/google/gnostic-models and the generated buf SDK register protobuf
// extension number 1143, so linking either alongside a consumer of the other
// panics at init. Compiling these into a descriptor local to this process
// avoids the global registry entirely.
//
// The buf spelling is deliberate: protoc-gen-connect-openapi's own test corpus
// imports "gnostic/openapi/v3/annotations.proto", so an emitted file naming
// that path is the one that resolves for callers.
package gnosticdefs

import "embed"

// AnnotationsPath is the file declaring the property extension.
const AnnotationsPath = "gnostic/openapi/v3/annotations.proto"

// PropertyExtension is the field option carrying a per-field OpenAPI schema.
const PropertyExtension = "property"

// FS holds the annotation schema, rooted so that a path inside it is the same
// path an emitted .proto uses in its import statement.
//
//go:embed gnostic/openapi/v3/*.proto
var FS embed.FS
