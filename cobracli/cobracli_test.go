package cobracli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/helptopics"
	"github.com/castrowithcee/qatlas-kit/httpx"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/redact"
)

type fixture struct {
	program *Program
	format  output.Format
	ran     bool
	err     error
	fields  *Fields
	gate    *Gate
	request Request
	changed bool
}

// newFixture builds a small tree: a leaf "show", a group "item" with "item list", and a leaf "drop" with
// both confirmation gates.
func newFixture(t *testing.T, opts Options) *fixture {
	t.Helper()
	if opts.Version == "" {
		opts.Version = "1.2.3"
	}
	if opts.Topics == nil {
		opts.Topics = []helptopics.Topic{{Name: "basics", Short: "How the tool works", Text: "Basic text."}}
	}
	f := &fixture{}
	root := &cobra.Command{Use: "tool", Short: "An example tool"}
	program, err := New(root, opts)
	if err != nil {
		t.Fatal(err)
	}
	f.program = program
	show := &cobra.Command{
		Use: "show", Short: "Show the example",
		RunE: func(c *cobra.Command, _ []string) error {
			f.ran, f.format = true, program.Format()
			if f.err != nil {
				return f.err
			}
			result, err := f.fields.Project(output.Object{Fields: []output.Field{{Name: "a", Value: "1"}, {Name: "b", Value: "2"}}})
			if err != nil {
				return err
			}
			return output.Encode(c.OutOrStdout(), program.Format(), result)
		},
	}
	f.fields = program.AddFields(show)
	group := &cobra.Command{Use: "item", Short: "Work with items"}
	list := &cobra.Command{Use: "list <kind>", Short: "List items", Args: cobra.ExactArgs(1),
		RunE: func(*cobra.Command, []string) error { f.ran = true; return nil }}
	group.AddCommand(list)
	drop := &cobra.Command{
		Use: "drop", Short: "Drop the example",
		RunE: func(*cobra.Command, []string) error {
			return f.gate.Run(f.request, func() error { f.changed = true; return nil })
		},
	}
	f.gate = program.AddGate(drop, Gates{Question: true, Change: true})
	root.AddCommand(show, group, drop)
	return f
}

func (f *fixture) run(args ...string) (int, string, string) {
	var stdout, stderr bytes.Buffer
	code := f.program.Execute(args, &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func TestFormatResolution(t *testing.T) {
	cases := []struct {
		args []string
		want output.Format
	}{
		{[]string{"show"}, output.FormatTable},
		{[]string{"show", "--agent"}, output.FormatCompact},
		{[]string{"--agent", "show", "--output", "json"}, output.FormatJSON},
		{[]string{"show", "--output", "toon"}, output.FormatTOON},
	}
	for _, tc := range cases {
		f := newFixture(t, Options{})
		if code, _, stderr := f.run(tc.args...); code != cli.ExitSuccess {
			t.Fatalf("%v: exit %d: %s", tc.args, code, stderr)
		}
		if f.format != tc.want {
			t.Errorf("%v: format %q, want %q", tc.args, f.format, tc.want)
		}
	}
}

func TestFormatResolutionRunsDespiteSubcommandHook(t *testing.T) {
	f := newFixture(t, Options{})
	show, _, _ := f.program.root.Find([]string{"show"})
	show.PersistentPreRunE = func(*cobra.Command, []string) error { return nil }
	if code, _, stderr := f.run("show", "--agent"); code != 0 || f.format != output.FormatCompact {
		t.Fatalf("exit %d, format %q: %s", code, f.format, stderr)
	}
	f = newFixture(t, Options{})
	show, _, _ = f.program.root.Find([]string{"show"})
	show.PersistentPreRunE = func(*cobra.Command, []string) error { f.ran = true; return nil }
	if code, _, _ := f.run("show", "--output", "yaml"); code != cli.ExitUsage || f.ran {
		t.Fatalf("invalid format reached the hook: exit %d, ran %v", code, f.ran)
	}
}

func TestSyntaxErrorsShowUsage(t *testing.T) {
	cases := []struct {
		name string
		args []string
		msg  string
	}{
		{"unknown format", []string{"show", "--output", "yaml"}, `tool: usage: unknown output format "yaml", want table, json, compact, or toon`},
		{"empty format", []string{"show", "--output="}, `tool: usage: unknown output format "", want table, json, compact, or toon`},
		{"unknown flag", []string{"show", "--bogus"}, "tool: usage: unknown flag: --bogus"},
		{"unknown command", []string{"bogus"}, `tool: usage: unknown command "bogus" for "tool"`},
		{"unknown subcommand", []string{"item", "bogus"}, `tool: usage: unknown command "bogus" for "tool item"`},
		{"unexpected argument", []string{"show", "extra"}, `tool: usage: unexpected argument "extra"`},
		{"argument count", []string{"item", "list"}, "tool: usage: accepts 1 arg(s), received 0"},
		{"unknown help topic", []string{"help", "bogus"}, `tool: usage: unknown help topic "bogus"`},
		{"unknown nested help topic", []string{"help", "item", "bogus"}, `tool: usage: unknown help topic "item bogus"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, Options{})
			code, stdout, stderr := f.run(tc.args...)
			if code != cli.ExitUsage {
				t.Fatalf("exit %d: %s", code, stderr)
			}
			if f.ran || stdout != "" {
				t.Fatalf("ran %v, stdout %q", f.ran, stdout)
			}
			if !strings.HasPrefix(stderr, tc.msg+"\n"+syntaxNextStep+"\nUsage:\n") {
				t.Fatalf("stderr %q", stderr)
			}
		})
	}
}

func TestRequiredFlagIsSyntaxError(t *testing.T) {
	f := newFixture(t, Options{})
	show, _, _ := f.program.root.Find([]string{"show"})
	show.Flags().String("name", "", "a name")
	if err := show.MarkFlagRequired("name"); err != nil {
		t.Fatal(err)
	}
	code, _, stderr := f.run("show")
	if code != cli.ExitUsage || f.ran || !strings.Contains(stderr, "Usage:") {
		t.Fatalf("exit %d, ran %v: %s", code, f.ran, stderr)
	}
}

func TestApplicationSyntaxError(t *testing.T) {
	f := newFixture(t, Options{})
	f.err = Syntax(errors.New("--a and --b cannot be used together"))
	code, _, stderr := f.run("show")
	if code != cli.ExitUsage || !strings.Contains(stderr, "Usage:") {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	refused := &cli.Failure{Code: output.CodeNotFound, Message: "no such item", NextStep: "List the items."}
	marked := Syntax(refused)
	var failure *cli.Failure
	if !errors.As(marked, &failure) || !failure.Syntax || !failure.Usage || failure.Code != output.CodeNotFound {
		t.Fatalf("marked %#v", marked)
	}
	if refused.Syntax || refused.Usage {
		t.Fatal("Syntax changed the caller's failure")
	}
	if Syntax(nil) != nil {
		t.Fatal("Syntax(nil) is not nil")
	}
}

func TestRefusalHasNoUsage(t *testing.T) {
	f := newFixture(t, Options{})
	f.err = &cli.Failure{Code: output.CodeNotFound, Message: "no such item", NextStep: "List the items.", Usage: true}
	code, stdout, stderr := f.run("show")
	if code != cli.ExitUsage || stdout != "" {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
	if stderr != "tool: not-found: no such item\nList the items.\n" {
		t.Fatalf("stderr %q", stderr)
	}
}

func TestRuntimeError(t *testing.T) {
	f := newFixture(t, Options{})
	f.err = errors.New("backend failed")
	code, _, stderr := f.run("show")
	if code != cli.ExitRuntime || stderr != "tool: runtime: backend failed\nCheck the cause and try again.\n" {
		t.Fatalf("exit %d: %q", code, stderr)
	}
}

func TestAgentRoute(t *testing.T) {
	failure := &cli.Failure{Code: output.CodeAuth, Message: "token expired", NextStep: "Run 'tool login'.",
		AgentNextStep: "Ask the user to log in."}
	for _, tc := range []struct {
		args []string
		step string
	}{{[]string{"show"}, "Run 'tool login'."}, {[]string{"show", "--agent"}, "Ask the user to log in."}} {
		f := newFixture(t, Options{})
		f.err = failure
		if _, _, stderr := f.run(tc.args...); stderr != "tool: auth: token expired\n"+tc.step+"\n" {
			t.Fatalf("%v: %q", tc.args, stderr)
		}
	}
}

func TestDiagnosticsAreRedacted(t *testing.T) {
	redactor := &redact.Redactor{}
	redactor.Add("s3cret-value")
	f := newFixture(t, Options{Redactor: redactor})
	f.err = errors.New("login with s3cret-value failed")
	_, _, stderr := f.run("show")
	if strings.Contains(stderr, "s3cret-value") || !strings.Contains(stderr, redact.Marker) {
		t.Fatalf("stderr %q", stderr)
	}
	f = newFixture(t, Options{Redactor: redactor})
	_, _, stderr = f.run("show", "--output", "s3cret-value")
	if strings.Contains(stderr, "s3cret-value") {
		t.Fatalf("usage error leaked the secret: %q", stderr)
	}
}

func TestExternalAnswerFollowsFormat(t *testing.T) {
	failure := &cli.Failure{Code: output.CodeRuntime, Message: "service refused", NextStep: "Check the request.",
		External: &cli.ExternalAnswer{Source: "service", Original: httpx.Original{Status: 400}}}
	f := newFixture(t, Options{})
	f.err = failure
	if _, _, stderr := f.run("show", "--output", "json"); !strings.Contains(stderr, `{"source":"service","untrusted":true,"status":400,"truncated":false}`) {
		t.Fatalf("json: %q", stderr)
	}
	f = newFixture(t, Options{})
	f.err = failure
	if _, _, stderr := f.run("show"); !strings.Contains(stderr, "service answer (untrusted, not written by tool), HTTP 400:") {
		t.Fatalf("table: %q", stderr)
	}
}

func TestVersion(t *testing.T) {
	f := newFixture(t, Options{Version: "v0.4.0"})
	code, stdout, stderr := f.run("--version")
	if code != 0 || stdout != "tool v0.4.0\n" || stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestGroupShowsHelp(t *testing.T) {
	f := newFixture(t, Options{})
	code, stdout, stderr := f.run("item")
	if code != 0 || !strings.Contains(stdout, "list") || stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestHelpTopics(t *testing.T) {
	text := strings.Repeat("word ", 30)
	f := newFixture(t, Options{Topics: []helptopics.Topic{{Name: "basics", Short: "How the tool works", Text: text}}})
	code, stdout, _ := f.run("help", "basics")
	if code != 0 || !strings.Contains(stdout, helptopics.Wrap(text, 80)) {
		t.Fatalf("exit %d: %q", code, stdout)
	}
	code, stdout, _ = f.run("--help")
	if code != 0 || !strings.Contains(stdout, "Additional help topics:") || !strings.Contains(stdout, "basics") {
		t.Fatalf("exit %d: %q", code, stdout)
	}
	code, stdout, _ = f.run("help", "item", "list")
	if code != 0 || !strings.Contains(stdout, "List items") {
		t.Fatalf("exit %d: %q", code, stdout)
	}
}

func TestFields(t *testing.T) {
	f := newFixture(t, Options{})
	code, stdout, stderr := f.run("show", "--output", "json", "--fields", "b,a")
	if code != 0 || stdout != `{"b":"2","a":"1"}`+"\n" {
		t.Fatalf("exit %d, stdout %q: %s", code, stdout, stderr)
	}
	for _, tc := range []struct {
		args []string
		msg  string
	}{
		{[]string{"show", "--fields", "c"}, `tool: usage: unknown field "c", available fields are a, b`},
		{[]string{"show", "--fields", "a", "--fields", "a"}, `tool: usage: field "a" is requested more than once`},
		{[]string{"show", "--fields="}, `tool: usage: unknown field "", available fields are a, b`},
	} {
		f := newFixture(t, Options{})
		code, stdout, stderr := f.run(tc.args...)
		if code != cli.ExitUsage || stdout != "" || !strings.HasPrefix(stderr, tc.msg+"\n") || strings.Contains(stderr, "Usage:") {
			t.Fatalf("%v: exit %d, stdout %q, stderr %q", tc.args, code, stdout, stderr)
		}
	}
}

func TestGateRefusals(t *testing.T) {
	cases := []struct {
		name    string
		request Request
		args    []string
		code    output.Code
	}{
		{"change without confirm", Request{Changes: true}, []string{"drop", "--yes"}, output.CodeConfirmationRequired},
		{"agent question without yes", Request{Question: "Drop it?"}, []string{"drop", "--agent", "--confirm"}, output.CodeConfirmationRequired},
		{"yes does not confirm a change", Request{Question: "Drop it?", Changes: true}, []string{"drop", "--agent", "--yes"}, output.CodeConfirmationRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, Options{})
			f.request = tc.request
			code, _, stderr := f.run(tc.args...)
			if code != cli.ExitUsage || f.changed || !strings.HasPrefix(stderr, "tool: "+string(tc.code)+": ") {
				t.Fatalf("exit %d, changed %v: %s", code, f.changed, stderr)
			}
			if strings.Contains(stderr, "Usage:") {
				t.Fatalf("refusal shows usage: %s", stderr)
			}
		})
	}
}

func TestGateAccepts(t *testing.T) {
	for _, tc := range []struct {
		request Request
		args    []string
	}{
		{Request{}, []string{"drop"}},
		{Request{Changes: true}, []string{"drop", "--confirm"}},
		{Request{Question: "Drop it?", Changes: true}, []string{"drop", "--agent", "--yes", "--confirm"}},
	} {
		f := newFixture(t, Options{})
		f.request = tc.request
		if code, _, stderr := f.run(tc.args...); code != 0 || !f.changed {
			t.Fatalf("%v: exit %d, changed %v: %s", tc.args, code, f.changed, stderr)
		}
	}
}

func TestGateWithoutFlagRefuses(t *testing.T) {
	root := &cobra.Command{Use: "tool"}
	program, err := New(root, Options{Version: "1"})
	if err != nil {
		t.Fatal(err)
	}
	changed := false
	cmd := &cobra.Command{Use: "drop"}
	gate := program.AddGate(cmd, Gates{})
	for _, req := range []Request{{Question: "Drop it?"}, {Changes: true}} {
		if err := gate.Run(req, func() error { changed = true; return nil }); err == nil || changed {
			t.Fatalf("%+v: err %v, changed %v", req, err, changed)
		}
	}
	if cmd.Flags().Lookup(yesFlag) != nil || cmd.Flags().Lookup(confirmFlag) != nil {
		t.Fatal("flags registered without gates")
	}
}

func TestNewRejects(t *testing.T) {
	withFlag := &cobra.Command{Use: "tool"}
	withFlag.Flags().Bool("agent", false, "")
	child := &cobra.Command{Use: "child"}
	(&cobra.Command{Use: "tool"}).AddCommand(child)
	cases := []struct {
		name string
		root *cobra.Command
		opts Options
	}{
		{"nil root", nil, Options{Version: "1"}},
		{"not a root", child, Options{Version: "1"}},
		{"invalid program name", &cobra.Command{Use: "Tool"}, Options{Version: "1"}},
		{"missing version", &cobra.Command{Use: "tool"}, Options{}},
		{"version with space", &cobra.Command{Use: "tool"}, Options{Version: "1 2"}},
		{"version with control", &cobra.Command{Use: "tool"}, Options{Version: "1\x1b[2J"}},
		{"reserved flag", withFlag, Options{Version: "1"}},
		{"invalid topic name", &cobra.Command{Use: "tool"}, Options{Version: "1", Topics: []helptopics.Topic{{Name: "Bad", Short: "x"}}}},
		{"topic without short", &cobra.Command{Use: "tool"}, Options{Version: "1", Topics: []helptopics.Topic{{Name: "bad"}}}},
		{"topic short with line break", &cobra.Command{Use: "tool"}, Options{Version: "1", Topics: []helptopics.Topic{{Name: "bad", Short: "a\nb"}}}},
	}
	for _, tc := range cases {
		if _, err := New(tc.root, tc.opts); err == nil {
			t.Errorf("%s: accepted", tc.name)
		}
	}
}

func TestExecuteRejectsBrokenTree(t *testing.T) {
	cases := []struct {
		name  string
		build func(*fixture)
	}{
		{"reserved local flag", func(f *fixture) {
			cmd := &cobra.Command{Use: "other", RunE: func(*cobra.Command, []string) error { f.ran = true; return nil }}
			cmd.Flags().Bool("yes", false, "")
			f.program.root.AddCommand(cmd)
		}},
		{"reserved persistent flag", func(f *fixture) {
			cmd := &cobra.Command{Use: "other", RunE: func(*cobra.Command, []string) error { f.ran = true; return nil }}
			cmd.PersistentFlags().String("output", "", "")
			f.program.root.AddCommand(cmd)
		}},
		{"topic collides with command", func(f *fixture) {
			f.program.root.AddCommand(&cobra.Command{Use: "basics", RunE: func(*cobra.Command, []string) error { f.ran = true; return nil }})
		}},
		{"alias collides with command", func(f *fixture) {
			f.program.root.AddCommand(&cobra.Command{Use: "other", Aliases: []string{"show"}})
		}},
		{"short help with line break", func(f *fixture) {
			f.program.root.AddCommand(&cobra.Command{Use: "other", Short: "one\ntwo"})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, Options{})
			tc.build(f)
			code, stdout, stderr := f.run("show")
			if code != cli.ExitRuntime || f.ran || stdout != "" || !strings.HasPrefix(stderr, "tool: runtime: command ") {
				t.Fatalf("exit %d, ran %v, stdout %q, stderr %q", code, f.ran, stdout, stderr)
			}
		})
	}
}

func TestInvalidDiagnosticIsStillReported(t *testing.T) {
	f := newFixture(t, Options{})
	f.err = &cli.Failure{Code: "Not Valid", Message: "broken", NextStep: "x", Usage: true}
	code, _, stderr := f.run("show")
	if code != cli.ExitUsage || !strings.HasPrefix(stderr, "tool: runtime: the diagnostic could not be written: ") {
		t.Fatalf("exit %d: %q", code, stderr)
	}
}

func TestOutputExplicit(t *testing.T) {
	f := newFixture(t, Options{})
	f.run("show")
	if f.program.OutputExplicit() {
		t.Fatal("explicit without --output")
	}
	f = newFixture(t, Options{})
	f.run("show", "--output", "table")
	if !f.program.OutputExplicit() {
		t.Fatal("not explicit with --output")
	}
}

func TestManpage(t *testing.T) {
	f := newFixture(t, Options{Topics: []helptopics.Topic{{Name: "basics", Short: "How the tool works", Text: "Basic text."}}})
	dir := t.TempDir()
	err := f.program.Manpage(dir, "", func(root *cobra.Command, dir string) error {
		if !root.DisableAutoGenTag {
			t.Error("generated pages would carry a date")
		}
		return os.WriteFile(filepath.Join(dir, root.Name()+".1"), []byte(".TH TOOL 1\n.SH SEE ALSO\n"), 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(dir, "tool.1"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), ".SS basics") || !strings.Contains(string(page), "Basic text.") {
		t.Fatalf("page %q", page)
	}
	if err := f.program.Manpage(dir, "", nil); err == nil {
		t.Fatal("nil generator accepted")
	}
}

// brokenTree breaks several conventions at once below a root that is still acceptable to New.
func brokenTree(f *fixture) {
	a := &cobra.Command{Use: "alpha", Short: "one\ntwo"}
	a.Flags().Bool("fields", false, "")
	b := &cobra.Command{Use: "beta", Aliases: []string{"show"}}
	b.PersistentFlags().String("output", "", "")
	f.program.root.AddCommand(a, b)
}

func TestProgramCheckReportsAllViolations(t *testing.T) {
	f := newFixture(t, Options{})
	if err := f.program.Check(); err != nil {
		t.Fatalf("clean tree: %v", err)
	}
	brokenTree(f)
	err := f.program.Check()
	if err == nil {
		t.Fatal("broken tree accepted")
	}
	want := []string{
		`command "tool": subcommand name "show" is used twice`,
		`command "tool alpha": the short help must be one line`,
		`command "tool alpha": flag --fields is reserved`,
		`command "tool beta": flag --output is reserved`,
	}
	got := err.Error()
	last := -1
	for _, w := range want {
		i := strings.Index(got, w)
		if i < 0 || i < last {
			t.Fatalf("missing or misordered %q in:\n%s", w, got)
		}
		last = i
	}
	// Flags registered through the Program are not reported.
	if strings.Contains(got, `"tool show"`) || strings.Contains(got, `"tool drop"`) || strings.Contains(got, `"tool": flag`) {
		t.Fatalf("owned flags reported:\n%s", got)
	}
}

func TestExecuteReportsWhatCheckReports(t *testing.T) {
	f := newFixture(t, Options{})
	brokenTree(f)
	check := f.program.Check().Error()
	code, stdout, stderr := f.run("show")
	if code != cli.ExitRuntime || f.ran || stdout != "" {
		t.Fatalf("exit %d, ran %v, stdout %q", code, f.ran, stdout)
	}
	for _, line := range strings.Split(check, "\n") {
		if !strings.Contains(stderr, line) {
			t.Errorf("Execute does not name %q:\n%s", line, stderr)
		}
	}

	clean := newFixture(t, Options{})
	if err := clean.program.Check(); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := clean.run("show"); code != cli.ExitSuccess || !clean.ran {
		t.Fatalf("clean tree: exit %d, ran %v", code, clean.ran)
	}
	// A run does not make its own help command a violation.
	if err := clean.program.Check(); err != nil {
		t.Fatalf("after Execute: %v", err)
	}
}

func TestManpageRefusesBrokenTree(t *testing.T) {
	f := newFixture(t, Options{})
	brokenTree(f)
	err := f.program.Manpage(t.TempDir(), "intro", func(*cobra.Command, string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "flag --fields is reserved") {
		t.Fatalf("Manpage: %v", err)
	}
}

func TestPackageCheckOnRawTree(t *testing.T) {
	root := &cobra.Command{Use: "tool"}
	root.PersistentFlags().String("output", "", "")
	root.PersistentFlags().Bool("agent", false, "")
	sub := &cobra.Command{Use: "sub", Short: "a\nb", Aliases: []string{"basics"}}
	sub.Flags().Bool("yes", false, "")
	sub.Flags().Bool("confirm", false, "")
	root.AddCommand(sub)
	opts := Options{Version: "1", Topics: []helptopics.Topic{{Name: "basics", Short: "x"}}}

	err := Check(root, opts)
	if err == nil {
		t.Fatal("broken tree accepted")
	}
	for _, w := range []string{
		`command "tool": flag --agent is reserved`,
		`command "tool": flag --output is reserved`,
		`command "tool sub": the short help must be one line`,
		`command "tool sub": flag --confirm is reserved`,
		`command "tool sub": flag --yes is reserved`,
		`command "tool": subcommand name "basics" is used twice`,
	} {
		if strings.Count(err.Error(), w) != 1 {
			t.Errorf("want %q exactly once in:\n%s", w, err)
		}
	}
	if len(root.Commands()) != 1 || root.Version != "" || sub.Args != nil || root.Flags().Lookup("version") != nil {
		t.Fatal("Check changed the tree")
	}
	if _, err := New(root, opts); err == nil {
		t.Fatal("New accepted the root")
	}
}

func TestPackageCheckRootViolationsAndCleanTree(t *testing.T) {
	err := Check(&cobra.Command{Use: "Tool"}, Options{Version: "1 2", Topics: []helptopics.Topic{{Name: "Bad"}}})
	if err == nil {
		t.Fatal("accepted")
	}
	for _, w := range []string{"invalid program name", "invalid version", "invalid help topic name", "needs a short help"} {
		if !strings.Contains(err.Error(), w) {
			t.Errorf("missing %q in %v", w, err)
		}
	}
	if Check(nil, Options{Version: "1"}) == nil {
		t.Error("nil root accepted")
	}
	clean := &cobra.Command{Use: "tool"}
	clean.AddCommand(&cobra.Command{Use: "sub", Short: "ok"})
	if err := Check(clean, Options{Version: "1"}); err != nil {
		t.Fatal(err)
	}
}

func TestChecksDoNotStopAtRootViolations(t *testing.T) {
	root := &cobra.Command{Use: "tool"}
	sub := &cobra.Command{Use: "sub"}
	sub.Flags().Bool("fields", false, "")
	root.AddCommand(sub)
	err := Check(root, Options{Version: "1 2"})
	if err == nil || !strings.Contains(err.Error(), "invalid version") || !strings.Contains(err.Error(), `command "tool sub": flag --fields is reserved`) {
		t.Fatalf("Check: %v", err)
	}

	withOutput := &cobra.Command{Use: "tool"}
	withOutput.PersistentFlags().String("output", "", "")
	_, err = New(withOutput, Options{Version: "1 2"})
	if err == nil || !strings.Contains(err.Error(), "invalid version") || !strings.Contains(err.Error(), `command "tool": flag --output is reserved`) {
		t.Fatalf("New: %v", err)
	}

	for _, err := range []error{Check(nil, Options{Version: "1 2"}), func() error { _, err := New(nil, Options{Version: "1 2"}); return err }()} {
		if err == nil || err.Error() != "a root command is required" {
			t.Errorf("nil root: %v", err)
		}
	}
}

func TestFieldsNames(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want []string
	}{
		{[]string{"show"}, nil},
		{[]string{"show", "--fields", "b,a"}, []string{"b", "a"}},
		{[]string{"show", "--fields="}, []string{""}},
	} {
		f := newFixture(t, Options{})
		var got []string
		cmd, _, _ := f.program.root.Find([]string{"show"})
		cmd.RunE = func(*cobra.Command, []string) error { got = f.fields.Names(); return nil }
		f.run(tc.args...)
		if !slices.Equal(got, tc.want) || (tc.want == nil) != (got == nil) {
			t.Fatalf("%v: %#v, want %#v", tc.args, got, tc.want)
		}
		if len(got) > 0 {
			got[0] = "changed"
			if f.fields.Names()[0] == "changed" {
				t.Fatal("Names exposes the internal slice")
			}
		}
	}
}

// runProjectJSON runs a command that projects list with available through the real flag path.
func runProjectJSON(t *testing.T, list string, available []string, args ...string) (int, string, string, error) {
	t.Helper()
	f := newFixture(t, Options{})
	cmd, _, _ := f.program.root.Find([]string{"show"})
	var projErr error
	cmd.RunE = func(c *cobra.Command, _ []string) error {
		out, err := f.fields.ProjectJSON(json.RawMessage(list), available)
		if err != nil {
			projErr = err
			return err
		}
		_, err = c.OutOrStdout().Write(append(out, '\n'))
		return err
	}
	code, stdout, stderr := f.run(append([]string{"show"}, args...)...)
	return code, stdout, stderr, projErr
}

func TestProjectJSON(t *testing.T) {
	avail := []string{"id", "name", "size"}
	list := `[ {"size": 1.50, "name":"a\u00e9", "id": 7, "extra": true}, {"id": 8} ]`
	want := `[{"size":1.50,"id":7},{"id":8}]` + "\n"
	for i := 0; i < 2; i++ {
		code, stdout, stderr, _ := runProjectJSON(t, list, avail, "--fields", "size,id")
		if code != 0 || stdout != want {
			t.Fatalf("exit %d, stdout %q: %s", code, stdout, stderr)
		}
	}
	// Without --fields the input comes back unchanged.
	code, stdout, _, _ := runProjectJSON(t, list, avail)
	if code != 0 || stdout != list+"\n" {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
	// An empty list is still checked against the declared fields.
	code, stdout, _, _ = runProjectJSON(t, `[]`, avail, "--fields", "name")
	if code != 0 || stdout != "[]\n" {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
}

func TestProjectJSONRefusals(t *testing.T) {
	avail := []string{"id", "name"}
	for _, tc := range []struct {
		name  string
		list  string
		avail []string
		args  []string
		msg   string
	}{
		{"unknown", `[{"id":1}]`, avail, []string{"--fields", "zzz"}, `unknown field "zzz", available fields are id, name`},
		{"unknown in empty list", `[]`, avail, []string{"--fields", "zzz"}, `unknown field "zzz", available fields are id, name`},
		{"repeated", `[{"id":1}]`, avail, []string{"--fields", "id,id"}, `field "id" is requested more than once`},
		{"explicitly empty", `[{"id":1}]`, avail, []string{"--fields="}, `unknown field ""`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, stdout, stderr, err := runProjectJSON(t, tc.list, tc.avail, tc.args...)
			var pe *output.ProjectionError
			if !errors.As(err, &pe) || code != cli.ExitUsage || stdout != "" || !strings.Contains(stderr, tc.msg) {
				t.Fatalf("exit %d, stdout %q, stderr %q, err %v", code, stdout, stderr, err)
			}
		})
	}
	for _, tc := range []struct {
		name  string
		list  string
		avail []string
	}{
		{"object", `{"id":1}`, avail},
		{"null", `null`, avail},
		{"scalar entry", `[1]`, avail},
		{"null entry", `[null]`, avail},
		{"invalid", `[{`, avail},
		{"trailing data", `[] x`, avail},
		{"nil available", `[]`, nil},
		{"empty available", `[]`, []string{}},
	} {
		for _, args := range [][]string{{"--fields", "id"}, nil} {
			t.Run(tc.name, func(t *testing.T) {
				code, stdout, _, err := runProjectJSON(t, tc.list, tc.avail, args...)
				var pe *output.ProjectionError
				if err == nil || errors.As(err, &pe) || code == 0 || stdout != "" {
					t.Fatalf("exit %d, stdout %q, err %v", code, stdout, err)
				}
			})
		}
	}
}

func TestZeroOptionsOutputIsUnchanged(t *testing.T) {
	f := newFixture(t, Options{})
	f.err = errors.New("backend failed")
	code, stdout, stderr := f.run("show")
	if code != cli.ExitRuntime || stdout != "" || stderr != "tool: runtime: backend failed\nCheck the cause and try again.\n" {
		t.Fatalf("failure: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
	f = newFixture(t, Options{})
	code, stdout, stderr = f.run("show", "--output", "json")
	if code != cli.ExitSuccess || stdout == "" || stderr != "" {
		t.Fatalf("success: exit %d, stdout %q, stderr %q", code, stdout, stderr)
	}
}

func TestAgentHelp(t *testing.T) {
	f := newFixture(t, Options{AgentHelp: "machine mode of this tool"})
	_, stdout, _ := f.run("--help")
	if !strings.Contains(stdout, "machine mode of this tool") || strings.Contains(stdout, cli.AgentFlagHelp) {
		t.Fatalf("help %q", stdout)
	}
	f = newFixture(t, Options{})
	if _, stdout, _ = f.run("--help"); !strings.Contains(stdout, cli.AgentFlagHelp) {
		t.Fatalf("default help %q", stdout)
	}
	for _, value := range []string{"a\nb", "a\rb", "a\x1b[2Jb", "a\tb", "a b"} {
		if _, err := New(&cobra.Command{Use: "tool"}, Options{Version: "1", AgentHelp: value}); err == nil {
			t.Errorf("agent help %q accepted", value)
		}
		if err := Check(&cobra.Command{Use: "tool"}, Options{Version: "1", AgentHelp: value}); err == nil {
			t.Errorf("Check accepted agent help %q", value)
		}
	}
}

func TestOptionTextsAreJoinedWithOtherViolations(t *testing.T) {
	_, err := New(&cobra.Command{Use: "Tool"}, Options{Version: "1", AgentHelp: "a\nb", Notice: "c\nd"})
	if err == nil {
		t.Fatal("accepted")
	}
	for _, want := range []string{"invalid program name", "agent help", "notice"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q missing from %q", want, err)
		}
	}
}

func TestNewExit(t *testing.T) {
	for _, code := range []int{0, 1, 2, 126, 127, 255, -1} {
		if exit, err := NewExit(code, errors.New("x")); err == nil || exit != nil {
			t.Errorf("code %d accepted", code)
		}
	}
	for _, code := range []int{3, 64, 125} {
		exit, err := NewExit(code, nil)
		if err != nil || exit.Code != code {
			t.Errorf("code %d: %v", code, err)
		}
	}
	cause := errors.New("cause")
	exit, _ := NewExit(3, cause)
	if !errors.Is(exit, cause) || exit.Error() != "cause" {
		t.Fatalf("unwrap: %v", exit)
	}
}

func TestExitCodes(t *testing.T) {
	cause := &cli.Failure{Code: output.CodeAuth, Message: "token expired", NextStep: "Log in."}
	exit3, _ := NewExit(3, cause)
	exit4, _ := NewExit(4, nil)
	wrapped := fmt.Errorf("context: %w", exit3)
	cases := []struct {
		name   string
		err    error
		code   int
		stderr string
	}{
		{"code with error", exit3, 3, "tool: auth: token expired\nLog in.\n"},
		{"wrapped", wrapped, 3, "tool: auth: token expired\nLog in.\n"},
		{"silent", exit4, 4, ""},
	}
	for _, tc := range cases {
		f := newFixture(t, Options{})
		f.err = tc.err
		code, stdout, stderr := f.run("show")
		if code != tc.code || stdout != "" || stderr != tc.stderr {
			t.Errorf("%s: exit %d, stdout %q, stderr %q", tc.name, code, stdout, stderr)
		}
	}
	for _, code := range []int{0, 1, 2, 126, -1} {
		f := newFixture(t, Options{})
		f.err = &Exit{Code: code, Err: errors.New("x")}
		got, stdout, stderr := f.run("show")
		want := fmt.Sprintf("tool: runtime: the program chose exit code %d, which is reserved or invalid; only 3 through 125 are allowed\n", code)
		if got != cli.ExitRuntime || stdout != "" || !strings.HasPrefix(stderr, want) {
			t.Errorf("literal %d: exit %d, stdout %q, stderr %q", code, got, stdout, stderr)
		}
		f = newFixture(t, Options{})
		f.err = &Exit{Code: code}
		if got, _, stderr = f.run("show"); got != cli.ExitRuntime || !strings.HasPrefix(stderr, "tool: runtime: ") {
			t.Errorf("silent literal %d: exit %d, stderr %q", code, got, stderr)
		}
	}
	f := newFixture(t, Options{})
	f.err = (*Exit)(nil)
	if code, _, stderr := f.run("show"); code != cli.ExitRuntime || !strings.HasPrefix(stderr, "tool: runtime: ") {
		t.Errorf("nil exit: exit %d, stderr %q", code, stderr)
	}
}

func TestSyntaxErrorKeepsItsCode(t *testing.T) {
	f := newFixture(t, Options{})
	if code, _, _ := f.run("show", "--nope"); code != cli.ExitUsage {
		t.Fatalf("exit %d", code)
	}
}

func TestDiagnosed(t *testing.T) {
	exit4, _ := NewExit(4, nil)
	boom := errors.New("boom")
	cases := []struct {
		name    string
		args    []string
		err     error
		broken  bool
		command string
		code    int
		check   func(error) bool
	}{
		{"runtime", []string{"show"}, boom, false, "tool show", 1, func(e error) bool { return e == boom }},
		{"syntax", []string{"show", "--nope"}, nil, false, "tool show", 2, func(e error) bool { return isSyntax(e) }},
		{"unknown command", []string{"nope"}, nil, false, "tool", 2, func(e error) bool { return isSyntax(e) }},
		{"silent exit", []string{"show"}, exit4, false, "tool show", 4, func(e error) bool { return e == error(exit4) }},
		{"invalid exit", []string{"show"}, &Exit{Code: 1}, false, "tool show", 1, func(e error) bool { return e != nil }},
		{"tree check", []string{"show"}, nil, true, "tool", 1, func(e error) bool { return e != nil }},
	}
	for _, tc := range cases {
		var got []Diagnosis
		plain := newFixture(t, Options{})
		f := newFixture(t, Options{Diagnosed: func(d Diagnosis) { got = append(got, d) }})
		for _, fx := range []*fixture{plain, f} {
			fx.err = tc.err
			if tc.broken {
				fx.program.root.AddCommand(&cobra.Command{Use: "other", Short: "a\nb"})
			}
		}
		pc, po, pe := plain.run(tc.args...)
		code, stdout, stderr := f.run(tc.args...)
		if code != pc || stdout != po || stderr != pe {
			t.Errorf("%s: output changed: %d %q %q vs %d %q %q", tc.name, code, stdout, stderr, pc, po, pe)
		}
		if len(got) != 1 {
			t.Errorf("%s: called %d times", tc.name, len(got))
			continue
		}
		if got[0].Command != tc.command || got[0].Code != tc.code || code != tc.code || !tc.check(got[0].Err) {
			t.Errorf("%s: %+v, exit %d", tc.name, got[0], code)
		}
	}
	calls := 0
	f := newFixture(t, Options{Diagnosed: func(Diagnosis) { calls++ }})
	for _, args := range [][]string{{"show"}, {"--help"}, {"--version"}, {"item", "list", "x"}} {
		if code, _, _ := f.run(args...); code != 0 {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
	if calls != 0 {
		t.Fatalf("called %d times on success", calls)
	}
}

func TestNotice(t *testing.T) {
	f := newFixture(t, Options{Notice: "audit is active"})
	show, _, _ := f.program.root.Find([]string{"show"})
	show.RunE = func(c *cobra.Command, _ []string) error {
		_, _ = c.ErrOrStderr().Write([]byte("command output\n"))
		return nil
	}
	for _, args := range [][]string{{"show"}, {"show", "--agent"}} {
		code, stdout, stderr := f.run(args...)
		if code != 0 || stdout != "" || stderr != "tool: audit is active\ncommand output\n" {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
	}
	for _, args := range [][]string{{"--help"}, {"--version"}} {
		code, stdout, stderr := f.run(args...)
		if code != 0 || stderr != "tool: audit is active\n" || strings.Contains(stdout, "audit is active") {
			t.Errorf("%v: exit %d, stdout %q, stderr %q", args, code, stdout, stderr)
		}
	}
	if _, _, stderr := f.run("show", "--nope"); strings.Count(stderr, "audit is active") != 1 || !strings.HasPrefix(stderr, "tool: audit is active\ntool: usage: ") {
		t.Errorf("syntax error: %q", stderr)
	}

	f = newFixture(t, Options{Notice: "audit is active"})
	f.program.root.AddCommand(&cobra.Command{Use: "other", Short: "a\nb"})
	if code, _, stderr := f.run("show"); code != cli.ExitRuntime || strings.Contains(stderr, "audit is active") {
		t.Errorf("tree check: exit %d, stderr %q", code, stderr)
	}

	for _, value := range []string{"a\nb", "a\rb", "a\x1b[2Jb", "a b"} {
		if _, err := New(&cobra.Command{Use: "tool"}, Options{Version: "1", Notice: value}); err == nil {
			t.Errorf("notice %q accepted", value)
		}
	}

	redactor := &redact.Redactor{}
	redactor.Add("s3cret-value")
	f = newFixture(t, Options{Notice: "token s3cret-value in use", Redactor: redactor})
	if _, _, stderr := f.run("show"); strings.Contains(stderr, "s3cret-value") || !strings.Contains(stderr, redact.Marker) {
		t.Errorf("notice not redacted: %q", stderr)
	}
}
