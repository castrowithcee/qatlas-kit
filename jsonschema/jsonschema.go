package jsonschema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Mode selects which keywords a value is held to.
type Mode int

const (
	// Strict holds a value to every checked keyword. It is the zero value, the mode for input, and the mode
	// any value other than Lenient selects.
	Strict Mode = iota
	// Lenient does not hold a value to minProperties, maxProperties, uniqueItems, anyOf, or a schema under
	// additionalProperties. It suits data a remote side produced, which a caller checks for its shape but
	// does not refuse for those constraints.
	Lenient
)

// ValidateJSON checks raw against schema in Strict mode and names the first violation by its path, such as
// "$.foo is not allowed". raw must hold exactly one JSON value. A field the schema does not allow is named
// before a missing required one, since it is often the misspelling of that field.
func ValidateJSON(schema, raw json.RawMessage) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("input must be valid JSON")
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return fmt.Errorf("input must contain exactly one JSON value")
	}
	return Validate(schema, value, Strict)
}

// Validate checks a decoded value against schema in the given mode. The value must be decoded the way
// encoding/json does with UseNumber: numbers are json.Number, so a float64 never matches number or
// integer.
func Validate(schema json.RawMessage, value any, mode Mode) error {
	var node map[string]json.RawMessage
	if err := json.Unmarshal(schema, &node); err != nil {
		return fmt.Errorf("schema is invalid")
	}
	if err := checkKeywords(node, "$"); err != nil {
		return err
	}
	return validateAt(node, value, "$", mode != Lenient)
}

// checkKeywords refuses a keyword this package does not check anywhere in the schema, including members
// no value reaches, so a constraint is never skipped in silence.
func checkKeywords(node map[string]json.RawMessage, path string) error {
	names := make([]string, 0, len(node))
	for name := range node {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !knownKeyword(name) {
			return fmt.Errorf("schema at %s uses the unsupported keyword %s", path, name)
		}
	}
	sub := func(raw json.RawMessage, at string, invalid error) error {
		var child map[string]json.RawMessage
		if json.Unmarshal(raw, &child) != nil || child == nil {
			return invalid
		}
		return checkKeywords(child, at)
	}
	if raw := node["properties"]; len(raw) > 0 {
		var properties map[string]json.RawMessage
		if json.Unmarshal(raw, &properties) != nil {
			return fmt.Errorf("schema at %s has invalid properties", path)
		}
		members := make([]string, 0, len(properties))
		for name := range properties {
			members = append(members, name)
		}
		sort.Strings(members)
		for _, name := range members {
			at := path + "." + name
			if err := sub(properties[name], at, fmt.Errorf("schema at %s is invalid", at)); err != nil {
				return err
			}
		}
	}
	if raw := node["additionalProperties"]; len(raw) > 0 && json.Unmarshal(raw, new(bool)) != nil {
		if err := sub(raw, path+".*", fmt.Errorf("schema at %s has invalid additionalProperties", path)); err != nil {
			return err
		}
	}
	if raw := node["items"]; len(raw) > 0 {
		if err := sub(raw, path+"[]", fmt.Errorf("schema at %s has invalid items", path)); err != nil {
			return err
		}
	}
	if raw := node["anyOf"]; len(raw) > 0 {
		var branches []json.RawMessage
		if json.Unmarshal(raw, &branches) != nil || len(branches) == 0 {
			return fmt.Errorf("schema at %s has an invalid anyOf", path)
		}
		for _, branch := range branches {
			if err := sub(branch, path, fmt.Errorf("schema at %s has an invalid anyOf", path)); err != nil {
				return err
			}
		}
	}
	return nil
}

// knownKeyword reports whether this package checks a keyword or the keyword only annotates a schema. An
// x- keyword is an extension that annotates, such as x-form.
func knownKeyword(name string) bool {
	switch name {
	case "type", "enum", "properties", "required", "additionalProperties", "minProperties", "maxProperties",
		"items", "minItems", "maxItems", "uniqueItems", "minLength", "maxLength", "pattern", "minimum",
		"maximum", "anyOf",
		"title", "description", "default", "examples", "deprecated", "readOnly", "writeOnly", "$comment",
		"$schema", "$id":
		return true
	}
	return strings.HasPrefix(name, "x-")
}

func validateAt(schema map[string]json.RawMessage, value any, path string, strict bool) error {
	var kind string
	if raw := schema["type"]; len(raw) > 0 {
		if err := json.Unmarshal(raw, &kind); err != nil {
			return fmt.Errorf("schema at %s has an invalid type", path)
		}
		if !isType(kind, value) {
			return fmt.Errorf("%s must be %s", path, kind)
		}
	}

	if raw := schema["enum"]; len(raw) > 0 {
		var allowed []any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if decoder.Decode(&allowed) != nil {
			return fmt.Errorf("schema at %s has an invalid enum", path)
		}
		match := false
		encoded, _ := json.Marshal(value)
		for _, candidate := range allowed {
			other, _ := json.Marshal(candidate)
			if bytes.Equal(encoded, other) {
				match = true
				break
			}
		}
		if !match {
			return fmt.Errorf("%s is not an allowed value", path)
		}
	}

	switch value := value.(type) {
	case map[string]any:
		properties := map[string]json.RawMessage{}
		if raw := schema["properties"]; len(raw) > 0 {
			if err := json.Unmarshal(raw, &properties); err != nil {
				return fmt.Errorf("schema at %s has invalid properties", path)
			}
		}
		additional := true
		var extra map[string]json.RawMessage
		if raw := schema["additionalProperties"]; len(raw) > 0 {
			if json.Unmarshal(raw, &additional) != nil {
				additional = true
				// A schema for the other members is held to in Strict mode only.
				if strict && json.Unmarshal(raw, &extra) != nil {
					return fmt.Errorf("schema at %s has invalid additionalProperties", path)
				}
			}
		}
		if strict {
			minimum, maximum, err := counts(schema, "minProperties", "maxProperties", path)
			if err != nil {
				return err
			}
			if minimum >= 0 && len(value) < minimum {
				if minimum == 1 {
					return fmt.Errorf("%s must not be empty", path)
				}
				return fmt.Errorf("%s must have at least %d members", path, minimum)
			}
			if maximum >= 0 && len(value) > maximum {
				return fmt.Errorf("%s must have at most %d members", path, maximum)
			}
		}
		names := make([]string, 0, len(value))
		for name := range value {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if _, ok := properties[name]; !ok && !additional {
				return fmt.Errorf("%s.%s is not allowed", path, name)
			}
		}
		var required []string
		if raw := schema["required"]; len(raw) > 0 {
			if err := json.Unmarshal(raw, &required); err != nil {
				return fmt.Errorf("schema at %s has invalid required fields", path)
			}
		}
		for _, name := range required {
			if _, ok := value[name]; !ok {
				return fmt.Errorf("%s.%s is required", path, name)
			}
		}
		for _, name := range names {
			child := value[name]
			raw, ok := properties[name]
			if !ok {
				if extra != nil {
					if err := validateAt(extra, child, path+"."+name, strict); err != nil {
						return err
					}
				}
				continue
			}
			var childSchema map[string]json.RawMessage
			if err := json.Unmarshal(raw, &childSchema); err != nil {
				return fmt.Errorf("schema at %s.%s is invalid", path, name)
			}
			if err := validateAt(childSchema, child, path+"."+name, strict); err != nil {
				return err
			}
		}
	case []any:
		minimum, maximum, err := counts(schema, "minItems", "maxItems", path)
		if err != nil {
			return err
		}
		if minimum >= 0 && len(value) < minimum {
			if minimum == 1 {
				return fmt.Errorf("%s must not be empty", path)
			}
			return fmt.Errorf("%s must have at least %d items", path, minimum)
		}
		if maximum >= 0 && len(value) > maximum {
			return fmt.Errorf("%s must have at most %d items", path, maximum)
		}
		if raw := schema["uniqueItems"]; strict && string(raw) == "true" {
			first := make(map[string]int, len(value))
			for i, item := range value {
				key := equalityKey(item)
				if earlier, ok := first[key]; ok {
					return fmt.Errorf("%s[%d] repeats %s[%d]", path, i, path, earlier)
				}
				first[key] = i
			}
		}
		if raw := schema["items"]; len(raw) > 0 {
			var itemSchema map[string]json.RawMessage
			if err := json.Unmarshal(raw, &itemSchema); err != nil {
				return fmt.Errorf("schema at %s has invalid items", path)
			}
			for i, item := range value {
				if err := validateAt(itemSchema, item, path+"["+strconv.Itoa(i)+"]", strict); err != nil {
					return err
				}
			}
		}
	case string:
		minimum, maximum, err := counts(schema, "minLength", "maxLength", path)
		if err != nil {
			return err
		}
		if length := utf8.RuneCountInString(value); minimum >= 0 && length < minimum {
			return fmt.Errorf("%s is shorter than %d characters", path, minimum)
		} else if maximum >= 0 && length > maximum {
			return fmt.Errorf("%s is longer than %d characters", path, maximum)
		}
		if raw := schema["pattern"]; len(raw) > 0 {
			var pattern string
			if err := json.Unmarshal(raw, &pattern); err != nil {
				return fmt.Errorf("schema at %s has an invalid pattern", path)
			}
			expression, err := regexp.Compile(pattern)
			if err != nil {
				return fmt.Errorf("schema at %s has an invalid pattern", path)
			}
			if !expression.MatchString(value) {
				// A pattern is no help to read, so a schema may name the form in words under x-form. It is
				// the schema's own text, never the rejected value.
				var form string
				if raw := schema["x-form"]; len(raw) > 0 && json.Unmarshal(raw, &form) == nil && form != "" {
					return fmt.Errorf("%s does not have the required form %s", path, form)
				}
				return fmt.Errorf("%s does not have the required form", path)
			}
		}
	case json.Number:
		number, _ := value.Float64()
		for _, name := range []string{"minimum", "maximum"} {
			raw := schema[name]
			if len(raw) == 0 {
				continue
			}
			limit, err := strconv.ParseFloat(string(raw), 64)
			if err != nil {
				return fmt.Errorf("schema at %s has an invalid %s", path, name)
			}
			if name == "minimum" && number < limit {
				return fmt.Errorf("%s must be at least %v", path, limit)
			}
			if name == "maximum" && number > limit {
				return fmt.Errorf("%s must be at most %v", path, limit)
			}
		}
	}
	if raw := schema["anyOf"]; strict && len(raw) > 0 {
		return validateAnyOf(raw, value, path)
	}
	return nil
}

// counts reads a pair of count bounds, such as minItems and maxItems. An absent bound is -1; a bound that
// is not a non-negative whole number makes the schema invalid instead of being ignored.
func counts(schema map[string]json.RawMessage, minName, maxName, path string) (minimum, maximum int, err error) {
	read := func(name string) (int, error) {
		raw := schema[name]
		if len(raw) == 0 {
			return -1, nil
		}
		var n int
		if json.Unmarshal(raw, &n) != nil || n < 0 {
			return 0, fmt.Errorf("schema at %s has an invalid %s", path, name)
		}
		return n, nil
	}
	if minimum, err = read(minName); err != nil {
		return 0, 0, err
	}
	if maximum, err = read(maxName); err != nil {
		return 0, 0, err
	}
	return minimum, maximum, nil
}

// validateAnyOf accepts value as soon as one branch does. Otherwise it names the failure the way a reader
// can act on it: the allowed types when no branch has the value's type, the one branch of its type, or the
// members of which one is required.
func validateAnyOf(raw json.RawMessage, value any, path string) error {
	var branches []map[string]json.RawMessage
	if err := json.Unmarshal(raw, &branches); err != nil || len(branches) == 0 {
		return fmt.Errorf("schema at %s has an invalid anyOf", path)
	}
	var errs []error
	var types, requiredPaths []string
	onlyRequired, typeMatch := true, -1
	for i, branch := range branches {
		err := validateAt(branch, value, path, true)
		if err == nil {
			return nil
		}
		errs = append(errs, err)

		var names []string
		if len(branch) != 1 || json.Unmarshal(branch["required"], &names) != nil {
			onlyRequired = false
		}
		for _, name := range names {
			if !contains(requiredPaths, path+"."+name) {
				requiredPaths = append(requiredPaths, path+"."+name)
			}
		}

		var kind string
		if json.Unmarshal(branch["type"], &kind) == nil && kind != "" {
			if !contains(types, kind) {
				types = append(types, kind)
			}
			if typeMatch < 0 && isType(kind, value) {
				typeMatch = i
			}
		}
	}
	switch {
	case onlyRequired:
		return fmt.Errorf("%s is required", joinOr(requiredPaths))
	case len(types) > 0 && typeMatch < 0:
		return fmt.Errorf("%s must be %s", path, joinOr(types))
	case typeMatch >= 0:
		return errs[typeMatch]
	}
	return errs[0]
}

// joinOr lists words in English: "a or b", "a, b, or c".
func joinOr(words []string) string {
	switch len(words) {
	case 1:
		return words[0]
	case 2:
		return words[0] + " or " + words[1]
	}
	return strings.Join(words[:len(words)-1], ", ") + ", or " + words[len(words)-1]
}

// equalityKey encodes a decoded JSON value so that equal JSON values yield equal keys. Numbers are
// normalized to their exact value at every level, so 1, 1.0 and 1e0 match; a number never matches a string.
func equalityKey(value any) string {
	switch v := value.(type) {
	case json.Number:
		return "n" + canonicalNumber(string(v))
	case []any:
		parts := make([]string, len(v))
		for i, item := range v {
			parts[i] = equalityKey(item)
		}
		return "[" + strings.Join(parts, ",") + "]"
	case map[string]any:
		names := make([]string, 0, len(v))
		for name := range v {
			names = append(names, name)
		}
		sort.Strings(names)
		parts := make([]string, len(names))
		for i, name := range names {
			parts[i] = strconv.Quote(name) + ":" + equalityKey(v[name])
		}
		return "{" + strings.Join(parts, ",") + "}"
	}
	encoded, _ := json.Marshal(value)
	return "v" + string(encoded)
}

// canonicalNumber spells a JSON number so that equal values are spelled alike, such as 1, 1.0, 10e-1 and
// 1e0. It never expands the exponent, because an input like 1e999999 would otherwise cost time and memory
// in proportion to its value rather than its length.
func canonicalNumber(n string) string {
	sign := ""
	if strings.HasPrefix(n, "-") {
		sign, n = "-", n[1:]
	}
	mantissa, exponent := n, 0
	if i := strings.IndexAny(n, "eE"); i >= 0 {
		e, err := strconv.Atoi(n[i+1:])
		if err != nil {
			return sign + n
		}
		mantissa, exponent = n[:i], e
	}
	digits := mantissa
	if i := strings.IndexByte(mantissa, '.'); i >= 0 {
		digits = mantissa[:i] + mantissa[i+1:]
		exponent -= len(mantissa) - i - 1
	}
	digits = strings.TrimLeft(digits, "0")
	if digits == "" {
		return "0"
	}
	significant := strings.TrimRight(digits, "0")
	exponent += len(digits) - len(significant)
	return sign + significant + "e" + strconv.Itoa(exponent)
}

func isType(kind string, value any) bool {
	switch kind {
	case "object":
		_, ok := value.(map[string]any)
		return ok
	case "array":
		_, ok := value.([]any)
		return ok
	case "string":
		_, ok := value.(string)
		return ok
	case "integer":
		n, ok := value.(json.Number)
		if !ok {
			return false
		}
		_, err := n.Int64()
		return err == nil
	case "number":
		_, ok := value.(json.Number)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	default:
		return false
	}
}

func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
