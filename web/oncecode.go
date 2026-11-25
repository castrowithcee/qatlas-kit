package web

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	// 32 symbols without I and O, so every symbol carries exactly 5 bit and none is easily misread.
	codeAlphabet   = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	codeLength     = 8 // 40 bit; the throttle of Authenticator bounds the guesses
	defaultCodeTTL = 5 * time.Minute

	// CodeField is the form field OnceCode reads the submitted code from.
	CodeField = "code"
)

// LoginMethod is how a person proves who they are. Authenticator calls it once per attempt. The method
// reads what it needs from the request, such as a form field, and must not log or echo it. A method for a
// later procedure with more than one round trip keeps its own state and routes.
type LoginMethod interface {
	// Verify reports whether r carries valid credentials. A false answer is an ordinary wrong attempt; an
	// error means the method itself failed.
	Verify(r *http.Request) (ok bool, err error)
}

// OnceCodeOptions configure OnceCode.
type OnceCodeOptions struct {
	// Lifetime is how long a code stays valid; 0 means 5 minutes.
	Lifetime time.Duration
	// Redactor, if set, learns every code, so a code that reaches a log is removed.
	Redactor *redact.Redactor
}

// OnceCode is a LoginMethod with a code the application shows the person in the terminal. One code is
// valid at a time, once and for a short time. A wrong guess does not consume it, so a stranger cannot burn
// the code of the person; Authenticator limits the guesses instead.
type OnceCode struct {
	mu     sync.Mutex
	life   time.Duration
	redact *redact.Redactor
	now    func() time.Time

	digest  [sha256.Size]byte
	expires time.Time
	live    bool
}

// NewOnceCode returns a OnceCode that has no valid code yet.
func NewOnceCode(opts OnceCodeOptions) *OnceCode {
	o := &OnceCode{life: opts.Lifetime, redact: opts.Redactor, now: time.Now}
	if o.life <= 0 {
		o.life = defaultCodeTTL
	}
	return o
}

// Generate makes a new code, ends the previous one and returns the code for the application to print,
// in the form ABCD-EFGH. Only the application calls it, never a browser request, so a request cannot make
// a code appear or reset one.
func (o *OnceCode) Generate() string {
	var b [codeLength]byte
	_, _ = rand.Read(b[:]) // never fails since Go 1.24; it crashes the program instead
	for i := range b {
		b[i] = codeAlphabet[b[i]&31]
	}
	code := string(b[:])
	o.mu.Lock()
	o.digest, o.expires, o.live = sha256.Sum256(b[:]), o.now().Add(o.life), true
	o.mu.Unlock()
	if o.redact != nil {
		o.redact.Add(code, code[:4]+"-"+code[4:])
	}
	return code[:4] + "-" + code[4:]
}

// Verify reads the code from the form field CodeField of the request body, never from the URL. A correct
// code ends it. Case, spaces and hyphens are ignored.
func (o *OnceCode) Verify(r *http.Request) (bool, error) {
	got := strings.Map(func(c rune) rune {
		if c == ' ' || c == '-' {
			return -1
		}
		return c
	}, strings.ToUpper(r.PostFormValue(CodeField)))
	digest := sha256.Sum256([]byte(got))

	o.mu.Lock()
	defer o.mu.Unlock()
	if o.live && !o.now().Before(o.expires) {
		o.live = false
	}
	// Equal-length digests make the comparison independent of where the codes differ.
	match := subtle.ConstantTimeCompare(digest[:], o.digest[:]) == 1
	if !o.live || !match {
		return false, nil
	}
	o.live = false
	return true, nil
}
