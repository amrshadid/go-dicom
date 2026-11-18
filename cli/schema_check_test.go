package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// This file is the whole of the JSON Schema check the contract tests need.
// The module has one dependency and a validator is not worth a second, so
// it understands only the keywords docs/*.schema.json use, and fails on any
// other rather than passing what it cannot check.

// schemaDoc is a schema as docs/ keeps it.
type schemaDoc struct {
	root map[string]any
}

// loadSchema reads a schema file from docs/.
func loadSchema(t *testing.T, name string) *schemaDoc {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "docs", name))
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := decodeJSON(data, &root); err != nil {
		t.Fatalf("%s is not JSON: %v", name, err)
	}
	return &schemaDoc{root: root}
}

// decodeJSON decodes one JSON value, keeping numbers exact.
func decodeJSON(data []byte, into any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(into); err != nil {
		return err
	}
	if decoder.More() {
		return fmt.Errorf("more than one JSON value")
	}
	return nil
}

// check returns what is wrong with an instance, empty when it is valid.
//
// With closed, a property the schema does not name is an error. The
// documents say readers ignore such properties, so a reader does not; the
// test does, so that output which grew a field nobody described fails.
func (s *schemaDoc) check(instance any, closed bool) []string {
	var problems []string
	s.validate(s.root, instance, "$", closed, &problems)
	return problems
}

// annotations are keywords that say something to a reader and nothing to a
// validator.
var annotations = map[string]bool{"$schema": true, "title": true, "description": true, "$defs": true}

func (s *schemaDoc) validate(schema map[string]any, v any, path string, closed bool, out *[]string) {
	fail := func(format string, args ...any) { *out = append(*out, path+": "+fmt.Sprintf(format, args...)) }

	for keyword, arg := range schema {
		switch keyword {
		case "$ref":
			ref, _ := arg.(string)
			target := s.resolve(ref)
			if target == nil {
				fail("cannot resolve %q", ref)
				return
			}
			s.validate(target, v, path, closed, out)
		case "type":
			names, ok := arg.([]any)
			if !ok {
				names = []any{arg}
			}
			matched := false
			for _, name := range names {
				matched = matched || isType(name.(string), v)
			}
			if !matched {
				fail("is %s, want %v", describeType(v), arg)
				return
			}
		case "enum":
			found := false
			for _, option := range arg.([]any) {
				found = found || equalJSON(option, v)
			}
			if !found {
				fail("is %v, want one of %v", v, arg)
			}
		case "const":
			if !equalJSON(arg, v) {
				fail("is %v, want %v", v, arg)
			}
		case "minimum", "maximum":
			number, ok := v.(json.Number)
			if !ok {
				continue
			}
			got, _ := number.Float64()
			limit, _ := arg.(json.Number).Float64()
			if (keyword == "minimum" && got < limit) || (keyword == "maximum" && got > limit) {
				fail("is %v, %s is %v", got, keyword, limit)
			}
		case "pattern":
			text, ok := v.(string)
			if !ok {
				continue
			}
			if !regexp.MustCompile(arg.(string)).MatchString(text) {
				fail("is %q, which does not match %s", text, arg)
			}
		case "required":
			object, ok := v.(map[string]any)
			if !ok {
				continue
			}
			for _, name := range arg.([]any) {
				if _, present := object[name.(string)]; !present {
					fail("has no %q", name)
				}
			}
		case "properties":
			object, ok := v.(map[string]any)
			if !ok {
				continue
			}
			declared := arg.(map[string]any)
			names := make([]string, 0, len(object))
			for name := range object {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				sub, known := declared[name]
				if !known {
					if closed {
						fail("has %q, which the schema does not describe", name)
					}
					continue
				}
				s.validate(sub.(map[string]any), object[name], path+"."+name, closed, out)
			}
		case "items":
			list, ok := v.([]any)
			if !ok {
				continue
			}
			for i, item := range list {
				s.validate(arg.(map[string]any), item, fmt.Sprintf("%s[%d]", path, i), closed, out)
			}
		default:
			if !annotations[keyword] {
				fail("the schema uses %q, which this check does not understand", keyword)
			}
		}
	}
}

// resolve follows a local reference of the form #/$defs/name.
func (s *schemaDoc) resolve(ref string) map[string]any {
	name, ok := strings.CutPrefix(ref, "#/$defs/")
	if !ok {
		return nil
	}
	defs, _ := s.root["$defs"].(map[string]any)
	target, _ := defs[name].(map[string]any)
	return target
}

func isType(name string, v any) bool {
	switch name {
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "null":
		return v == nil
	case "number":
		_, ok := v.(json.Number)
		return ok
	case "integer":
		number, ok := v.(json.Number)
		if !ok {
			return false
		}
		_, err := number.Int64()
		return err == nil
	}
	return false
}

func describeType(v any) string {
	switch v := v.(type) {
	case nil:
		return "null"
	case map[string]any:
		return "an object"
	case []any:
		return "an array"
	case string:
		return "a string"
	case bool:
		return "a boolean"
	case json.Number:
		return "the number " + v.String()
	}
	return fmt.Sprintf("%T", v)
}

func equalJSON(a, b any) bool {
	if x, ok := a.(json.Number); ok {
		y, ok := b.(json.Number)
		return ok && x.String() == y.String()
	}
	return reflect.DeepEqual(a, b)
}

// The check has to fail what is wrong before it can be trusted to pass what
// is right: each of these is a defect a reader would trip on.
func TestSchemaCheckRejectsWhatIsWrong(t *testing.T) {
	schema := &schemaDoc{root: map[string]any{}}
	if err := decodeJSON([]byte(`{
		"type": "object",
		"required": ["n", "tag"],
		"properties": {
			"n": {"type": "integer", "minimum": 1},
			"tag": {"type": "string", "pattern": "^\\([0-9A-F]{4}\\)$"},
			"kind": {"enum": ["a", "b"]},
			"nested": {"$ref": "#/$defs/leaf"},
			"list": {"type": "array", "items": {"$ref": "#/$defs/leaf"}},
			"maybe": {"type": ["integer", "null"]}
		},
		"$defs": {"leaf": {"type": "object", "required": ["x"], "properties": {"x": {"const": true}}}}
	}`), &schema.root); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, instance string
		closed         bool
		want           string
	}{
		{"valid", `{"n": 2, "tag": "(00AB)", "kind": "a", "nested": {"x": true}, "list": [{"x": true}], "maybe": null}`, true, ""},
		{"an open schema ignores a property it does not name", `{"n": 2, "tag": "(00AB)", "other": 1}`, false, ""},
		{"a missing required property", `{"n": 2}`, false, `has no "tag"`},
		{"a wrong type", `{"n": "2", "tag": "(00AB)"}`, false, "is a string, want integer"},
		{"a fraction is not an integer", `{"n": 1.5, "tag": "(00AB)"}`, false, "is the number 1.5, want integer"},
		{"below the minimum", `{"n": 0, "tag": "(00AB)"}`, false, "minimum is 1"},
		{"a pattern", `{"n": 1, "tag": "(00ab)"}`, false, "does not match"},
		{"an enum", `{"n": 1, "tag": "(00AB)", "kind": "c"}`, false, "want one of"},
		{"a const reached through a reference", `{"n": 1, "tag": "(00AB)", "nested": {"x": false}}`, false, "$.nested.x"},
		{"an item of an array", `{"n": 1, "tag": "(00AB)", "list": [{"x": true}, {}]}`, false, `$.list[1]: has no "x"`},
		{"a type that is one of two", `{"n": 1, "tag": "(00AB)", "maybe": "no"}`, false, "want [integer null]"},
		{"a closed schema refuses an undescribed property", `{"n": 1, "tag": "(00AB)", "other": 1}`, true, `has "other"`},
	} {
		var instance any
		if err := decodeJSON([]byte(c.instance), &instance); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		problems := strings.Join(schema.check(instance, c.closed), "\n")
		if c.want == "" && problems != "" || !strings.Contains(problems, c.want) {
			t.Errorf("%s: the problems are %q, want one containing %q", c.name, problems, c.want)
		}
	}

	unknown := &schemaDoc{root: map[string]any{"oneOf": []any{}}}
	if problems := unknown.check(nil, false); len(problems) == 0 {
		t.Error("a keyword the check does not understand passed")
	}
}
