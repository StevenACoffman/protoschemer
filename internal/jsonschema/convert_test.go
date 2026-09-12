package jsonschema_test

import (
	"encoding/json"
	"strings"
	"testing"

	schemalib "github.com/swaggest/jsonschema-go"

	"github.com/StevenACoffman/protoschemer/internal/jsonschema"
	"github.com/StevenACoffman/protoschemer/internal/protoemit"
	"github.com/StevenACoffman/protoschemer/internal/protoir"
)

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

func equals(t *testing.T, want, got string) {
	t.Helper()
	if want != got {
		t.Fatalf("mismatch\nwant: %s\ngot:  %s", want, got)
	}
}

// parse decodes a JSON Schema literal, so each case reads as the schema an
// author would actually write rather than as a tree of Go structs.
func parse(t *testing.T, raw string) *schemalib.Schema {
	t.Helper()
	var s schemalib.Schema
	ok(t, json.Unmarshal([]byte(raw), &s))
	return &s
}

func options(t *testing.T) *jsonschema.Options {
	t.Helper()
	opts, err := jsonschema.NewOptions("example.v1", "example.com/gen", "gen/v1/", "DType", "")
	ok(t, err)
	return opts
}

// find returns the single message produced for a document root.
func find(t *testing.T, files []protoir.File, path string) protoir.File {
	t.Helper()
	for _, f := range files {
		if f.Path == path {
			return f
		}
	}
	t.Fatalf("no file at %q; got %d files", path, len(files))
	return protoir.File{}
}

// TestConvertRules exercises each mapping rule through the exported API by
// rendering the result, so a case states the protobuf an author would expect
// rather than the intermediate representation they never see.
func TestConvertRules(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		schema string
		want   string
	}{
		"date-time becomes Timestamp": {
			schema: `{"properties":{"dateLastModified":{"type":"string","format":"date-time"}},
			          "required":["dateLastModified"]}`,
			want: "  google.protobuf.Timestamp date_last_modified = 1;\n",
		},
		"date becomes google.type.Date": {
			schema: `{"properties":{"birthDate":{"type":"string","format":"date"}},
			          "required":["birthDate"]}`,
			want: "  google.type.Date birth_date = 1;\n",
		},
		"string enum of true/false becomes bool": {
			schema: `{"properties":{"asian":{"type":"string","enum":["true","false"]}}}`,
			want:   "  optional bool asian = 1;\n",
		},
		"required field has no presence wrapper": {
			schema: `{"properties":{"sourcedId":{"type":"string"}},"required":["sourcedId"]}`,
			want:   "  string sourced_id = 1;\n",
		},
		"optional scalar gets presence": {
			schema: `{"properties":{"email":{"type":"string"}}}`,
			want:   "  optional string email = 1;\n",
		},
		"free-form object becomes Struct": {
			schema: `{"properties":{"metadata":{"type":"object","additionalProperties":true}}}`,
			want:   "  google.protobuf.Struct metadata = 1;\n",
		},
		"integer becomes int32 and number becomes double": {
			schema: `{"properties":{"count":{"type":"integer"},"ratio":{"type":"number"}},
			          "required":["count","ratio"]}`,
			want: "  int32 count = 1;\n  double ratio = 2;\n",
		},
		"array of strings becomes repeated": {
			schema: `{"properties":{"grades":{"type":"array","items":{"type":"string"}}}}`,
			want:   "  repeated string grades = 1;\n",
		},
		"extensible enum yields an enum plus a string escape hatch": {
			schema: `{"properties":{"sex":{"anyOf":[
			            {"type":"string","enum":["male","female","other"]},
			            {"type":"string","pattern":"(ext:)[a-z]+"}]}}}`,
			want: "  RootSex sex = 1;\n",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			files, err := jsonschema.Convert([]jsonschema.Document{{
				RootMessage: "Root", Schema: parse(t, tc.schema),
			}}, options(t))
			ok(t, err)

			out, err := protoemit.Render(files)
			ok(t, err)
			if len(out) != 1 {
				t.Fatalf("expected 1 file, got %d", len(out))
			}
			if !strings.Contains(out[0].Source, tc.want) {
				t.Fatalf("missing expected fields\nwant:\n%s\ngot:\n%s", tc.want, out[0].Source)
			}
		})
	}
}

// TestConvertExtensibleEnum pins the whole shape of the open-enum rule: the
// escape-hatch field, and the folding of a literal "unspecified" member onto
// protobuf's zero value.
func TestConvertExtensibleEnum(t *testing.T) {
	t.Parallel()

	files, err := jsonschema.Convert([]jsonschema.Document{{
		RootMessage: "Demographics",
		Schema: parse(t, `{"properties":{"sex":{"anyOf":[
			{"type":"string","enum":["male","female","unspecified","other"]},
			{"type":"string","pattern":"(ext:)[a-zA-Z0-9]+"}]}}}`),
	}}, options(t))
	ok(t, err)

	out, err := protoemit.Render(files)
	ok(t, err)

	want := `syntax = "proto3";
package example.v1;
option go_package = "example.com/gen";
message Demographics {
  DemographicsSex sex = 1;
  // Set instead of sex when the value falls outside the enumeration above.
  optional string sex_ext = 2;
}
enum DemographicsSex {
  // Unset, or the schema value 'unspecified'.
  DEMOGRAPHICS_SEX_UNSPECIFIED = 0;
  DEMOGRAPHICS_SEX_MALE = 1;
  DEMOGRAPHICS_SEX_FEMALE = 2;
  DEMOGRAPHICS_SEX_OTHER = 3;
}
`
	equals(t, want, out[0].Source)
}

// TestConvertPoolsDefinitions checks that a definition repeated across
// documents produces one message, and that a $ref to a free-form definition
// becomes Struct rather than an empty message.
func TestConvertPoolsDefinitions(t *testing.T) {
	t.Parallel()

	doc := `{"definitions":{
		"MetadataDType":{"type":"object","additionalProperties":true,"properties":{}},
		"OrgDType":{"type":"object","properties":{
			"sourcedId":{"type":"string"},
			"metadata":{"$ref":"#/definitions/MetadataDType"}},
			"required":["sourcedId"]}}}`

	files, err := jsonschema.Convert([]jsonschema.Document{
		{Schema: parse(t, doc)},
		{Schema: parse(t, doc)},
	}, options(t))
	ok(t, err)

	if len(files) != 1 {
		paths := make([]string, 0, len(files))
		for _, f := range files {
			paths = append(paths, f.Path)
		}
		t.Fatalf("expected 1 pooled file, got %d: %v", len(files), paths)
	}
	org := find(t, files, "gen/v1/org.proto")
	equals(t, "Org", org.Messages[0].Name)

	out, err := protoemit.Render(files)
	ok(t, err)
	if !strings.Contains(out[0].Source, "  google.protobuf.Struct metadata = 1;\n") {
		t.Fatalf("free-form $ref did not become Struct:\n%s", out[0].Source)
	}
}

func TestNewOptionsRejects(t *testing.T) {
	t.Parallel()

	cases := map[string]struct{ pkg, suffix string }{
		"empty package":                         {"", ""},
		"package with a dash":                   {"example.v-1", ""},
		"package segment starting with a digit": {"example.1v", ""},
		"non-identifier suffix":                 {"example.v1", "D Type"},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := jsonschema.NewOptions(tc.pkg, "", "", tc.suffix, "")
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			equals(t, protoir.EINVALID, protoir.ErrorCode(err))
		})
	}
}
