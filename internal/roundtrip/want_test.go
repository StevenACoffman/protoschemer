package roundtrip_test

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
)

// The checks below are the vocabulary the aspect table is written in. Each says
// one thing about a recovered property schema and reports what it saw, so a
// failure names the difference rather than dumping two documents to compare.

// hasType checks the JSON type, accepting the nullable spelling ["T","null"]
// that the converter uses for a field without explicit presence.
func hasType(want string) func(map[string]any) error {
	return func(got map[string]any) error {
		if slices.Contains(typesOf(got), want) {
			return nil
		}
		return fmt.Errorf("type: want %q, got %v", want, typesOf(got))
	}
}

// isNullable checks that a type is present alongside an explicit null, which is
// how the round trip renders a property absent from "required".
func isNullable(want string) func(map[string]any) error {
	return func(got map[string]any) error {
		types := typesOf(got)
		var sawType, sawNull bool
		for _, t := range types {
			switch t {
			case want:
				sawType = true
			case "null":
				sawNull = true
			}
		}
		if sawType && sawNull {
			return nil
		}
		return fmt.Errorf("want nullable %q, got types %v", want, types)
	}
}

// isArrayOf checks a list and its element type.
func isArrayOf(elem string) func(map[string]any) error {
	return func(got map[string]any) error {
		if err := hasType("array")(got); err != nil {
			return err
		}
		items, ok := got["items"].(map[string]any)
		if !ok {
			return fmt.Errorf("items: want a schema, got %T", got["items"])
		}
		return hasType(elem)(items)
	}
}

// hasKeyword checks a scalar keyword such as format or description.
func hasKeyword(keyword, want string) func(map[string]any) error {
	return func(got map[string]any) error {
		actual, ok := got[keyword].(string)
		if !ok {
			return fmt.Errorf("%s: missing (have %v)", keyword, keysOf(got))
		}
		if !strings.Contains(actual, want) {
			return fmt.Errorf("%s: want %q, got %q", keyword, want, actual)
		}
		return nil
	}
}

// hasAdditionalProperties checks that arbitrary members are permitted, however
// that permission is spelled.
func hasAdditionalProperties() func(map[string]any) error {
	return func(got map[string]any) error {
		switch ap := got["additionalProperties"].(type) {
		case bool:
			if ap {
				return nil
			}
			return errors.New("additionalProperties: want permissive, got false")
		case map[string]any:
			return nil
		default:
			return fmt.Errorf("additionalProperties: want permissive, got %T", ap)
		}
	}
}

// hasProperties checks that an object declares the named members.
func hasProperties(want ...string) func(map[string]any) error {
	return func(got map[string]any) error {
		props, ok := got["properties"].(map[string]any)
		if !ok {
			return fmt.Errorf("properties: missing (have %v)", keysOf(got))
		}
		for _, name := range want {
			if _, found := props[name]; !found {
				return fmt.Errorf("properties: missing %q (have %v)", name, keysOf(props))
			}
		}
		return nil
	}
}

// hasEnumSuffixes checks the enum members by suffix.
//
// Suffix rather than exact match because protobuf requires every value to carry
// its enum's name as a prefix, so the prefix is a naming artifact while the
// suffix is the value the source schema actually named.
func hasEnumSuffixes(want ...string) func(map[string]any) error {
	return func(got map[string]any) error {
		raw, ok := got["enum"].([]any)
		if !ok {
			return fmt.Errorf("enum: missing (have %v)", keysOf(got))
		}
		values := make([]string, 0, len(raw))
		for _, v := range raw {
			if s, isText := v.(string); isText {
				values = append(values, s)
			}
		}
		for _, suffix := range want {
			if !anyHasSuffix(values, "_"+suffix) {
				return fmt.Errorf("enum: no value ending in %q (got %v)", suffix, values)
			}
		}
		return nil
	}
}

// both runs checks in order and reports the first difference.
func both(checks ...func(map[string]any) error) func(map[string]any) error {
	return func(got map[string]any) error {
		for _, check := range checks {
			if err := check(got); err != nil {
				return err
			}
		}
		return nil
	}
}

// typesOf normalizes the "type" keyword, which JSON Schema permits to be either
// a single name or a list of them.
func typesOf(schema map[string]any) []string {
	switch t := schema["type"].(type) {
	case string:
		return []string{t}
	case []any:
		out := make([]string, 0, len(t))
		for _, v := range t {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func keysOf(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func anyHasSuffix(values []string, suffix string) bool {
	for _, v := range values {
		if strings.HasSuffix(v, suffix) {
			return true
		}
	}
	return false
}
