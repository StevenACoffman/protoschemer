// Package roundtrip sends emitted protobuf source back through
// protoc-gen-connect-openapi to recover a JSON Schema document.
//
// It exists so that tests can ask what a schema becomes after a full
// JSON Schema -> protobuf -> JSON Schema trip. That trip is deliberately lossy
// in several places, and the losses only stay deliberate if something checks
// them; this package is the instrument that makes them observable.
package roundtrip

import (
	"context"
	"encoding/json"
	"path"
	"strings"

	"github.com/bufbuild/protocompile"
	"github.com/sudorandom/protoc-gen-connect-openapi/converter"
	// Registering well-known descriptors that emitted files may import.
	_ "google.golang.org/genproto/googleapis/type/date"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	_ "google.golang.org/protobuf/types/known/structpb"
	_ "google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/pluginpb"

	"github.com/StevenACoffman/protoschemer/internal/protoir"
)

// converterParams asks the plugin for a single standalone JSON Schema document
// rather than one OpenAPI file per proto.
const converterParams = "format=jsonschema,path=schema.json"

// Document is a JSON Schema document recovered from protobuf.
//
// Definitions are keyed by fully-qualified protobuf name, so a message Org in
// package example.v1 is "example.v1.Org".
type Document struct {
	Defs map[string]map[string]any
	Raw  []byte
}

// Through compiles emitted protobuf source and converts it back to JSON Schema.
//
// Requires: files are the complete output of one protoemit.Render call, so that
// every cross-file import resolves.
// Ensures: the returned Document holds one entry per message and enum, keyed by
// fully-qualified name, including the well-known types the files import.
func Through(files []protoir.SourceFile) (Document, error) {
	const op = "roundtrip.Through"

	fds, err := compile(files)
	if err != nil {
		return Document{}, protoir.Wrap(op, err)
	}
	raw, err := convert(fds)
	if err != nil {
		return Document{}, protoir.Wrap(op, err)
	}

	var parsed struct {
		Defs map[string]map[string]any `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Document{}, protoir.Wrap(op, protoir.Errorf(protoir.EINTERNAL,
			"parse recovered schema: %s", err))
	}
	return Document{Defs: parsed.Defs, Raw: raw}, nil
}

// Def returns the definition for a fully-qualified name.
func (d Document) Def(name string) (map[string]any, bool) {
	def, ok := d.Defs[name]
	return def, ok
}

// Property returns one property of a message, with any indirection resolved.
//
// The converter renders a message-typed field as `allOf: [{$ref: ...}]` and a
// nullable enum as `oneOf: [{$ref: ...}, {type: null}]`. Both are wrappers
// around a single referent. Resolving them here keeps every caller from
// encoding the converter's rendering choices, which are not what the tests are
// about.
func (d Document) Property(message, property string) (map[string]any, bool) {
	def, ok := d.Defs[message]
	if !ok {
		return nil, false
	}
	props, ok := def["properties"].(map[string]any)
	if !ok {
		return nil, false
	}
	raw, ok := props[property].(map[string]any)
	if !ok {
		return nil, false
	}
	return d.resolve(raw), true
}

// PropertyNames returns a message's property names.
func (d Document) PropertyNames(message string) []string {
	def, ok := d.Defs[message]
	if !ok {
		return nil
	}
	props, ok := def["properties"].(map[string]any)
	if !ok {
		return nil
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	return names
}

// resolve follows a single layer of $ref, allOf, or oneOf indirection, merging
// sibling keywords such as title and description onto the referent.
func (d Document) resolve(schema map[string]any) map[string]any {
	target, ok := d.refOf(schema)
	if !ok {
		target, ok = d.unwrap(schema)
	}
	if !ok {
		return schema
	}

	merged := make(map[string]any, len(target)+len(schema))
	for k, v := range target {
		merged[k] = v
	}
	for _, keyword := range []string{"title", "description"} {
		if v, ok := schema[keyword]; ok {
			merged[keyword] = v
		}
	}
	return merged
}

// unwrap pulls the single referent out of an allOf or oneOf wrapper.
func (d Document) unwrap(schema map[string]any) (map[string]any, bool) {
	for _, keyword := range []string{"allOf", "oneOf", "anyOf"} {
		branches, ok := schema[keyword].([]any)
		if !ok {
			continue
		}
		found, ok := soleBranch(branches)
		if !ok {
			continue
		}
		if ref, isRef := d.refOf(found); isRef {
			return ref, true
		}
		return found, true
	}
	return nil, false
}

// soleBranch returns the one meaningful branch of a composition keyword.
//
// A bare {"type":"null"} branch is skipped: the converter uses it to mark a
// field optional, not to offer an alternative type. More than one real branch
// means a genuine union, which is not a wrapper and must not be collapsed.
func soleBranch(branches []any) (map[string]any, bool) {
	var found map[string]any
	for _, branch := range branches {
		b, isMap := branch.(map[string]any)
		if !isMap || b["type"] == "null" {
			continue
		}
		if found != nil {
			return nil, false
		}
		found = b
	}
	return found, found != nil
}

func (d Document) refOf(schema map[string]any) (map[string]any, bool) {
	ref, ok := schema["$ref"].(string)
	if !ok {
		return nil, false
	}
	def, ok := d.Defs[path.Base(ref)]
	return def, ok
}

// compile turns emitted source into descriptors, resolving imports from the
// emitted set first and the global registry second, which is where the
// well-known types live.
func compile(files []protoir.SourceFile) ([]protoreflect.FileDescriptor, error) {
	sources := make(map[string]string, len(files))
	paths := make([]string, 0, len(files))
	for _, f := range files {
		sources[f.Path] = f.Source
		paths = append(paths, f.Path)
	}

	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(protocompile.CompositeResolver{
			protocompile.ResolverFunc(func(p string) (protocompile.SearchResult, error) {
				src, ok := sources[p]
				if !ok {
					return protocompile.SearchResult{}, protoregistry.NotFound
				}
				return protocompile.SearchResult{Source: strings.NewReader(src)}, nil
			}),
			protocompile.ResolverFunc(func(p string) (protocompile.SearchResult, error) {
				fd, err := protoregistry.GlobalFiles.FindFileByPath(p)
				if err != nil {
					return protocompile.SearchResult{}, protoregistry.NotFound
				}
				return protocompile.SearchResult{Desc: fd}, nil
			}),
		}),
		SourceInfoMode: protocompile.SourceInfoStandard,
	}

	compiled, err := compiler.Compile(context.Background(), paths...)
	if err != nil {
		return nil, protoir.Errorf(protoir.EINVALID, "compile emitted proto: %s", err)
	}
	fds := make([]protoreflect.FileDescriptor, 0, len(compiled))
	for _, fd := range compiled {
		fds = append(fds, fd)
	}
	return fds, nil
}

// convert runs the plugin's converter over the compiled files.
//
// The request is built by hand rather than through converter.WithFiles because
// that helper only marks files declaring a service, and protoschemer emits
// message-only files, which would yield an empty document.
func convert(fds []protoreflect.FileDescriptor) ([]byte, error) {
	var protoFiles []*descriptorpb.FileDescriptorProto
	seen := make(map[string]bool)

	var walk func(fd protoreflect.FileDescriptor)
	walk = func(fd protoreflect.FileDescriptor) {
		if seen[fd.Path()] {
			return
		}
		seen[fd.Path()] = true
		imports := fd.Imports()
		for i := range imports.Len() {
			walk(imports.Get(i).FileDescriptor)
		}
		protoFiles = append(protoFiles, protodesc.ToFileDescriptorProto(fd))
	}

	toGenerate := make([]string, 0, len(fds))
	for _, fd := range fds {
		walk(fd)
		toGenerate = append(toGenerate, fd.Path())
	}

	resp, err := converter.Convert(&pluginpb.CodeGeneratorRequest{
		FileToGenerate: toGenerate,
		Parameter:      proto.String(converterParams),
		ProtoFile:      protoFiles,
	})
	if err != nil {
		return nil, protoir.Errorf(protoir.EINTERNAL, "convert to json schema: %s", err)
	}
	if resp.Error != nil {
		return nil, protoir.Errorf(protoir.EINTERNAL, "converter: %s", resp.GetError())
	}
	if len(resp.GetFile()) != 1 {
		return nil, protoir.Errorf(protoir.EINTERNAL,
			"expected 1 schema document, got %d", len(resp.GetFile()))
	}
	return []byte(resp.GetFile()[0].GetContent()), nil
}
