//go:build linux || darwin

package exteditor

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"golang.org/x/sys/unix"
	"golang.org/x/term"

	"github.com/castrowithcee/qatlas-kit/ptytest"
)

const (
	// fakeEditorEnv selects what the fake editor does to the copy.
	fakeEditorEnv = "EXTEDITOR_TEST_FAKE"
	// fakeDataEnv passes a value to the fake editor.
	fakeDataEnv = "EXTEDITOR_TEST_DATA"
	// programEnv makes the program helper run and names its private directory.
	programEnv = "EXTEDITOR_TEST_PROGRAM"
)

// TestFakeEditor is the editor the tests start; it does nothing in a normal run. The copy is its last
// argument.
func TestFakeEditor(t *testing.T) {
	mode := os.Getenv(fakeEditorEnv)
	if mode == "" {
		return
	}
	path := os.Args[len(os.Args)-1]
	data := os.Getenv(fakeDataEnv)
	if err := fakeEdit(mode, path, data); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	os.Exit(0)
}

func fakeEdit(mode, path, data string) error {
	appendText := func(s string) error {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			return err
		}
		_, err = f.WriteString(s)
		return errors.Join(err, f.Close())
	}
	replace := func(make func() error) error {
		if err := os.Remove(path); err != nil {
			return err
		}
		return make()
	}
	switch mode {
	case "keep":
		return nil
	case "append":
		return appendText("added\n")
	case "fail":
		_ = appendText("added\n")
		os.Exit(3)
	case "abort":
		_ = appendText("added\n")
		return syscall.Kill(os.Getpid(), syscall.SIGKILL)
	case "args":
		return os.WriteFile(data, []byte(strings.Join(os.Args, "\n")), 0o600)
	case "rename":
		tmp := path + ".new"
		if err := os.WriteFile(tmp, []byte("renamed\n"), 0o644); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	case "symlink":
		return replace(func() error { return os.Symlink(data, path) })
	case "hardlink":
		return replace(func() error { return os.Link(data, path) })
	case "fifo":
		return replace(func() error { return unix.Mkfifo(path, 0o600) })
	case "dir":
		return replace(func() error { return os.Mkdir(path, 0o700) })
	case "delete":
		return os.Remove(path)
	case "binary":
		return appendText("\x00")
	case "latin1":
		return appendText("caf\xe9")
	case "grow":
		return appendText(strings.Repeat("x", 64))
	case "tty":
		if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
			return errors.New("the standard streams are no terminal")
		}
		fmt.Println("editor-ready")
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return err
		}
		return appendText(strings.TrimRight(line, "\r\n") + "\n")
	}
	return fmt.Errorf("unknown mode %q", mode)
}

// request returns a valid request for the fake editor in a new private directory.
func request(t *testing.T) Request {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Request{Editor: Editor{Path: self, Args: []string{"-test.run=^TestFakeEditor$"}}, Dir: privateDir(t),
		Name: "notes.md", Text: []byte("sample text\n"), MaxSize: 32}
}

func privateDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func assertEmpty(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("%s still holds %v", dir, entries)
	}
}

// edit runs a prepared edit with the fake editor in mode, without a terminal, and checks that the copy
// is gone afterwards.
func edit(t *testing.T, r Request, mode, data string) tea.Msg {
	t.Helper()
	s, err := prepare(r)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeEditorEnv, mode)
	t.Setenv(fakeDataEnv, data)
	msg := s.finish(s.cmd.Run())
	assertEmpty(t, r.Dir)
	return msg
}

func failure(t *testing.T, msg tea.Msg, want error) {
	t.Helper()
	failed, ok := msg.(FailedMsg)
	if !ok || !errors.Is(failed.Err, want) {
		t.Fatalf("msg = %#v, want a FailedMsg with %v", msg, want)
	}
	if strings.Contains(failed.Err.Error(), "sample text") {
		t.Errorf("the error quotes the text: %v", failed.Err)
	}
}

func TestEditReturnsTheChange(t *testing.T) {
	r := request(t)
	if msg := edit(t, r, "append", ""); !reflect.DeepEqual(msg, ChangedMsg{Text: []byte("sample text\nadded\n")}) {
		t.Fatalf("msg = %#v", msg)
	}
	if msg := edit(t, r, "rename", ""); !reflect.DeepEqual(msg, ChangedMsg{Text: []byte("renamed\n")}) {
		t.Fatalf("an editor that saves by renaming: msg = %#v", msg)
	}
}

func TestEditRecognizesAnUnchangedText(t *testing.T) {
	if msg := edit(t, request(t), "keep", ""); msg != (UnchangedMsg{}) {
		t.Fatalf("msg = %#v", msg)
	}
}

func TestEditorFailureAndAbortDiscardTheCopy(t *testing.T) {
	for _, mode := range []string{"fail", "abort"} {
		failure(t, edit(t, request(t), mode, ""), ErrEditorFailed)
	}
	r := request(t)
	r.Editor.Path = filepath.Join(t.TempDir(), "missing-editor")
	failure(t, edit(t, r, "keep", ""), ErrEditorFailed)
}

func TestEditPassesARandomCopyWithTheExtension(t *testing.T) {
	r := request(t)
	r.Editor.Args = append(r.Editor.Args, "sample-arg")
	var copies []string
	for range 2 {
		record := filepath.Join(t.TempDir(), "args")
		edit(t, r, "args", record)
		data, err := os.ReadFile(record)
		if err != nil {
			t.Fatal(err)
		}
		args := strings.Split(string(data), "\n")
		cp := args[len(args)-1]
		if filepath.Dir(cp) != r.Dir || filepath.Ext(cp) != ".md" || cp == filepath.Join(r.Dir, r.Name) ||
			args[len(args)-2] != "sample-arg" {
			t.Fatalf("args = %q", args)
		}
		copies = append(copies, cp)
	}
	if copies[0] == copies[1] {
		t.Errorf("both copies are named %s", copies[0])
	}
}

func TestEditRefusesAnUnsafeCopy(t *testing.T) {
	other := filepath.Join(t.TempDir(), "other.txt")
	if err := os.WriteFile(other, []byte("other\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for mode, reason := range map[string]string{"symlink": "symbolic link", "hardlink": "2 links",
		"fifo": "regular file", "dir": "regular file"} {
		r := request(t)
		s, err := prepare(r)
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv(fakeEditorEnv, mode)
		t.Setenv(fakeDataEnv, other)
		msg := s.finish(s.cmd.Run())
		if mode == "dir" {
			// The directory in place of the copy cannot be unlinked; the failure says so.
			_ = os.Remove(filepath.Join(r.Dir, s.name))
		}
		assertEmpty(t, r.Dir)
		failure(t, msg, ErrUnsafeCopy)
		if err := msg.(FailedMsg).Err; !strings.Contains(err.Error(), reason) {
			t.Errorf("%s: %v does not name %q", mode, err, reason)
		}
	}
}

func TestEditRefusesACopyOfAnotherUser(t *testing.T) {
	r := request(t)
	s, err := prepare(r)
	if err != nil {
		t.Fatal(err)
	}
	s.owner++
	t.Setenv(fakeEditorEnv, "keep")
	msg := s.finish(s.cmd.Run())
	assertEmpty(t, r.Dir)
	failure(t, msg, ErrUnsafeCopy)
	if err := msg.(FailedMsg).Err; !strings.Contains(err.Error(), "belongs to user") {
		t.Errorf("%v does not name the owner", err)
	}
}

func TestEditRefusesAnInvalidReturn(t *testing.T) {
	for mode, want := range map[string]error{"binary": ErrBinary, "latin1": ErrNotUTF8, "grow": ErrTooLarge,
		"delete": fs.ErrNotExist} {
		failure(t, edit(t, request(t), mode, ""), want)
	}
}

func TestEditRefusesAnInvalidInputAndLeavesNothing(t *testing.T) {
	for text, want := range map[string]error{strings.Repeat("x", 33): ErrTooLarge, "a\x00": ErrBinary, "\xff": ErrNotUTF8} {
		r := request(t)
		r.Text = []byte(text)
		if _, err := Edit(r); !errors.Is(err, want) {
			t.Errorf("Edit(%q) = %v, want %v", text, err, want)
		}
		assertEmpty(t, r.Dir)
	}
}

func TestEditRefusesAnUnsafeDirectory(t *testing.T) {
	base := t.TempDir()
	good := privateDir(t)
	link := filepath.Join(base, "link")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(base, "file")
	if err := os.WriteFile(file, nil, 0o700); err != nil {
		t.Fatal(err)
	}
	cases := map[string]struct{ dir, reason string }{"symbolic link": {link, "is a symbolic link"},
		"regular file": {file, "is not a directory"}}
	for _, mode := range []os.FileMode{0o755, 0o750, 0o701, 0o710, 0o500} {
		dir := filepath.Join(base, mode.String())
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		cases["mode "+mode.String()] = struct{ dir, reason string }{dir, "has mode"}
	}
	for name, c := range cases {
		r := request(t)
		r.Dir = c.dir
		if _, err := Edit(r); !errors.Is(err, ErrUnsafeDir) || !strings.Contains(err.Error(), c.reason) {
			t.Errorf("%s: Edit = %v, want ErrUnsafeDir that names %q", name, err, c.reason)
		}
	}
	assertEmpty(t, good)
	r := request(t)
	r.Dir = filepath.Join(base, "missing")
	if _, err := Edit(r); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing directory: Edit = %v", err)
	}
}

func TestEditRefusesADirectoryOfAnotherUser(t *testing.T) {
	r := request(t)
	uid := os.Getuid()
	r.geteuid = func() int { return uid + 1 }
	if _, err := Edit(r); !errors.Is(err, ErrUnsafeDir) || !strings.Contains(err.Error(), "belongs to user") {
		t.Errorf("Edit = %v, want ErrUnsafeDir for the owner", err)
	}
	r.geteuid = func() int { return 0 }
	r.Target = &Target{UID: uint32(uid + 1), GID: uint32(os.Getgid()), Home: "/home/sample"}
	if _, err := Edit(r); !errors.Is(err, ErrUnsafeDir) || !strings.Contains(err.Error(), "belongs to user") {
		t.Errorf("target: Edit = %v, want ErrUnsafeDir for the owner", err)
	}
	assertEmpty(t, r.Dir)
}

func TestEditRefusesRootWithoutTarget(t *testing.T) {
	r := request(t)
	r.geteuid = func() int { return 0 }
	if _, err := Edit(r); !errors.Is(err, ErrRoot) {
		t.Errorf("Edit = %v, want ErrRoot", err)
	}
	r.Target = &Target{UID: 0, Home: "/root"}
	if _, err := Edit(r); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("root target: Edit = %v, want ErrInvalidRequest", err)
	}
	r = request(t)
	r.Target = &Target{UID: uint32(os.Getuid()), GID: uint32(os.Getgid()), Home: "/home/sample"}
	if _, err := Edit(r); !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("target without root: Edit = %v, want ErrInvalidRequest", err)
	}
	assertEmpty(t, r.Dir)
}

// As root with a target user, the copy belongs to the target and the editor gets its identity without
// supplementary groups and only the listed environment. The command is not run, since dropping groups
// needs root.
func TestEditRunsAsTheTargetUser(t *testing.T) {
	t.Setenv("TERM", "xterm-sample")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_ALL", "")
	t.Setenv("SAMPLE_TOKEN", "invented")
	for _, key := range []string{"COLORTERM", "LC_CTYPE", "LC_MESSAGES"} {
		t.Setenv(key, "")
		_ = os.Unsetenv(key)
	}
	uid, gid := uint32(os.Getuid()), uint32(os.Getgid())
	r := request(t)
	r.geteuid = func() int { return 0 }
	r.Target = &Target{UID: uid, GID: gid, Home: "/home/sample"}
	s, err := prepare(r)
	if err != nil {
		t.Fatal(err)
	}
	want := &syscall.Credential{Uid: uid, Gid: gid, Groups: []uint32{}}
	if got := s.cmd.SysProcAttr.Credential; !reflect.DeepEqual(got, want) || got.Groups == nil || got.NoSetGroups {
		t.Errorf("credential = %+v, want %+v", got, want)
	}
	env := slices.Clone(s.cmd.Env)
	slices.Sort(env)
	if wantEnv := []string{"HOME=/home/sample", "LANG=en_US.UTF-8", "LC_ALL=", "PATH=" + targetPath, "TERM=xterm-sample"}; !reflect.DeepEqual(env, wantEnv) {
		t.Errorf("env = %q, want %q", env, wantEnv)
	}
	if s.cmd.Dir != r.Dir {
		t.Errorf("dir = %q, want %q", s.cmd.Dir, r.Dir)
	}
	info, err := os.Lstat(filepath.Join(r.Dir, s.name))
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	if st.Uid != uid || st.Gid != gid || info.Mode() != 0o600 {
		t.Errorf("copy: owner %d:%d, mode %v", st.Uid, st.Gid, info.Mode())
	}
	if msg := s.finish(nil); msg != (UnchangedMsg{}) {
		t.Errorf("msg = %#v", msg)
	}
	assertEmpty(t, r.Dir)
}

func TestEditWithoutTargetInheritsTheEnvironment(t *testing.T) {
	s, err := prepare(request(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.finish(nil)
	if s.cmd.Env != nil || s.cmd.SysProcAttr != nil {
		t.Errorf("env = %q, attributes = %+v; want both inherited", s.cmd.Env, s.cmd.SysProcAttr)
	}
}

// editProgram starts the edit and shows its result.
type editProgram struct {
	request Request
	result  string
}

func (m editProgram) Init() tea.Cmd {
	cmd, err := Edit(m.request)
	if err != nil {
		return func() tea.Msg { return FailedMsg{Err: err} }
	}
	return cmd
}

func (m editProgram) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case ChangedMsg:
		m.result = fmt.Sprintf("changed %q", msg.Text)
	case UnchangedMsg:
		m.result = "unchanged"
	case FailedMsg:
		m.result = "failed: " + msg.Err.Error()
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m editProgram) View() tea.View {
	if m.result == "" {
		return tea.NewView("waiting")
	}
	return tea.NewView("result: " + m.result)
}

// TestEditProgramHelper is the program the terminal test starts; it does nothing in a normal run.
func TestEditProgramHelper(t *testing.T) {
	dir := os.Getenv(programEnv)
	if dir == "" {
		return
	}
	self, err := os.Executable()
	if err != nil {
		os.Exit(1)
	}
	r := Request{Editor: Editor{Path: self, Args: []string{"-test.run=^TestFakeEditor$"}}, Dir: dir,
		Name: "notes.txt", Text: []byte("before\n"), MaxSize: 64}
	if _, err := tea.NewProgram(editProgram{request: r}).Run(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

// The editor gets the terminal of the program, reads what is typed there, and the program resumes with
// the result.
func TestEditHandsTheTerminalToTheEditor(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := privateDir(t)
	pty, err := ptytest.Start(ptytest.Command{Path: self, Args: []string{"-test.run=^TestEditProgramHelper$"},
		Env: []string{programEnv + "=" + dir, fakeEditorEnv + "=tty", "TERM=xterm-256color"}})
	if errors.Is(err, errors.ErrUnsupported) {
		t.Skipf("no pseudo-terminal: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pty.Close() })
	if err := pty.WaitForText(15*time.Second, "editor-ready"); err != nil {
		t.Fatalf("%v: %q", err, pty.Output())
	}
	if _, err := pty.Write([]byte("typed words\r")); err != nil {
		t.Fatal(err)
	}
	if err := pty.WaitForText(15*time.Second, `result: changed "before\ntyped words\n"`); err != nil {
		t.Fatalf("%v: %q", err, pty.Output())
	}
	assertEmpty(t, dir)
	if _, err := pty.Write([]byte{0x03}); err != nil {
		t.Fatal(err)
	}
	if code, err := pty.Wait(15 * time.Second); err != nil || code != 0 {
		t.Fatalf("the program ended with %d, %v: %q", code, err, strings.TrimSpace(pty.Output()))
	}
}
