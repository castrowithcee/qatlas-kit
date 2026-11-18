package localipc

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"net"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// requireRefusal fails unless err is an ErrRefused with a *RefusalError of the
// given reason.
func requireRefusal(t *testing.T, err error, reason RefusalReason) {
	t.Helper()
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("error %v is not ErrRefused", err)
	}
	var refusal *RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("error %v carries no *RefusalError", err)
	}
	if refusal.Reason != reason {
		t.Fatalf("reason = %q, want %q (error %v)", refusal.Reason, reason, err)
	}
}

func TestRefusalTextAndChain(t *testing.T) {
	t.Run("plain text", func(t *testing.T) {
		err := refusal(RefusedOtherUser, "peer has another user ID")
		requireRefusal(t, err, RefusedOtherUser)
		if got, want := err.Error(), "local channel refused: peer has another user ID"; got != want {
			t.Fatalf("text = %q, want %q", got, want)
		}
	})
	t.Run("errno and permission", func(t *testing.T) {
		err := refusal(RefusedNotPrivate, "%q: %w", "dir", syscall.EACCES)
		requireRefusal(t, err, RefusedNotPrivate)
		if got, want := err.Error(), `local channel refused: "dir": `+syscall.EACCES.Error(); got != want {
			t.Fatalf("text = %q, want %q", got, want)
		}
		if !errors.Is(err, syscall.EACCES) || !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("chain lost the errno or fs.ErrPermission: %v", err)
		}
	})
	t.Run("program replaced", func(t *testing.T) {
		err := refusal(RefusedProgramReplaced, "%w", ErrProgramReplaced)
		requireRefusal(t, err, RefusedProgramReplaced)
		if got, want := err.Error(), "local channel refused: this program was removed or replaced since it started"; got != want {
			t.Fatalf("text = %q, want %q", got, want)
		}
		if !errors.Is(err, ErrProgramReplaced) {
			t.Fatalf("chain lost ErrProgramReplaced: %v", err)
		}
	})
	t.Run("verb v hides the cause", func(t *testing.T) {
		err := refusal(RefusedUnidentified, "cannot inspect peer: %v", syscall.EPERM)
		requireRefusal(t, err, RefusedUnidentified)
		if errors.Is(err, syscall.EPERM) {
			t.Fatalf("a %%v cause must stay unreachable: %v", err)
		}
	})
	t.Run("label and peer error", func(t *testing.T) {
		conn, other := net.Pipe()
		defer other.Close()
		check := func(net.Conn) (int, error) { return 42, refusal(RefusedOtherProgram, "peer runs another program") }
		_, err := establish(context.Background(), conn, "tool channel", check, nil)
		requireRefusal(t, err, RefusedOtherProgram)
		var peer *PeerError
		if !errors.As(err, &peer) || peer.PID != 42 {
			t.Fatalf("no *PeerError with the process id in %v", err)
		}
		want := "tool channel: local channel refused: peer runs another program (process 42)"
		if err.Error() != want {
			t.Fatalf("text = %q, want %q", err.Error(), want)
		}
	})
}

func TestBoundedRefusalReason(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	boom := errors.New("boom")
	err := bounded(context.Background(), a, RefusedVersionCheck, "the exchange failed", func() error { return boom })
	requireRefusal(t, err, RefusedVersionCheck)
	if got, want := err.Error(), "local channel refused: the exchange failed: boom"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	if !errors.Is(err, boom) {
		t.Fatalf("chain lost the exchange error: %v", err)
	}
	err = runChallenge(context.Background(), a, func(context.Context, net.Conn) error { return boom })
	requireRefusal(t, err, RefusedChallenge)
	if got, want := err.Error(), "local channel refused: the challenge failed: boom"; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
}

// TestErrRefusedOnlyInRefusalHelper keeps every refusal on the typed helper,
// including syscall failures no test can trigger.
func TestErrRefusedOnlyInRefusalHelper(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	scanned := 0
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Name.Name == "refusal" && fn.Recv == nil {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok || !isFmtErrorf(call) {
					return true
				}
				for _, arg := range call.Args {
					ast.Inspect(arg, func(m ast.Node) bool {
						if id, ok := m.(*ast.Ident); ok && id.Name == "ErrRefused" {
							t.Errorf("%s: ErrRefused in fmt.Errorf; use refusal", fset.Position(id.Pos()))
						}
						return true
					})
				}
				return true
			})
		}
	}
	if scanned < 10 {
		t.Fatalf("scanned only %d source files", scanned)
	}
}

func isFmtErrorf(call *ast.CallExpr) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Errorf" {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "fmt"
}
