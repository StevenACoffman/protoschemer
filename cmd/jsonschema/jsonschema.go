// Package jsonschema implements the "jsonschema" CLI command.
package jsonschema

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ettle/strcase"
	"github.com/peterbourgon/ff/v4"
	schemalib "github.com/swaggest/jsonschema-go"

	"github.com/StevenACoffman/protoschemer/cmd/root"
	"github.com/StevenACoffman/protoschemer/internal/jsonschema"
	"github.com/StevenACoffman/protoschemer/internal/protoemit"
	"github.com/StevenACoffman/protoschemer/internal/protoir"
)

// dirPerm is the mode for directories created to hold generated output.
const dirPerm fs.FileMode = 0o750

// filePerm is the mode for generated files. They are derived artifacts that
// callers regenerate rather than edit, so they need no group or world access.
const filePerm fs.FileMode = 0o600

// Config holds the configuration for the jsonschema command.
type Config struct {
	*root.Config
	Flags   *ff.FlagSet
	Command *ff.Command

	In           string
	Out          string
	Package      string
	GoPackage    string
	ImportPrefix string
	StripSuffix  string
	Header       string
	NameMap      string
	DryRun       bool
}

// New creates and registers the jsonschema command with the given parent config.
func New(parent *root.Config) *Config {
	var cfg Config
	cfg.Config = parent
	cfg.Flags = ff.NewFlagSet("jsonschema").SetParent(parent.Flags)
	cfg.Flags.StringVar(&cfg.In, 'i', "in", ".",
		"directory holding the JSON Schema documents to convert")
	cfg.Flags.StringVar(&cfg.Out, 'o', "out", ".",
		"directory to write .proto files into")
	cfg.Flags.StringVar(&cfg.Package, 'p', "proto-package", "",
		"protobuf package for the generated files (required)")
	cfg.Flags.StringVar(&cfg.GoPackage, 0, "go-package", "",
		"value for the generated option go_package")
	cfg.Flags.StringVar(&cfg.ImportPrefix, 0, "import-prefix", "",
		"path of the output directory relative to the protobuf module root, "+
			"used to build import statements")
	cfg.Flags.StringVar(&cfg.StripSuffix, 0, "strip-suffix", "",
		"type-name suffix to drop when naming messages, e.g. DType")
	cfg.Flags.StringVar(&cfg.Header, 0, "header", "",
		"leading comment placed at the top of every generated file")
	cfg.Flags.StringVar(&cfg.NameMap, 0, "name-map", "",
		"JSON file mapping a document's base name to the message name for its "+
			"root schema; documents absent from it contribute only definitions")
	cfg.Flags.BoolVar(&cfg.DryRun, 'n', "dry-run",
		"print the generated files to stdout instead of writing them")
	cfg.Command = &ff.Command{
		Name:      "jsonschema",
		Usage:     "protoschemer jsonschema -p <package> [FLAGS]",
		ShortHelp: "convert JSON Schema documents to protobuf definitions",
		LongHelp: `Convert a directory of JSON Schema documents into .proto files.

Every *.json file under --in is parsed. Their "definitions" are pooled into one
namespace, so a type several documents declare identically yields one message.

Type mapping:

  format: date-time          google.protobuf.Timestamp
  format: date               google.type.Date
  enum ["true","false"]      bool
  additionalProperties only  google.protobuf.Struct
  anyOf[enum, "ext:" ...]    an enum plus a <field>_ext string escape hatch

A document's root schema becomes a message only when --name-map supplies a name
for it; otherwise the document contributes just its definitions.`,
		Flags: cfg.Flags,
		Exec:  cfg.exec,
	}
	parent.Command.Subcommands = append(parent.Command.Subcommands, cfg.Command)
	return &cfg
}

// exec reads the schemas, converts them, and writes the result. The work in
// between is pure, so this function stays a flat sequence of I/O.
func (cfg *Config) exec(_ context.Context, _ []string) error {
	opts, err := jsonschema.NewOptions(
		cfg.Package, cfg.GoPackage, cfg.ImportPrefix, cfg.StripSuffix, cfg.Header)
	if err != nil {
		return fmt.Errorf("jsonschema: %w", err)
	}

	names, err := loadNameMap(cfg.NameMap)
	if err != nil {
		return fmt.Errorf("jsonschema: %w", err)
	}

	docs, err := readDocuments(os.DirFS(cfg.In), names)
	if err != nil {
		return fmt.Errorf("jsonschema: %w", err)
	}
	if len(docs) == 0 {
		return fmt.Errorf("jsonschema: no .json documents in %s", cfg.In)
	}

	files, err := jsonschema.Convert(docs, opts)
	if err != nil {
		return fmt.Errorf("jsonschema: %w", err)
	}
	sources, err := protoemit.Render(files)
	if err != nil {
		return fmt.Errorf("jsonschema: %w", err)
	}

	if cfg.DryRun {
		return cfg.print(sources)
	}
	return cfg.write(sources)
}

// print writes the generated sources to stdout without touching the filesystem.
func (cfg *Config) print(sources []protoir.SourceFile) error {
	for _, f := range sources {
		if _, err := fmt.Fprintf(cfg.Stdout, "// === %s ===\n%s\n", f.Path, f.Source); err != nil {
			return fmt.Errorf("jsonschema: write stdout: %w", err)
		}
	}
	return nil
}

// write saves each generated source under the output directory, named by the
// last element of its path: the leading elements exist to make import
// statements resolve from the protobuf module root, not to nest the output.
func (cfg *Config) write(sources []protoir.SourceFile) error {
	if err := os.MkdirAll(cfg.Out, dirPerm); err != nil {
		return fmt.Errorf("jsonschema: create %s: %w", cfg.Out, err)
	}
	for _, f := range sources {
		dest := filepath.Join(cfg.Out, filepath.Base(f.Path))
		if err := os.WriteFile(dest, []byte(f.Source), filePerm); err != nil {
			return fmt.Errorf("jsonschema: write %s: %w", dest, err)
		}
	}
	_, err := fmt.Fprintf(cfg.Stdout, "wrote %d files to %s\n", len(sources), cfg.Out)
	if err != nil {
		return fmt.Errorf("jsonschema: write stdout: %w", err)
	}
	return nil
}

// readDocuments parses every *.json file in dir, in name order so that a
// definition present in several documents is resolved the same way each run.
func readDocuments(dir fs.FS, names map[string]string) ([]jsonschema.Document, error) {
	entries, err := fs.ReadDir(dir, ".")
	if err != nil {
		return nil, fmt.Errorf("read schema directory: %w", err)
	}

	docs := make([]jsonschema.Document, 0, len(entries))
	paths := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(path.Ext(e.Name()), ".json") {
			paths = append(paths, e.Name())
		}
	}
	sort.Strings(paths)

	for _, name := range paths {
		raw, readErr := fs.ReadFile(dir, name)
		if readErr != nil {
			return nil, fmt.Errorf("read %s: %w", name, readErr)
		}
		var schema schemalib.Schema
		if err := json.Unmarshal(raw, &schema); err != nil {
			return nil, fmt.Errorf("parse %s: %w", name, err)
		}
		docs = append(docs, jsonschema.Document{
			Schema:      &schema,
			RootMessage: rootMessage(name, names),
		})
	}
	return docs, nil
}

// rootMessage returns the message name for a document's root schema, or "" when
// the document should contribute only its definitions.
//
// The mapping is supplied rather than derived because a document's file name is
// usually a wire-format artifact ("...-getallusers-200-responsepayload...") from
// which the intended casing cannot be recovered.
func rootMessage(file string, names map[string]string) string {
	stem := strings.TrimSuffix(file, path.Ext(file))
	if mapped, found := names[stem]; found {
		return strcase.ToPascal(mapped)
	}
	return ""
}

// loadNameMap reads the document-to-message mapping. An empty path yields an
// empty map, meaning no document contributes a root message.
func loadNameMap(file string) (map[string]string, error) {
	if file == "" {
		return map[string]string{}, nil
	}
	raw, err := os.ReadFile(filepath.Clean(file))
	if err != nil {
		return nil, fmt.Errorf("read name map: %w", err)
	}
	names := map[string]string{}
	if err := json.Unmarshal(raw, &names); err != nil {
		return nil, fmt.Errorf("parse name map %s: %w", file, err)
	}
	return names, nil
}
