package jsonschema_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/protoschemer/cmd"
)

const schemaDoc = `{
  "definitions": {
    "MetadataDType": {"type":"object","additionalProperties":true,"properties":{}},
    "OrgDType": {
      "type": "object",
      "description": "An organization.",
      "required": ["sourcedId","dateLastModified"],
      "properties": {
        "sourcedId": {"type":"string","description":"The identifier."},
        "dateLastModified": {"type":"string","format":"date-time"},
        "metadata": {"$ref":"#/definitions/MetadataDType"},
        "active": {"type":"string","enum":["true","false"]},
        "type": {"anyOf":[
          {"type":"string","enum":["school","district"]},
          {"type":"string","pattern":"(ext:)[a-z]+"}]}
      }
    }
  },
  "type": "object",
  "properties": {"orgs": {"type":"array","items":{"$ref":"#/definitions/OrgDType"}}}
}`

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

// run drives the real dispatcher, so the test covers flag parsing and the
// command wiring rather than only the conversion beneath it.
func run(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err = cmd.Run(context.Background(), args, strings.NewReader(""), &out, &errOut)
	return out.String(), errOut.String(), err
}

func TestJSONSchemaCommand(t *testing.T) {
	t.Parallel()

	in := t.TempDir()
	out := t.TempDir()
	ok(t, os.WriteFile(filepath.Join(in, "roster.json"), []byte(schemaDoc), 0o600))

	nameMap := filepath.Join(t.TempDir(), "names.json")
	ok(t, os.WriteFile(nameMap, []byte(`{"roster":"GetRosterResponse"}`), 0o600))

	stdout, stderr, err := run(t,
		"jsonschema",
		"--in", in,
		"--out", out,
		"--proto-package", "oneroster.v1p2.v1",
		"--go-package", "example.com/gen/oneroster/v1p2/v1",
		"--import-prefix", "oneroster/v1p2/v1",
		"--strip-suffix", "DType",
		"--name-map", nameMap,
	)
	if err != nil {
		t.Fatalf("command failed: %s\nstderr: %s", err, stderr)
	}
	if !strings.Contains(stdout, "wrote 2 files") {
		t.Fatalf("unexpected stdout: %q", stdout)
	}

	// MetadataDType is free-form, so it becomes Struct at the use site rather
	// than a file of its own; the root schema becomes the mapped message.
	entries, err := os.ReadDir(out)
	ok(t, err)
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "get_roster_response.proto,org.proto" {
		t.Fatalf("unexpected output files: %v", names)
	}

	org, err := os.ReadFile(filepath.Join(out, "org.proto"))
	ok(t, err)
	want := `syntax = "proto3";
package oneroster.v1p2.v1;
import "buf/validate/validate.proto";
import "gnostic/openapi/v3/annotations.proto";
import "google/protobuf/struct.proto";
import "google/protobuf/timestamp.proto";
option go_package = "example.com/gen/oneroster/v1p2/v1";
// An organization.
message Org {
  optional bool active = 1 [
    (gnostic.openapi.v3.property) = {
      enum: [ { yaml: "\"true\"" }, { yaml: "\"false\"" } ],
      type: "string"
    }
  ];
  google.protobuf.Timestamp date_last_modified = 2 [(buf.validate.field) = { required: true }];
  google.protobuf.Struct metadata = 3;
  // The identifier.
  string sourced_id = 4 [(buf.validate.field) = { required: true }];
  optional string type = 5 [
    (gnostic.openapi.v3.property) = {
      type: "string",
      any_of: [
        {
          schema: {
            enum: [
              { yaml: "\"school\"" },
              { yaml: "\"district\"" }
            ],
            type: "string"
          }
        },
        {
          schema: { pattern: "(ext:)[a-z]+", type: "string" }
        }
      ]
    }
  ];
}
`
	if string(org) != want {
		t.Fatalf("org.proto mismatch\nwant:\n%s\ngot:\n%s", want, org)
	}

	// The cross-file reference must produce an import derived from the prefix.
	root, err := os.ReadFile(filepath.Join(out, "get_roster_response.proto"))
	ok(t, err)
	if !strings.Contains(string(root), `import "oneroster/v1p2/v1/org.proto";`) {
		t.Fatalf("missing derived import:\n%s", root)
	}
	if !strings.Contains(string(root), "repeated Org orgs = 1;") {
		t.Fatalf("missing repeated message field:\n%s", root)
	}
}

func TestJSONSchemaCommandRejects(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		args []string
		want string
	}{
		"missing proto package": {
			args: []string{"jsonschema", "--in", t.TempDir()},
			want: "proto package is required",
		},
		"empty input directory": {
			args: []string{"jsonschema", "--in", t.TempDir(), "--proto-package", "a.v1"},
			want: "no .json documents",
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, _, err := run(t, tc.args...)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %q", tc.want, err)
			}
		})
	}
}
