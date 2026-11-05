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
// integer. It runs CheckSchema first, so a malformed schema is refused whether or not the value reaches the
// malformed part.
func Validate(schema json.RawMessage, value any, mode Mode) error {
	if err := CheckSchema(schema); err != nil {
		return err
	}
	var node map[string]json.RawMessage
	if err := json.Unmarshal(schema, &node); err != nil {
		return fmt.Errorf("schema is invalid")
	}
	return validateAt(node, value, "$", mode != Lenient)
}

// CheckSchema checks a schema without a value. It reports every keyword this package does not check and
// every malformed checked keyword anywhere in the schema, including members no value reaches, so a
// constraint is never skipped in silence. The errors are joined in a fixed order, depth first, each naming
// its schema path.
func CheckSchema(schema json.RawMessage) error {
	var node map[string]json.RawMessage
	if json.Unmarshal(schema, &node) != nil || node == nil {
		return fmt.Errorf("schema is invalid")
	}
	return errors.Join(checkNode(node, "$")...)
}

// asSchema reads raw as a schema object; null and anything but an object is not one.
func asSchema(raw json.RawMessage) (map[string]json.RawMessage, bool) {
	var child map[string]json.RawMessage
	if json.Unmarshal(raw, &child) != nil || child == nil {
		return nil, false
	}
	return child, true
}

func sortedKeys(m map[string]json.RawMessage) []string {
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// nullMessages lists the checked keywords with the error a literal null value earns.
var nullMessages = map[string]string{
	"type": "has an invalid type", "enum": "has an invalid enum", "properties": "has invalid properties",
	"required": "has invalid required fields", "additionalProperties": "has invalid additionalProperties",
	"minProperties": "has an invalid minProperties", "maxProperties": "has an invalid maxProperties",
	"minItems": "has an invalid minItems", "maxItems": "has an invalid maxItems",
	"minLength": "has an invalid minLength", "maxLength": "has an invalid maxLength",
	"items": "has invalid items", "uniqueItems": "has an invalid uniqueItems", "pattern": "has an invalid pattern",
	"minimum": "has an invalid minimum", "maximum": "has an invalid maximum", "anyOf": "has an invalid anyOf",
	"format": "has an invalid format", "propertyNames": "has invalid propertyNames",
}

func checkNode(node map[string]json.RawMessage, path string) []error {
	var errs []error
	bad := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("schema at %s %s", path, fmt.Sprintf(format, args...)))
	}
	for _, name := range sortedKeys(node) {
		if !knownKeyword(name) {
			bad("uses the unsupported keyword %s", name)
		}
	}

	// A literal null is malformed for every checked keyword, though encoding/json decodes it into a bool or
	// a string without error. Past this point node holds no null checked keyword.
	trimmed := make(map[string]json.RawMessage, len(node))
	for name, raw := range node {
		trimmed[name] = raw
	}
	for _, name := range sortedKeys(node) {
		if message, checked := nullMessages[name]; checked && string(node[name]) == "null" {
			bad("%s", message)
			delete(trimmed, name)
		}
	}
	node = trimmed

	if raw := node["type"]; len(raw) > 0 {
		var kind string
		if json.Unmarshal(raw, &kind) != nil || !knownType(kind) {
			bad("has an invalid type")
		}
	}
	if raw := node["enum"]; len(raw) > 0 {
		var allowed []any
		if json.Unmarshal(raw, &allowed) != nil || len(allowed) == 0 {
			bad("has an invalid enum")
		}
	}
	if raw := node["required"]; len(raw) > 0 {
		var required []string
		if json.Unmarshal(raw, &required) != nil || required == nil {
			bad("has invalid required fields")
		}
	}
	for _, pair := range [][2]string{
		{"minProperties", "maxProperties"}, {"minItems", "maxItems"}, {"minLength", "maxLength"},
	} {
		valid := true
		for _, name := range pair {
			if len(node[name]) > 0 {
				if _, ok := countBound(node[name]); !ok {
					bad("has an invalid %s", name)
					valid = false
				}
			}
		}
		if valid && len(node[pair[0]]) > 0 && len(node[pair[1]]) > 0 {
			lower, _ := countBound(node[pair[0]])
			upper, _ := countBound(node[pair[1]])
			if lower > upper {
				bad("has %s greater than %s", pair[0], pair[1])
			}
		}
	}
	if raw := node["pattern"]; len(raw) > 0 {
		var pattern string
		if json.Unmarshal(raw, &pattern) != nil {
			bad("has an invalid pattern")
		} else if _, err := regexp.Compile(pattern); err != nil {
			bad("has an invalid pattern")
		}
	}
	for _, name := range []string{"minimum", "maximum"} {
		if raw := node[name]; len(raw) > 0 {
			if _, err := strconv.ParseFloat(string(raw), 64); err != nil {
				bad("has an invalid %s", name)
			}
		}
	}
	if raw := node["uniqueItems"]; len(raw) > 0 && json.Unmarshal(raw, new(bool)) != nil {
		bad("has an invalid uniqueItems")
	}
	if raw := node["format"]; len(raw) > 0 {
		var format string
		switch {
		case json.Unmarshal(raw, &format) != nil:
			bad("has an invalid format")
		case format != "uuid" && format != "date-time":
			bad("uses the unsupported format %s", format)
		}
	}

	if raw := node["properties"]; len(raw) > 0 {
		var properties map[string]json.RawMessage
		if json.Unmarshal(raw, &properties) != nil || properties == nil {
			bad("has invalid properties")
		}
		for _, name := range sortedKeys(properties) {
			at := path + "." + name
			if child, ok := asSchema(properties[name]); ok {
				errs = append(errs, checkNode(child, at)...)
			} else {
				errs = append(errs, fmt.Errorf("schema at %s is invalid", at))
			}
		}
	}
	if raw := node["additionalProperties"]; len(raw) > 0 && json.Unmarshal(raw, new(bool)) != nil {
		if child, ok := asSchema(raw); ok {
			errs = append(errs, checkNode(child, path+".*")...)
		} else {
			bad("has invalid additionalProperties")
		}
	}
	if raw := node["items"]; len(raw) > 0 {
		if child, ok := asSchema(raw); ok {
			errs = append(errs, checkNode(child, path+"[]")...)
		} else {
			bad("has invalid items")
		}
	}
	if raw := node["propertyNames"]; len(raw) > 0 {
		if child, ok := asSchema(raw); ok {
			errs = append(errs, checkNode(child, path+"{}")...)
		} else {
			bad("has invalid propertyNames")
		}
	}
	if raw := node["anyOf"]; len(raw) > 0 {
		var branches []json.RawMessage
		if json.Unmarshal(raw, &branches) != nil || len(branches) == 0 {
			bad("has an invalid anyOf")
		}
		for _, branch := range branches {
			if child, ok := asSchema(branch); ok {
				errs = append(errs, checkNode(child, path)...)
			} else {
				bad("has an invalid anyOf")
			}
		}
	}
	return errs
}

// countBound reads a count bound: a non-negative whole number.
func countBound(raw json.RawMessage) (int, bool) {
	var n int
	if string(raw) == "null" || json.Unmarshal(raw, &n) != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// knownKeyword reports whether this package checks a keyword or the keyword only annotates a schema. An
// x- keyword is an extension that annotates, such as x-form.
func knownKeyword(name string) bool {
	switch name {
	case "type", "enum", "properties", "required", "additionalProperties", "minProperties", "maxProperties",
		"items", "minItems", "maxItems", "uniqueItems", "minLength", "maxLength", "pattern", "minimum",
		"maximum", "anyOf", "propertyNames", "format",
		"title", "description", "default", "examples", "deprecated", "readOnly", "writeOnly", "$comment",
		"$schema", "$id":
		return true
	}
	return strings.HasPrefix(name, "x-")
}

func knownType(kind string) bool {
	switch kind {
	case "object", "array", "string", "integer", "number", "boolean", "null":
		return true
	}
	return false
}

var (
	uuidForm = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	// RFC 3339 section 5.6: a four-digit year, T or t, and Z, z or a numeric offset are required.
	dateTimeForm = regexp.MustCompile(
		`^(\d{4})-(\d{2})-(\d{2})[Tt](\d{2}):(\d{2}):(\d{2})(?:\.\d+)?(?:[Zz]|[+-](\d{2}):(\d{2}))$`)
)

// validDateTime checks the RFC 3339 form and the ranges of its parts. time.Parse is not used: it accepts
// only an uppercase T and Z, and its range checks on offsets differ from the RFC. A leap second (:60) is
// refused, the narrower reading.
func validDateTime(s string) bool {
	m := dateTimeForm.FindStringSubmatch(s)
	if m == nil {
		return false
	}
	num := func(i int) int { n, _ := strconv.Atoi(m[i]); return n }
	year, month, day := num(1), num(2), num(3)
	if month < 1 || month > 12 || day < 1 {
		return false
	}
	days := [...]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}[month-1]
	if month == 2 && (year%4 == 0 && year%100 != 0 || year%400 == 0) {
		days = 29
	}
	if day > days || num(4) > 23 || num(5) > 59 || num(6) > 59 {
		return false
	}
	return m[7] == "" || num(7) <= 23 && num(8) <= 59
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
		if raw := schema["propertyNames"]; len(raw) > 0 {
			var nameSchema map[string]json.RawMessage
			if err := json.Unmarshal(raw, &nameSchema); err != nil {
				return fmt.Errorf("schema at %s has invalid propertyNames", path)
			}
			// Held to in both modes, so a schema's own anyOf inside it applies in both as well.
			for _, name := range names {
				if err := validateAt(nameSchema, name, path+"{"+name+"}", true); err != nil {
					return err
				}
			}
		}
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
		if raw := schema["format"]; len(raw) > 0 {
			var format string
			if err := json.Unmarshal(raw, &format); err != nil {
				return fmt.Errorf("schema at %s has an invalid format", path)
			}
			switch format {
			case "uuid":
				if !uuidForm.MatchString(value) {
					return fmt.Errorf("%s is not a valid uuid", path)
				}
			case "date-time":
				if !validDateTime(value) {
					return fmt.Errorf("%s is not a valid date-time", path)
				}
			default:
				return fmt.Errorf("schema at %s uses the unsupported format %s", path, format)
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
