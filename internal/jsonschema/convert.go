package jsonschema

import (
	"sort"

	schemalib "github.com/swaggest/jsonschema-go"

	"github.com/StevenACoffman/protoschemer/internal/protoir"
)

// converter carries the settings and the cross-definition facts that building
// any single message needs.
type converter struct {
	freeForm map[string]bool
	opts     *Options
}

// propertyInput is everything converting one property depends on. It carries no
// field number: numbering is assigned centrally in message, so that no rule can
// disagree with another about how many numbers it consumed.
type propertyInput struct {
	schema   *schemalib.Schema
	owner    string
	property string
	required bool
}

// propertyOutput is what one property contributes. It is a slice of fields
// rather than a single field because an extensible enumeration yields two: the
// closed enum and the string escape hatch that carries anything outside it.
type propertyOutput struct {
	fields []protoir.Field
	enums  []protoir.Enum
}

// Convert turns parsed JSON Schema documents into an intermediate
// representation ready for emission.
//
// Definitions are pooled across documents, so a type that several documents
// declare identically produces one message rather than one per document.
//
// Requires: opts came from NewOptions; every document has a non-nil Schema.
// Ensures: one File per message, deterministically ordered; enums live in the
// file of the message that induced them; a $ref to a free-form definition
// becomes google.protobuf.Struct rather than an empty message.
func Convert(docs []Document, opts *Options) ([]protoir.File, error) {
	const op = "jsonschema.Convert"

	pool := poolDefinitions(docs)
	conv := &converter{opts: opts, freeForm: freeFormNames(pool)}

	files := make([]protoir.File, 0, len(pool)+len(docs))
	for _, name := range sortedKeys(pool) {
		if conv.freeForm[name] {
			continue // becomes google.protobuf.Struct at each use site
		}
		file, err := conv.file(opts.messageName(name), pool[name])
		if err != nil {
			return nil, protoir.Wrap(op, err)
		}
		files = append(files, file)
	}

	for i := range docs {
		doc := &docs[i]
		if doc.RootMessage == "" {
			continue
		}
		file, err := conv.file(doc.RootMessage, doc.Schema)
		if err != nil {
			return nil, protoir.Wrap(op, err)
		}
		files = append(files, file)
	}

	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

// file builds the complete File for one message.
func (c *converter) file(name string, s *schemalib.Schema) (protoir.File, error) {
	msg, enums, err := c.message(name, s)
	if err != nil {
		return protoir.File{}, err
	}
	return protoir.File{
		Path:      c.opts.filePath(name),
		Package:   c.opts.pkg,
		GoPackage: c.opts.goPackage,
		Header:    c.opts.header,
		Messages:  []protoir.Message{msg},
		Enums:     enums,
	}, nil
}

// message builds one message and the enums its properties induce.
//
// Properties are visited in sorted order and numbered here rather than by the
// individual rules, so field numbers depend only on the set of properties: a
// schema edit elsewhere in the document must not renumber unrelated fields,
// because a renumbered field is a wire-incompatible change.
func (c *converter) message(
	name string,
	s *schemalib.Schema,
) (protoir.Message, []protoir.Enum, error) {
	required := make(map[string]bool, len(s.Required))
	for _, r := range s.Required {
		required[r] = true
	}

	msg := protoir.Message{Name: name, Comment: text(s.Description)}
	var enums []protoir.Enum
	var number int32

	for _, prop := range sortedProperties(s.Properties) {
		built, err := c.property(propertyInput{
			owner:    name,
			property: prop,
			schema:   s.Properties[prop].TypeObject,
			required: required[prop],
		})
		if err != nil {
			return protoir.Message{}, nil, err
		}
		for i := range built.fields {
			number++
			built.fields[i].Number = number
		}
		msg.Fields = append(msg.Fields, built.fields...)
		enums = append(enums, built.enums...)
	}
	return msg, enums, nil
}

// property converts one JSON Schema property.
//
// The rules are tried in a fixed order, most specific first; each returns as
// soon as it matches, so no rule can shadow an earlier one.
func (c *converter) property(in propertyInput) (propertyOutput, error) {
	if in.schema == nil {
		return propertyOutput{}, protoir.Errorf(protoir.EINVALID,
			"%s.%s: property has no schema", in.owner, in.property)
	}
	if values, open := openEnum(in.schema); open {
		return c.openEnumProperty(in, values), nil
	}
	if values, closed := closedEnum(in.schema); closed {
		return c.enumProperty(in, values), nil
	}
	if isArray(in.schema) {
		return c.arrayProperty(in), nil
	}
	ft := c.singularType(in.schema)
	return propertyOutput{fields: []protoir.Field{{
		Name:           fieldName(in.property),
		Comment:        text(in.schema.Description),
		Type:           ft,
		SourceForm:     sourceFormFor(ft, in.schema),
		SourceRequired: in.required,
		Optional:       !in.required && !ft.HasImplicitPresence(),
	}}}, nil
}

// sourceFormFor records the shape a source schema declared, when the protobuf
// type chosen for it serializes differently.
//
// Only google.type.Date differs: protojson renders it as {year,month,day}
// while the source called it an ISO string. Timestamp already renders as the
// RFC 3339 string that format: date-time described, and Struct already renders
// as the free-form object, so neither needs recording.
func sourceFormFor(ft protoir.FieldType, s *schemalib.Schema) *protoir.SourceForm {
	if ft.Kind != protoir.KindScalar || ft.Scalar != protoir.ScalarDate {
		return nil
	}
	form := &protoir.SourceForm{Type: "string", Format: "date"}
	if s.Format != nil {
		form.Format = *s.Format
	}
	return form
}

// singularType resolves a non-array, non-enum schema to a field type.
func (c *converter) singularType(s *schemalib.Schema) protoir.FieldType {
	if ref, isRef := refName(s); isRef {
		if c.freeForm[ref] {
			return protoir.FieldType{Kind: protoir.KindScalar, Scalar: protoir.ScalarStruct}
		}
		return protoir.FieldType{Kind: protoir.KindMessage, Ref: c.opts.messageName(ref)}
	}
	if scalar, matched := formatScalar(s); matched {
		return protoir.FieldType{Kind: protoir.KindScalar, Scalar: scalar}
	}
	if isFreeForm(s) {
		return protoir.FieldType{Kind: protoir.KindScalar, Scalar: protoir.ScalarStruct}
	}
	return protoir.FieldType{Kind: protoir.KindScalar, Scalar: plainScalar(s)}
}

// arrayProperty converts a list property by resolving its element schema. An
// array with no declared element schema becomes repeated string, matching
// plainScalar's choice of the least lossy representation.
func (c *converter) arrayProperty(in propertyInput) propertyOutput {
	comment := text(in.schema.Description)
	items := itemSchema(in.schema)
	if items == nil {
		return repeatedField(fieldName(in.property), comment,
			protoir.FieldType{Kind: protoir.KindScalar, Scalar: protoir.ScalarString},
			nil, in.required)
	}

	elem := in
	elem.schema = items
	if values, closed := closedEnum(items); closed && !isBoolEnum(values) {
		enum := c.buildEnum(elem, values)
		return repeatedField(fieldName(in.property), comment,
			protoir.FieldType{Kind: protoir.KindEnum, Ref: enum.Name},
			[]protoir.Enum{enum}, in.required)
	}
	return repeatedField(fieldName(in.property), comment,
		c.singularType(items), nil, in.required)
}

// enumProperty converts a closed enumeration, collapsing the two-valued
// "true"/"false" spelling to a real bool.
func (c *converter) enumProperty(in propertyInput, values []string) propertyOutput {
	comment := text(in.schema.Description)
	if isBoolEnum(values) {
		return propertyOutput{fields: []protoir.Field{{
			Name:    fieldName(in.property),
			Comment: comment,
			Type:    protoir.FieldType{Kind: protoir.KindScalar, Scalar: protoir.ScalarBool},
			// The source spelled this as two strings, and protojson renders a
			// bool as a JSON boolean, so the original spelling is recorded.
			SourceForm:     &protoir.SourceForm{Type: "string", Enum: values},
			SourceRequired: in.required,
			Optional:       !in.required,
		}}}
	}
	enum := c.buildEnum(in, values)
	return propertyOutput{
		enums: []protoir.Enum{enum},
		fields: []protoir.Field{{
			Name:    fieldName(in.property),
			Comment: comment,
			Type:    protoir.FieldType{Kind: protoir.KindEnum, Ref: enum.Name},
			// Protobuf requires identifier-shaped, prefixed value names, so the
			// source vocabulary is recorded to keep it documentable.
			SourceForm:     &protoir.SourceForm{Type: "string", Enum: values},
			SourceRequired: in.required,
			Optional:       !in.required,
		}},
	}
}

// openEnumProperty converts an extensible enumeration to a string.
//
// The source describes a fixed vocabulary together with an escape hatch for
// values the publisher did not foresee. Protobuf enums are closed, so modelling
// the vocabulary as one would need a second field to carry the extensions, and
// a single source property would become two. A string keeps the property whole;
// the annotation carries the vocabulary and the escape hatch for documentation.
func (c *converter) openEnumProperty(in propertyInput, values []string) propertyOutput {
	return propertyOutput{fields: []protoir.Field{{
		Name:           fieldName(in.property),
		Comment:        text(in.schema.Description),
		Type:           protoir.FieldType{Kind: protoir.KindScalar, Scalar: protoir.ScalarString},
		SourceForm:     openEnumForm(in.schema, values),
		SourceRequired: in.required,
		Optional:       !in.required,
	}}}
}

// openEnumForm records the union the source declared: the closed vocabulary,
// and the pattern that admits anything outside it.
func openEnumForm(s *schemalib.Schema, values []string) *protoir.SourceForm {
	branches := []protoir.SourceForm{{Type: "string", Enum: values}}
	if pattern, found := extPattern(s); found {
		branches = append(branches, protoir.SourceForm{Type: "string", Pattern: pattern})
	}
	return &protoir.SourceForm{Type: "string", AnyOf: branches}
}

// buildEnum names an enum and its members.
//
// A member whose name collides with the synthesized zero value is folded onto
// it rather than renamed: a schema that spells a value "unspecified" means the
// same thing protobuf's zero value means, and emitting both would force callers
// to treat two constants as one.
func (c *converter) buildEnum(in propertyInput, values []string) protoir.Enum {
	name := enumName(in.owner, in.property)
	zero := enumZeroName(name)

	enum := protoir.Enum{
		Name:     name,
		Comment:  text(in.schema.Description),
		ZeroName: zero,
	}
	var number int32
	for _, v := range values {
		if enumValueName(name, v) == zero {
			enum.ZeroComment = "Unset, or the schema value " + quote(v) + "."
			continue
		}
		number++
		enum.Values = append(enum.Values, protoir.EnumValue{
			Name:   enumValueName(name, v),
			Number: number,
		})
	}
	return enum
}

// repeatedField builds the single-field output shared by every array rule.
//
// A required list is recorded like any other required property. The generated
// documentation then marks it required, which is what the source said. Note
// that a validator reading the same annotation at runtime treats an empty list
// as unset, so it enforces non-empty where the source only demanded presence.
func repeatedField(
	name, comment string,
	ft protoir.FieldType,
	enums []protoir.Enum,
	required bool,
) propertyOutput {
	return propertyOutput{
		enums: enums,
		fields: []protoir.Field{{
			Name:           name,
			Comment:        comment,
			Type:           ft,
			SourceRequired: required,
			Repeated:       true,
		}},
	}
}
