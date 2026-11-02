package output

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"
)

func TestDropEmptyKeepsFalsyScalars(t *testing.T) {
	type label string
	nested := map[string]any{
		"null":   nil,
		"list":   []any{},
		"object": map[string]any{},
		"typed":  map[label][]string{"none": nil},
		"deep":   map[string]any{"inner": []any{map[string]any{}}},
		"chain":  map[string]any{"a": map[string]any{"b": []string{}}},
		"blank":  "",
		"zero":   0,
		"false":  false,
		"items":  []any{nil, map[string]any{"x": nil, "y": 1}, []any{}},
		"bytes":  []byte{},
		"nilptr": (*int)(nil),
	}
	got := DropEmpty(Object{Fields: []Field{
		{Name: "nested", Value: nested},
		{Name: "gone", Value: map[string]any{"only": nil}},
		{Name: "none", Value: Collection{Columns: []string{"a"}}},
	}})
	want := Object{Fields: []Field{{Name: "nested", Value: map[string]any{
		"deep":  map[string]any{"inner": []any{map[string]any{}}},
		"blank": "",
		"zero":  0,
		"false": false,
		"items": []any{nil, map[string]any{"y": 1}, []any{}},
		"bytes": []byte{},
	}}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DropEmpty() = %#v\nwant %#v", got, want)
	}
	if _, ok := nested["null"]; !ok || len(nested) != 12 {
		t.Fatal("DropEmpty modified its input")
	}
}

func TestDropEmptyKeepsRootAndColumns(t *testing.T) {
	if got := DropEmpty(Object{Fields: []Field{{Name: "a", Value: nil}}}); !reflect.DeepEqual(got, Object{Fields: []Field{}}) {
		t.Errorf("empty root object = %#v", got)
	}
	collection := Collection{Columns: []string{"name", "tags"}, Rows: []Row{
		{"name": "one", "tags": []any{}},
		{"name": "two", "tags": []any{"x"}},
	}}
	var out bytes.Buffer
	if err := Encode(&out, FormatJSON, collection); err != nil {
		t.Fatal(err)
	}
	if got, want := out.String(), `[{"name":"one","tags":null},{"name":"two","tags":["x"]}]`+"\n"; got != want {
		t.Errorf("collection JSON = %q, want %q", got, want)
	}
	if len(collection.Rows[0]) != 2 {
		t.Error("Encode modified the collection")
	}
}

// A value that only held empty members is gone before the text formats check what they can represent.
func TestEncodeDropsEmptyBeforeTextCheck(t *testing.T) {
	result := Object{Fields: []Field{
		{Name: "name", Value: "one"},
		{Name: "labels", Value: map[string]any{"a": []any{}}},
	}}
	for _, format := range []Format{FormatTable, FormatCompact, FormatJSON, FormatTOON} {
		var out bytes.Buffer
		if err := Encode(&out, format, result); err != nil {
			t.Fatalf("%s: %v", format, err)
		}
		if bytes.Contains(bytes.ToLower(out.Bytes()), []byte("labels")) {
			t.Errorf("%s kept the empty member: %q", format, out.String())
		}
	}
	var out bytes.Buffer
	if err := Encode(&out, FormatJSON, result); err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil || len(decoded) != 1 {
		t.Errorf("JSON = %q, %v", out.String(), err)
	}
}
