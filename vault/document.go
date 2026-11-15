package vault

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"filippo.io/age"
)

// DocumentErrorKind tells what is wrong with a document.
type DocumentErrorKind string

// The kinds of document error.
const (
	// DocumentDecrypt means the document file does not decrypt with the vault key.
	DocumentDecrypt DocumentErrorKind = "decrypt"
	// DocumentDecode means the document is not a JSON object with an integer schema version.
	DocumentDecode DocumentErrorKind = "decode"
	// DocumentSchema means the document's schema version is not one of Options.Schemas.
	DocumentSchema DocumentErrorKind = "schema"
)

// DecryptCause tells why a vault file does not decrypt. It is the answer to the question what to check
// first: the key, the file's origin, or the file's integrity.
type DecryptCause string

// The causes of a decryption failure. A failure that cannot be told apart safely is CauseDamaged.
const (
	// CauseWrongKey means the file is an age file that was not encrypted to this vault's key.
	CauseWrongKey DecryptCause = "wrong-key"
	// CauseNotAge means the file does not start like an age file, for instance a plain text file.
	CauseNotAge DecryptCause = "not-age"
	// CauseDamaged means the file starts like an age file but its header or payload is damaged or was
	// altered.
	CauseDamaged DecryptCause = "damaged"
)

// text is the fixed description of the cause; it never carries library text or file content.
func (c DecryptCause) text() string {
	switch c {
	case CauseWrongKey:
		return "the file was not encrypted to this vault's key"
	case CauseNotAge:
		return "the file is not an age file"
	}
	return "the file is damaged or was altered"
}

// ageIntro is the first line of every binary age file.
var ageIntro = []byte("age-encryption.org/v1\n")

// decryptCauseOf classifies a failed decryption of data from the type of the library's error and the
// file's first bytes, never from the library's message.
func decryptCauseOf(data []byte, err error) DecryptCause {
	var noMatch *age.NoIdentityMatchError
	switch {
	case !bytes.HasPrefix(data, ageIntro):
		return CauseNotAge
	case errors.As(err, &noMatch):
		return CauseWrongKey
	}
	return CauseDamaged
}

// decryptFailure is the error of a file at path that did not decrypt. It drops the library's error.
func decryptFailure(path string, data []byte, err error) *DocumentError {
	return &DocumentError{Path: path, Kind: DocumentDecrypt, Cause: decryptCauseOf(data, err)}
}

// DocumentError reports a document that cannot be used. It wraps ErrDamaged for DocumentDecrypt and
// DocumentDecode and ErrUnsupportedSchema for DocumentSchema. It carries no library text and no content.
type DocumentError struct {
	// Path is the file the document was read from or is written to.
	Path string
	Kind DocumentErrorKind
	// Cause is why the file does not decrypt, set only for DocumentDecrypt.
	Cause DecryptCause
	// Version is the schema version found, set only for DocumentSchema.
	Version int
}

func (e *DocumentError) Error() string {
	switch e.Kind {
	case DocumentDecrypt:
		return fmt.Sprintf("cannot decrypt %s: %s", e.Path, e.Cause.text())
	case DocumentSchema:
		return fmt.Sprintf("%s: %s: %d", e.Path, ErrUnsupportedSchema, e.Version)
	}
	return fmt.Sprintf("%s is not a versioned JSON object: %s", e.Path, ErrDamaged)
}

// Unwrap returns the sentinel that matches the kind.
func (e *DocumentError) Unwrap() error {
	if e.Kind == DocumentSchema {
		return ErrUnsupportedSchema
	}
	return ErrDamaged
}

// checkSchema refuses a document that is not a JSON object with an accepted integer schema version. path
// names the file for the error.
func checkSchema(path string, document []byte, schemas []int) error {
	var envelope struct {
		Schema *int `json:"schema"`
	}
	if err := json.Unmarshal(document, &envelope); err != nil || envelope.Schema == nil {
		return &DocumentError{Path: path, Kind: DocumentDecode}
	}
	if !slices.Contains(schemas, *envelope.Schema) {
		return &DocumentError{Path: path, Kind: DocumentSchema, Version: *envelope.Schema}
	}
	return nil
}

// readDocument validates a document just read from path. A file of 0 bytes becomes Options.EmptyDocument
// when set. On error plain is cleared.
func (v *Vault) readDocument(path string, plain []byte) ([]byte, error) {
	if len(plain) == 0 && v.opts.EmptyDocument != nil {
		return slices.Clone(v.opts.EmptyDocument), nil
	}
	if err := checkSchema(path, plain, v.opts.Schemas); err != nil {
		clear(plain)
		return nil, err
	}
	return plain, nil
}

// filterDocument runs Options.SwitchFilter over a copy of document and returns the checked result. The
// copy is cleared; so is the result on error. A nil filter returns a copy of document.
func (v *Vault) filterDocument(path string, document []byte) ([]byte, error) {
	in := slices.Clone(document)
	if v.opts.SwitchFilter == nil {
		return in, nil
	}
	defer clear(in)
	out, err := v.opts.SwitchFilter(in)
	if err != nil {
		clear(out)
		return nil, fmt.Errorf("the vault document filter failed: %w", err)
	}
	// The filter may return the slice it was given; keep a private copy either way.
	out = bytes.Clone(out)
	if err := checkSchema(path, out, v.opts.Schemas); err != nil {
		clear(out)
		return nil, err
	}
	return out, nil
}
