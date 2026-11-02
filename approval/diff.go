package approval

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf16"
)

// Change is one value that differs between an approved and a current state.
//
// Path names the value: object members are joined with dots, a member whose name is not plain letters,
// digits, '_' and '-' is written as ["name"], and list items as [i]; the root value itself has the empty
// path. Before and After are the JSON text of the value on each side, an empty object or list included,
// and empty where that side has no value at that path. Every rune that is not printable is written as a
// \u escape in both, so a state cannot steer the terminal it is shown on.
type Change struct {
	Path   string
	Before string
	After  string
}

// Diff lists the values that differ between the JSON documents before and after, sorted by path. A nil or
// empty side has no values, so every value of the other side is listed.
func Diff(before, after []byte) ([]Change, error) {
	b, err := leaves(before)
	if err != nil {
		return nil, fmt.Errorf("approval: the approved state is not JSON: %w", err)
	}
	a, err := leaves(after)
	if err != nil {
		return nil, fmt.Errorf("approval: the current state is not JSON: %w", err)
	}
	var changes []Change
	for path, value := range b {
		if a[path] != value {
			changes = append(changes, Change{Path: path, Before: value, After: a[path]})
		}
	}
	for path, value := range a {
		if _, ok := b[path]; !ok {
			changes = append(changes, Change{Path: path, After: value})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// leaves maps the path of every scalar, empty object and empty list of a JSON document to its text.
func leaves(data []byte) (map[string]string, error) {
	out := map[string]string{}
	if len(bytes.TrimSpace(data)) == 0 {
		return out, nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("more than one value")
	}
	return out, flatten("", value, out)
}

func flatten(path string, value any, out map[string]string) error {
	switch v := value.(type) {
	case map[string]any:
		if len(v) == 0 {
			out[path] = "{}"
		}
		for key, sub := range v {
			if err := flatten(path+member(path, key), sub, out); err != nil {
				return err
			}
		}
	case []any:
		if len(v) == 0 {
			out[path] = "[]"
		}
		for i, sub := range v {
			if err := flatten(fmt.Sprintf("%s[%d]", path, i), sub, out); err != nil {
				return err
			}
		}
	default:
		text, err := visible(v)
		if err != nil {
			return err
		}
		out[path] = text
	}
	return nil
}

// member is the path segment of the object member key below path.
func member(path, key string) string {
	plain := key != ""
	for _, r := range key {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			plain = false
			break
		}
	}
	if !plain {
		text, _ := visible(key)
		return "[" + text + "]"
	}
	if path == "" {
		return key
	}
	return "." + key
}

// visible is the JSON text of value with every rune that is not printable escaped.
func visible(value any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return "", err
	}
	text := strings.TrimSuffix(buf.String(), "\n")
	var out strings.Builder
	for _, r := range text {
		if unicode.IsPrint(r) && !unicode.IsControl(r) {
			out.WriteRune(r)
			continue
		}
		if hi, lo := utf16.EncodeRune(r); hi != unicode.ReplacementChar {
			fmt.Fprintf(&out, `\u%04x\u%04x`, hi, lo)
			continue
		}
		fmt.Fprintf(&out, `\u%04x`, r)
	}
	return out.String(), nil
}
