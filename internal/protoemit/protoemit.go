// Package protoemit renders a protoir intermediate representation as protobuf
// source text.
//
// It delegates every part of that job that is not specific to this application
// to github.com/jhump/protoreflect/v2: descriptor construction, import
// resolution, comment placement, and formatting. In exchange, output is
// guaranteed to be legal proto3 — a descriptor that cannot be built is reported
// as an error rather than written out as text that only protoc will reject.
package protoemit

import (
	"context"
	"sort"

	"github.com/jhump/protoreflect/v2/protobuilder"
	"github.com/jhump/protoreflect/v2/protoprint"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/StevenACoffman/protoschemer/internal/protoir"
)

// Render converts an intermediate representation into protobuf source text.
//
// Requires: every KindMessage or KindEnum FieldType names a declaration that
// exists somewhere in files, and no two files share a path.
//
// Ensures: one SourceFile per input File, ordered by Path; each Source is
// proto3 that compiles against the others, with imports derived from actual
// cross-file references rather than declared by the caller.
func Render(ctx context.Context, files []protoir.File) ([]protoir.SourceFile, error) {
	const op = "protoemit.Render"

	index, err := symbolIndex(files)
	if err != nil {
		return nil, protoir.Wrap(op, err)
	}
	ordered, err := inDependencyOrder(files, index)
	if err != nil {
		return nil, protoir.Wrap(op, err)
	}

	notes, err := newAnnotator(ctx)
	if err != nil {
		return nil, protoir.Wrap(op, err)
	}

	built := make(map[string]protoreflect.FileDescriptor, len(ordered))
	rendered := make([]protoir.SourceFile, 0, len(ordered))
	printer := protoprint.Printer{Compact: true}

	for i := range ordered {
		file := &ordered[i]
		fd, buildErr := buildFile(file, index, built, notes)
		if buildErr != nil {
			return nil, protoir.Wrap(op, buildErr)
		}
		built[file.Path] = fd

		source, printErr := printer.PrintProtoToString(fd)
		if printErr != nil {
			return nil, protoir.Wrap(op, protoir.Errorf(protoir.EINTERNAL,
				"print %s: %s", file.Path, printErr))
		}
		rendered = append(rendered, protoir.SourceFile{Path: file.Path, Source: source})
	}

	sort.Slice(rendered, func(i, j int) bool { return rendered[i].Path < rendered[j].Path })
	return rendered, nil
}

// symbolIndex maps each declared message and enum name to the file declaring
// it. Two declarations of one name are ECONFLICT: protobuf would reject the
// result, and silently preferring one would lose a field's type.
func symbolIndex(files []protoir.File) (map[string]string, error) {
	index := make(map[string]string)
	for i := range files {
		file := &files[i]
		names := make([]string, 0, len(file.Messages)+len(file.Enums))
		for _, m := range file.Messages {
			names = append(names, m.Name)
		}
		for _, e := range file.Enums {
			names = append(names, e.Name)
		}
		for _, name := range names {
			if prior, seen := index[name]; seen {
				return nil, protoir.Errorf(protoir.ECONFLICT,
					"%q is declared in both %s and %s", name, prior, file.Path)
			}
			index[name] = file.Path
		}
	}
	return index, nil
}

// buildFile constructs one file's descriptor. Declarations are created before
// any field is added so that fields may refer to peers in the same file
// regardless of declaration order.
func buildFile(
	file *protoir.File,
	index map[string]string,
	built map[string]protoreflect.FileDescriptor,
	notes *annotator,
) (protoreflect.FileDescriptor, error) {
	fb := protobuilder.NewFile(file.Path).SetPackageName(protoreflect.FullName(file.Package))
	fb.Syntax = protoreflect.Proto3
	if file.GoPackage != "" {
		fb.SetOptions(&descriptorpb.FileOptions{GoPackage: proto.String(file.GoPackage)})
	}
	if file.Header != "" {
		fb.SyntaxComments = protobuilder.Comments{LeadingComment: comment(file.Header)}
	}

	res := &resolver{
		index:    index,
		built:    built,
		messages: make(map[string]*protobuilder.MessageBuilder, len(file.Messages)),
		enums:    make(map[string]*protobuilder.EnumBuilder, len(file.Enums)),
	}
	for i := range file.Enums {
		e := &file.Enums[i]
		eb := buildEnum(e)
		res.enums[e.Name] = eb
		fb.AddEnum(eb)
	}
	for _, m := range file.Messages {
		mb := protobuilder.NewMessage(protoreflect.Name(m.Name))
		if m.Comment != "" {
			mb.SetComments(protobuilder.Comments{LeadingComment: comment(m.Comment)})
		}
		res.messages[m.Name] = mb
		fb.AddMessage(mb)
	}

	if err := addFields(file, res, notes); err != nil {
		return nil, err
	}

	fd, err := fb.Build()
	if err != nil {
		return nil, protoir.Errorf(protoir.EINVALID, "build %s: %s", file.Path, err)
	}
	return fd, nil
}

// addFields populates every message in file, now that all declarations exist.
func addFields(file *protoir.File, res *resolver, notes *annotator) error {
	for _, m := range file.Messages {
		mb := res.messages[m.Name]
		for i := range m.Fields {
			f := &m.Fields[i]
			fldb, err := buildField(f, res, notes)
			if err != nil {
				return protoir.Wrap(m.Name+"."+f.Name, err)
			}
			mb.AddField(fldb)
		}
	}
	return nil
}

// annotates reports whether a field carries any source fact worth recording as
// a protobuf option.
func annotates(f *protoir.Field) bool {
	return f.SourceForm != nil || f.SourceRequired
}

// buildField constructs one field, including the annotation that records the
// shape its source schema described.
func buildField(
	f *protoir.Field,
	res *resolver,
	notes *annotator,
) (*protobuilder.FieldBuilder, error) {
	ft, err := res.fieldType(f.Type)
	if err != nil {
		return nil, err
	}
	fldb := protobuilder.NewField(protoreflect.Name(f.Name), ft).
		SetNumber(protoreflect.FieldNumber(f.Number))
	if f.Comment != "" {
		fldb.SetComments(protobuilder.Comments{LeadingComment: comment(f.Comment)})
	}
	switch {
	case f.Repeated:
		fldb.SetRepeated()
	case f.Optional:
		fldb.SetProto3Optional(true)
	}
	if annotates(f) {
		opts, optErr := notes.fieldOptions(f)
		if optErr != nil {
			return nil, optErr
		}
		fldb.SetOptions(opts)
	}
	return fldb, nil
}

// buildEnum constructs an enum, synthesizing the zero value protobuf requires.
func buildEnum(e *protoir.Enum) *protobuilder.EnumBuilder {
	eb := protobuilder.NewEnum(protoreflect.Name(e.Name))
	if e.Comment != "" {
		eb.SetComments(protobuilder.Comments{LeadingComment: comment(e.Comment)})
	}
	zero := protobuilder.NewEnumValue(protoreflect.Name(e.ZeroName)).SetNumber(0)
	if e.ZeroComment != "" {
		zero.SetComments(protobuilder.Comments{LeadingComment: comment(e.ZeroComment)})
	}
	eb.AddValue(zero)
	for _, v := range e.Values {
		vb := protobuilder.NewEnumValue(protoreflect.Name(v.Name)).
			SetNumber(protoreflect.EnumNumber(v.Number))
		if v.Comment != "" {
			vb.SetComments(protobuilder.Comments{LeadingComment: comment(v.Comment)})
		}
		eb.AddValue(vb)
	}
	return eb
}

// comment adapts plain text to the leading space protoc writes after "//".
func comment(text string) string { return " " + text }
