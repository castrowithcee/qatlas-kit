package jsonschema

import (
	"bytes"
	"encoding/json"
	"regexp/syntax"
	"unicode/utf8"
)

// knownValid reports whether checkSchemaTree would accept a schema that json.Valid accepted, reading the
// bytes once without building a tree. It answers true only for a schema it recognizes as valid in full and
// false for anything else, so false never means invalid.
//
// It checks every occurrence of a keyword or member name, while the tree keeps the last of duplicates. A
// schema whose every occurrence passes is accepted by the tree as well, so duplicates need no detection.
func knownValid(schema []byte) bool {
	s := scan{data: schema}
	s.space()
	return s.pos < len(s.data) && s.data[s.pos] == '{' && s.node()
}

// scan reads a schema that json.Valid accepted, so it does not check the syntax again.
type scan struct {
	data []byte
	pos  int
	buf  []byte
}

func (s *scan) space() {
	for s.pos < len(s.data) {
		switch s.data[s.pos] {
		case ' ', '\t', '\r', '\n':
			s.pos++
		default:
			return
		}
	}
}

// next steps past the separator after a member or item, if there is one.
func (s *scan) next() {
	s.space()
	if s.data[s.pos] == ',' {
		s.pos++
		s.space()
	}
}

// node reads a schema object.
func (s *scan) node() bool {
	s.pos++
	// minProperties, maxProperties, minItems, maxItems, minLength and maxLength in pairs; -1 is absent.
	bounds := [6]int{-1, -1, -1, -1, -1, -1}
	for s.space(); s.data[s.pos] != '}'; s.next() {
		key, plain := s.string()
		if !plain {
			return false
		}
		s.space()
		s.pos++ // the colon
		s.space()
		var valid bool
		switch string(key) {
		case "type":
			kind, plain := s.string()
			valid = plain && knownType(string(kind))
		case "enum":
			valid = s.enum()
		case "properties":
			valid = s.properties()
		case "required":
			valid = s.required()
		case "additionalProperties":
			valid = s.boolean() || s.data[s.pos] == '{' && s.node()
		case "items", "propertyNames":
			valid = s.data[s.pos] == '{' && s.node()
		case "anyOf":
			valid = s.anyOf()
		case "minProperties":
			valid = s.count(&bounds[0])
		case "maxProperties":
			valid = s.count(&bounds[1])
		case "minItems":
			valid = s.count(&bounds[2])
		case "maxItems":
			valid = s.count(&bounds[3])
		case "minLength":
			valid = s.count(&bounds[4])
		case "maxLength":
			valid = s.count(&bounds[5])
		case "minimum", "maximum":
			valid = s.limit()
		case "uniqueItems":
			valid = s.boolean()
		case "pattern":
			valid = s.pattern()
		case "format":
			format, plain := s.string()
			valid = plain && (string(format) == "uuid" || string(format) == "date-time")
		case "title", "description", "default", "examples", "deprecated", "readOnly", "writeOnly", "$comment",
			"$schema", "$id":
			valid = s.skip(false)
		default:
			valid = bytes.HasPrefix(key, []byte("x-")) && s.skip(false)
		}
		if !valid {
			return false
		}
	}
	s.pos++
	for i := 0; i < len(bounds); i += 2 {
		if bounds[i] >= 0 && bounds[i+1] >= 0 && bounds[i] > bounds[i+1] {
			return false
		}
	}
	return true
}

func (s *scan) properties() bool {
	if s.data[s.pos] != '{' {
		return false
	}
	s.pos++
	for s.space(); s.data[s.pos] != '}'; s.next() {
		// A member name only matters for duplicates and error paths, so its escapes need no decoding.
		s.string()
		s.space()
		s.pos++ // the colon
		s.space()
		if s.data[s.pos] != '{' || !s.node() {
			return false
		}
	}
	s.pos++
	return true
}

func (s *scan) anyOf() bool {
	if s.data[s.pos] != '[' {
		return false
	}
	s.pos++
	s.space()
	if s.data[s.pos] == ']' {
		return false
	}
	for ; s.data[s.pos] != ']'; s.next() {
		if s.data[s.pos] != '{' || !s.node() {
			return false
		}
	}
	s.pos++
	return true
}

func (s *scan) enum() bool {
	start := s.pos
	if s.data[start] != '[' {
		return false
	}
	s.pos++
	s.space()
	if s.data[s.pos] == ']' {
		return false
	}
	s.pos = start
	return s.skip(true)
}

// required reads an array of strings, where null counts as a string as with encoding/json.
func (s *scan) required() bool {
	if s.data[s.pos] != '[' {
		return false
	}
	s.pos++
	for s.space(); s.data[s.pos] != ']'; s.next() {
		switch s.data[s.pos] {
		case '"':
			s.string()
		case 'n':
			s.pos += len("null")
		default:
			return false
		}
	}
	s.pos++
	return true
}

func (s *scan) boolean() bool {
	switch s.data[s.pos] {
	case 't':
		s.pos += len("true")
		return true
	case 'f':
		s.pos += len("false")
		return true
	}
	return false
}

// count reads a count bound. Only plain digits that fit an int on every platform are recognized.
func (s *scan) count(bound *int) bool {
	literal := s.number()
	if len(literal) == 0 || len(literal) > 9 {
		return false
	}
	n := 0
	for _, c := range literal {
		if c < '0' || c > '9' {
			return false
		}
		n = n*10 + int(c-'0')
	}
	*bound = n
	return true
}

// limit reads a minimum or maximum.
func (s *scan) limit() bool {
	return fitsFloat(s.number())
}

// fitsFloat recognizes a number literal that strconv.ParseFloat reads without a range error: without an
// exponent, fewer than 309 digits stay below the largest float64.
func fitsFloat(literal []byte) bool {
	return len(literal) > 0 && len(literal) <= 300 && bytes.IndexAny(literal, "eE") < 0
}

// number returns the number literal at the position, or nothing if the value is not a number.
func (s *scan) number() []byte {
	start := s.pos
	for s.pos < len(s.data) {
		switch c := s.data[s.pos]; {
		case c >= '0' && c <= '9', c == '-', c == '+', c == '.', c == 'e', c == 'E':
			s.pos++
			continue
		}
		break
	}
	return s.data[start:s.pos]
}

// string steps past a string and returns its content between the quotes. plain is false if the value is
// not a string or the string holds an escape.
func (s *scan) string() (content []byte, plain bool) {
	if s.data[s.pos] != '"' {
		return nil, false
	}
	start := s.pos + 1
	plain = true
	for s.pos++; ; s.pos++ {
		switch s.data[s.pos] {
		case '"':
			s.pos++
			return s.data[start : s.pos-1], plain
		case '\\':
			plain = false
			s.pos++
		}
	}
}

// skip steps past any value. With numbers set, it recognizes the value only if every number in it fits a
// float64, which encoding/json requires to decode it into an any.
func (s *scan) skip(numbers bool) bool {
	depth := 0
	for {
		switch c := s.data[s.pos]; c {
		case '"':
			s.string()
		case '{', '[':
			depth++
			s.pos++
		case '}', ']':
			depth--
			s.pos++
		case 't', 'n':
			s.pos += 4
		case 'f':
			s.pos += 5
		case ' ', '\t', '\r', '\n', ',', ':':
			s.pos++
			continue
		default:
			if literal := s.number(); numbers && !fitsFloat(literal) {
				return false
			}
		}
		if depth == 0 {
			return true
		}
	}
}

// pattern reads a pattern and parses it as checkNode does, decoding the string the way encoding/json does.
func (s *scan) pattern() bool {
	quoted := s.pos
	if s.data[quoted] != '"' {
		return false
	}
	content, plain := s.string()
	if !plain {
		if content = unescape(s.buf[:0], content); content != nil {
			s.buf = content
		}
	}
	var pattern string
	if content != nil && utf8.Valid(content) {
		pattern = string(content)
	} else if err := json.Unmarshal(s.data[quoted:s.pos], &pattern); err != nil {
		return false
	}
	_, err := syntax.Parse(pattern, syntax.Perl)
	return err == nil
}

// unescape appends the decoded content of a string to dst. It returns nil for a \u escape, which it leaves
// to encoding/json.
func unescape(dst, content []byte) []byte {
	for i := 0; i < len(content); i++ {
		c := content[i]
		if c != '\\' {
			dst = append(dst, c)
			continue
		}
		i++
		switch content[i] {
		case 'b':
			c = '\b'
		case 'f':
			c = '\f'
		case 'n':
			c = '\n'
		case 'r':
			c = '\r'
		case 't':
			c = '\t'
		case 'u':
			return nil
		default: // ", \ and /
			c = content[i]
		}
		dst = append(dst, c)
	}
	return dst
}
