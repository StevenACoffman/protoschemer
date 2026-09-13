package protoemit

import (
	"context"

	validate "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"github.com/bufbuild/protocompile"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/StevenACoffman/protoschemer/internal/protoemit/gnosticdefs"
	"github.com/StevenACoffman/protoschemer/internal/protoir"
)

// annotator turns a protoir.SourceForm into an OpenAPI property option.
//
// It exists because a protobuf type and the schema it came from do not always
// serialize alike: a calendar date modelled as google.type.Date is an object on
// the wire, while the source called it an ISO string. The annotation records
// what the source said so generated documentation can describe that contract.
//
// The annotation is documentation only. Protobuf JSON serialization ignores it,
// so it is truthful only where something translates between the two forms.
type annotator struct {
	// property is the extension descriptor for gnostic.openapi.v3.property.
	property protoreflect.ExtensionDescriptor
	// schema is the message descriptor for its value.
	schema protoreflect.MessageDescriptor
}

// newAnnotator compiles the embedded annotation schema into a descriptor that
// is local to this call, never reaching the global registry.
//
// Requires: nothing.
// Ensures: the returned annotator can build a property option, or an error
// explains why the embedded schema could not be loaded.
func newAnnotator(ctx context.Context) (*annotator, error) {
	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(protocompile.ResolverFunc(
			func(path string) (protocompile.SearchResult, error) {
				file, err := gnosticdefs.FS.Open(path)
				if err != nil {
					return protocompile.SearchResult{}, protoregistry.NotFound
				}
				return protocompile.SearchResult{Source: file}, nil
			})),
	}

	files, err := compiler.Compile(ctx, gnosticdefs.AnnotationsPath)
	if err != nil {
		return nil, protoir.Errorf(protoir.EINTERNAL,
			"compile %s: %s", gnosticdefs.AnnotationsPath, err)
	}
	property := files[0].Extensions().ByName(gnosticdefs.PropertyExtension)
	if property == nil {
		return nil, protoir.Errorf(protoir.EINTERNAL,
			"%s declares no %q extension", gnosticdefs.AnnotationsPath,
			gnosticdefs.PropertyExtension)
	}
	return &annotator{property: property, schema: property.Message()}, nil
}

// fieldOptions renders a field's source facts as protobuf field options.
//
// Requires: f carries at least one of SourceForm or SourceRequired, which
// annotates reports.
// Ensures: the returned options carry a gnostic.openapi.v3.property whose
// type, format, and enum are exactly what the source schema declared.
func (a *annotator) fieldOptions(f *protoir.Field) (*descriptorpb.FieldOptions, error) {
	options := &descriptorpb.FieldOptions{}
	if f.SourceForm != nil {
		value, err := a.schemaValue(f.SourceForm)
		if err != nil {
			return nil, err
		}
		proto.SetExtension(options, dynamicpb.NewExtensionType(a.property), value)
	}
	if f.SourceRequired {
		// Proto3 dropped required, so the source's requiredness is carried as a
		// validation rule. protoc-gen-connect-openapi reads it and restores the
		// parent's required list, which is otherwise lost entirely.
		proto.SetExtension(options, validate.E_Field, &validate.FieldRules{
			Required: proto.Bool(true),
		})
	}
	return options, nil
}

// schemaValue renders one SourceForm as an OpenAPI schema message. It recurses
// for a union, since each alternative is a schema in its own right.
func (a *annotator) schemaValue(form *protoir.SourceForm) (*dynamicpb.Message, error) {
	value := dynamicpb.NewMessage(a.schema)

	if err := a.setString(value, "type", form.Type); err != nil {
		return nil, err
	}
	if err := a.setString(value, "format", form.Format); err != nil {
		return nil, err
	}
	if err := a.setString(value, "pattern", form.Pattern); err != nil {
		return nil, err
	}
	if err := a.setEnum(value, form.Enum); err != nil {
		return nil, err
	}
	if err := a.setAnyOf(value, form.AnyOf); err != nil {
		return nil, err
	}
	return value, nil
}

// setAnyOf assigns the alternative shapes of a union.
//
// Each alternative is a SchemaOrReference, a one-of whose schema member carries
// the branch. Only that member is used: the source dialect describes branches
// inline, never by reference.
func (a *annotator) setAnyOf(value *dynamicpb.Message, branches []protoir.SourceForm) error {
	if len(branches) == 0 {
		return nil
	}
	field := a.schema.Fields().ByName("any_of")
	if field == nil {
		return protoir.Errorf(protoir.EINTERNAL, "annotation schema has no any_of field")
	}

	list := value.Mutable(field).List()
	for i := range branches {
		branch, err := a.schemaValue(&branches[i])
		if err != nil {
			return err
		}
		entry := list.NewElement()
		member := entry.Message().Descriptor().Fields().ByName("schema")
		if member == nil {
			return protoir.Errorf(protoir.EINTERNAL,
				"annotation SchemaOrReference has no schema member")
		}
		entry.Message().Set(member, protoreflect.ValueOfMessage(branch))
		list.Append(entry)
	}
	value.Set(field, protoreflect.ValueOfList(list))
	return nil
}

// setString assigns a scalar field of the annotation schema, skipping empties
// so an unset source keyword does not become an empty string in the output.
func (a *annotator) setString(value *dynamicpb.Message, name, text string) error {
	if text == "" {
		return nil
	}
	field := a.schema.Fields().ByName(protoreflect.Name(name))
	if field == nil {
		return protoir.Errorf(protoir.EINTERNAL,
			"annotation schema has no %q field", name)
	}
	value.Set(field, protoreflect.ValueOfString(text))
	return nil
}

// setEnum assigns the permitted values.
//
// Each member is an Any whose yaml member carries the literal text. Quoting is
// deliberate: an unquoted "true" parses as a YAML boolean, which would document
// a JSON boolean for a source that specified the string "true".
func (a *annotator) setEnum(value *dynamicpb.Message, members []string) error {
	if len(members) == 0 {
		return nil
	}
	field := a.schema.Fields().ByName("enum")
	if field == nil {
		return protoir.Errorf(protoir.EINTERNAL, "annotation schema has no enum field")
	}

	list := value.Mutable(field).List()
	for _, member := range members {
		entry := list.NewElement()
		yaml := entry.Message().Descriptor().Fields().ByName("yaml")
		if yaml == nil {
			return protoir.Errorf(protoir.EINTERNAL, "annotation Any has no yaml field")
		}
		entry.Message().Set(yaml, protoreflect.ValueOfString(quoteYAML(member)))
		list.Append(entry)
	}
	value.Set(field, protoreflect.ValueOfList(list))
	return nil
}

// quoteYAML wraps a value so YAML reads it as the string the source wrote.
func quoteYAML(value string) string { return `"` + value + `"` }
