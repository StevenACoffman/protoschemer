package jsonschema

import (
	"sort"
	"strings"

	schemalib "github.com/swaggest/jsonschema-go"
)

// poolDefinitions merges the definitions of every document into one namespace.
//
// Multi-document schema sets routinely repeat a shared definition in each
// document that references it. Where two copies differ they differ in prose, so
// the copy carrying more description is kept: it produces better comments and
// the structural content is the same either way.
func poolDefinitions(docs []Document) map[string]*schemalib.Schema {
	pool := make(map[string]*schemalib.Schema)
	for i := range docs {
		if docs[i].Schema == nil {
			continue
		}
		for name, def := range docs[i].Schema.Definitions {
			candidate := def.TypeObject
			if candidate == nil {
				continue
			}
			if existing, seen := pool[name]; seen &&
				descriptionLen(existing) >= descriptionLen(candidate) {
				continue
			}
			pool[name] = candidate
		}
	}
	return pool
}

// freeFormNames reports which pooled definitions are open bags of arbitrary
// JSON, and so are represented as google.protobuf.Struct wherever referenced
// rather than as messages of their own.
func freeFormNames(pool map[string]*schemalib.Schema) map[string]bool {
	names := make(map[string]bool, len(pool))
	for name, s := range pool {
		if isFreeForm(s) {
			names[name] = true
		}
	}
	return names
}

// descriptionLen totals the prose in a schema, used to pick between otherwise
// equivalent copies of one definition.
func descriptionLen(s *schemalib.Schema) int {
	total := len(text(s.Description))
	for _, p := range s.Properties {
		if p.TypeObject != nil {
			total += len(text(p.TypeObject.Description))
		}
	}
	return total
}

// sortedKeys returns a map's keys in a deterministic order.
func sortedKeys(m map[string]*schemalib.Schema) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedProperties returns property names in a deterministic order.
func sortedProperties(props map[string]schemalib.SchemaOrBool) []string {
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// text flattens optional schema prose into a single line, since a protobuf
// comment carries no paragraph structure.
func text(s *string) string {
	if s == nil {
		return ""
	}
	return strings.Join(strings.Fields(*s), " ")
}

// quote renders a schema value for inclusion in a comment.
func quote(s string) string { return "'" + s + "'" }
