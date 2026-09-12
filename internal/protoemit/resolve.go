package protoemit

import (
	"github.com/jhump/protoreflect/v2/protobuilder"
	// Registering the well-known descriptors that scalarType resolves by path.
	_ "google.golang.org/genproto/googleapis/type/date"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	_ "google.golang.org/protobuf/types/known/structpb"
	_ "google.golang.org/protobuf/types/known/timestamppb"

	"github.com/StevenACoffman/protoschemer/internal/protoir"
)

// wellKnown names the file and message backing a Scalar that is really a
// well-known message type.
type wellKnown struct {
	file string
	name string
}

// resolver turns a protoir.FieldType into a protobuilder.FieldType for one file.
//
// It holds the declarations of the file currently being built alongside the
// descriptors of files already built, because protobuf spells a reference to a
// peer declaration and a reference to an imported one differently.
type resolver struct {
	index    map[string]string
	built    map[string]protoreflect.FileDescriptor
	messages map[string]*protobuilder.MessageBuilder
	enums    map[string]*protobuilder.EnumBuilder
}

// fieldType resolves one field's type.
//
// Requires: for KindMessage and KindEnum, Ref names a declaration in this file
// or in an already-built one.
// Ensures: the returned type carries the import, if any, that the reference
// implies; callers never name imports themselves.
func (r *resolver) fieldType(ft protoir.FieldType) (*protobuilder.FieldType, error) {
	switch ft.Kind {
	case protoir.KindScalar:
		return scalarType(ft.Scalar)
	case protoir.KindMessage:
		return r.messageType(ft.Ref)
	case protoir.KindEnum:
		return r.enumType(ft.Ref)
	default:
		return nil, protoir.Errorf(protoir.EINTERNAL, "unknown type kind %d", ft.Kind)
	}
}

func (r *resolver) messageType(ref string) (*protobuilder.FieldType, error) {
	if local, ok := r.messages[ref]; ok {
		return protobuilder.FieldTypeMessage(local), nil
	}
	fd, err := r.imported(ref)
	if err != nil {
		return nil, err
	}
	md := fd.Messages().ByName(protoreflect.Name(ref))
	if md == nil {
		return nil, protoir.Errorf(protoir.ENOTFOUND, "message %q not in %s", ref, fd.Path())
	}
	return protobuilder.FieldTypeImportedMessage(md), nil
}

func (r *resolver) enumType(ref string) (*protobuilder.FieldType, error) {
	if local, ok := r.enums[ref]; ok {
		return protobuilder.FieldTypeEnum(local), nil
	}
	fd, err := r.imported(ref)
	if err != nil {
		return nil, err
	}
	ed := fd.Enums().ByName(protoreflect.Name(ref))
	if ed == nil {
		return nil, protoir.Errorf(protoir.ENOTFOUND, "enum %q not in %s", ref, fd.Path())
	}
	return protobuilder.FieldTypeImportedEnum(ed), nil
}

// imported returns the already-built descriptor declaring ref. A reference that
// resolves to a file not yet built means the dependency order was wrong, which
// is this package's bug rather than the caller's.
func (r *resolver) imported(ref string) (protoreflect.FileDescriptor, error) {
	path, known := r.index[ref]
	if !known {
		return nil, protoir.Errorf(protoir.ENOTFOUND, "undeclared type %q", ref)
	}
	fd, ready := r.built[path]
	if !ready {
		return nil, protoir.Errorf(protoir.EINTERNAL,
			"%q referenced before %s was built", ref, path)
	}
	return fd, nil
}

// scalarType maps a protoir scalar onto protobuf's type system. The well-known
// message types come from the global descriptor registry, populated by the
// blank imports above.
func scalarType(s protoir.Scalar) (*protobuilder.FieldType, error) {
	switch s {
	case protoir.ScalarString:
		return protobuilder.FieldTypeString(), nil
	case protoir.ScalarInt32:
		return protobuilder.FieldTypeInt32(), nil
	case protoir.ScalarDouble:
		return protobuilder.FieldTypeDouble(), nil
	case protoir.ScalarBool:
		return protobuilder.FieldTypeBool(), nil
	case protoir.ScalarTimestamp:
		return wellKnownType(wellKnown{
			file: "google/protobuf/timestamp.proto", name: "Timestamp",
		})
	case protoir.ScalarDate:
		return wellKnownType(wellKnown{
			file: "google/type/date.proto", name: "Date",
		})
	case protoir.ScalarStruct:
		return wellKnownType(wellKnown{
			file: "google/protobuf/struct.proto", name: "Struct",
		})
	default:
		return nil, protoir.Errorf(protoir.EINTERNAL, "unknown scalar %d", s)
	}
}

func wellKnownType(wk wellKnown) (*protobuilder.FieldType, error) {
	fd, err := protoregistry.GlobalFiles.FindFileByPath(wk.file)
	if err != nil {
		return nil, protoir.Errorf(protoir.EINTERNAL, "load %s: %s", wk.file, err)
	}
	md := fd.Messages().ByName(protoreflect.Name(wk.name))
	if md == nil {
		return nil, protoir.Errorf(protoir.EINTERNAL, "%s not in %s", wk.name, wk.file)
	}
	return protobuilder.FieldTypeImportedMessage(md), nil
}
