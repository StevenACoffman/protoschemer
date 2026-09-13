package protoemit_test

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/StevenACoffman/protoschemer/internal/protoemit"
	"github.com/StevenACoffman/protoschemer/internal/protoir"
)

const testPackage = "example.v1"

var update = flag.Bool("update", false, "rewrite golden files from actual output")

func ok(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

func equals(t *testing.T, want, got string) {
	t.Helper()
	if want != got {
		t.Fatalf("mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// golden compares actual against testdata/<name>.golden, rewriting it when
// -update is passed so the output can be reviewed by eye and then committed.
func golden(t *testing.T, name, actual string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		ok(t, os.MkdirAll("testdata", 0o750))
		ok(t, os.WriteFile(path, []byte(actual), 0o600))
		return
	}
	want, err := os.ReadFile(filepath.Clean(path))
	ok(t, err)
	equals(t, string(want), actual)
}

func TestRender(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		files []protoir.File
	}{
		// Every scalar the IR can name, including the three well-known message
		// types, plus repeated and proto3-optional presence.
		"scalars": {files: []protoir.File{{
			Path: "example/v1/scalars.proto", Package: testPackage,
			GoPackage: "example.com/gen/example/v1",
			Header:    "Code generated. DO NOT EDIT.",
			Messages: []protoir.Message{{
				Name: "Scalars", Comment: "Every scalar shape.",
				Fields: []protoir.Field{
					{
						Name:    "name",
						Number:  1,
						Comment: "A required string.",
						Type: protoir.FieldType{
							Kind:   protoir.KindScalar,
							Scalar: protoir.ScalarString,
						},
					},
					{
						Name:     "count",
						Number:   2,
						Optional: true,
						Type: protoir.FieldType{
							Kind:   protoir.KindScalar,
							Scalar: protoir.ScalarInt32,
						},
					},
					{
						Name:   "ratio",
						Number: 3,
						Type: protoir.FieldType{
							Kind:   protoir.KindScalar,
							Scalar: protoir.ScalarDouble,
						},
					},
					{
						Name:     "enabled",
						Number:   4,
						Optional: true,
						Type: protoir.FieldType{
							Kind:   protoir.KindScalar,
							Scalar: protoir.ScalarBool,
						},
					},
					{
						Name:   "updated_at",
						Number: 5,
						Type: protoir.FieldType{
							Kind:   protoir.KindScalar,
							Scalar: protoir.ScalarTimestamp,
						},
					},
					{
						Name:   "born_on",
						Number: 6,
						Type: protoir.FieldType{
							Kind:   protoir.KindScalar,
							Scalar: protoir.ScalarDate,
						},
					},
					{
						Name:   "metadata",
						Number: 7,
						Type: protoir.FieldType{
							Kind:   protoir.KindScalar,
							Scalar: protoir.ScalarStruct,
						},
					},
					{
						Name:     "tags",
						Number:   8,
						Repeated: true,
						Type: protoir.FieldType{
							Kind:   protoir.KindScalar,
							Scalar: protoir.ScalarString,
						},
					},
				},
			}},
		}}},

		// The zero value is synthesized, not supplied by the caller.
		"enum": {files: []protoir.File{{
			Path: "example/v1/enum.proto", Package: testPackage,
			Enums: []protoir.Enum{{
				Name: "Status", Comment: "Lifecycle status.",
				ZeroName: "STATUS_UNSPECIFIED", ZeroComment: "Unset, or the value 'unknown'.",
				Values: []protoir.EnumValue{
					{Name: "STATUS_ACTIVE", Number: 1, Comment: "In use."},
					{Name: "STATUS_TO_BE_DELETED", Number: 2},
				},
			}},
			Messages: []protoir.Message{{
				Name: "Thing",
				Fields: []protoir.Field{
					{
						Name: "status", Number: 1,
						Type: protoir.FieldType{Kind: protoir.KindEnum, Ref: "Status"},
					},
				},
			}},
		}}},

		// Two files, declared in reverse dependency order, to prove the sort
		// runs and that the import line is derived rather than declared.
		"cross_file": {files: []protoir.File{
			{
				Path: "example/v1/user.proto", Package: testPackage,
				Messages: []protoir.Message{{
					Name: "User",
					Fields: []protoir.Field{
						{
							Name: "org", Number: 1,
							Type: protoir.FieldType{Kind: protoir.KindMessage, Ref: "Org"},
						},
						{
							Name: "roles", Number: 2, Repeated: true,
							Type: protoir.FieldType{Kind: protoir.KindEnum, Ref: "Role"},
						},
					},
				}},
			},
			{
				Path: "example/v1/org.proto", Package: testPackage,
				Enums: []protoir.Enum{{
					Name: "Role", ZeroName: "ROLE_UNSPECIFIED",
					Values: []protoir.EnumValue{{Name: "ROLE_ADMIN", Number: 1}},
				}},
				Messages: []protoir.Message{{
					Name: "Org",
					Fields: []protoir.Field{
						{
							Name:   "sourced_id",
							Number: 1,
							Type: protoir.FieldType{
								Kind:   protoir.KindScalar,
								Scalar: protoir.ScalarString,
							},
						},
					},
				}},
			},
		}},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, err := protoemit.Render(context.Background(), tc.files)
			ok(t, err)

			var combined strings.Builder
			for _, f := range out {
				combined.WriteString("// === " + f.Path + " ===\n" + f.Source)
			}
			golden(t, name, combined.String())
		})
	}
}

func TestRenderRejects(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		files    []protoir.File
		wantCode string
	}{
		"import cycle": {
			wantCode: protoir.EINVALID,
			files: []protoir.File{
				{
					Path: "a.proto", Package: testPackage,
					Messages: []protoir.Message{{Name: "A", Fields: []protoir.Field{
						{
							Name: "b", Number: 1,
							Type: protoir.FieldType{Kind: protoir.KindMessage, Ref: "B"},
						},
					}}},
				},
				{
					Path: "b.proto", Package: testPackage,
					Messages: []protoir.Message{{Name: "B", Fields: []protoir.Field{
						{
							Name: "a", Number: 1,
							Type: protoir.FieldType{Kind: protoir.KindMessage, Ref: "A"},
						},
					}}},
				},
			},
		},
		"duplicate declaration": {
			wantCode: protoir.ECONFLICT,
			files: []protoir.File{
				{Path: "a.proto", Package: testPackage, Messages: []protoir.Message{{Name: "Dup"}}},
				{Path: "b.proto", Package: testPackage, Messages: []protoir.Message{{Name: "Dup"}}},
			},
		},
		"unknown reference": {
			wantCode: protoir.ENOTFOUND,
			files: []protoir.File{{
				Path: "a.proto", Package: testPackage,
				Messages: []protoir.Message{{Name: "A", Fields: []protoir.Field{
					{
						Name: "missing", Number: 1,
						Type: protoir.FieldType{Kind: protoir.KindMessage, Ref: "Nope"},
					},
				}}},
			}},
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := protoemit.Render(context.Background(), tc.files)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			equals(t, tc.wantCode, protoir.ErrorCode(err))
		})
	}
}
