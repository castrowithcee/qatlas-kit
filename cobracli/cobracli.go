package cobracli

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/helptopics"
	"github.com/castrowithcee/qatlas-kit/manpage"
	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-kit/redact"
)

const (
	outputFlag  = "output"
	agentFlag   = "agent"
	fieldsFlag  = "fields"
	yesFlag     = "yes"
	confirmFlag = "confirm"
)

// Help texts of the flags this package registers; the cli package holds those of --agent, --yes and --confirm.
const (
	OutputFlagHelp = "output format: table (default), json, compact (default with --agent), or toon"
	FieldsFlagHelp = "keep only these fields, in this order, comma separated or repeated"
)

const (
	syntaxNextStep = "Correct the command line; its usage follows."
	brokenNextStep = "Report this problem to the maintainers of the program."
)

var namePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Options are the application's choices for its command tree.
type Options struct {
	// Version is printed by --version as "<program> <version>".
	Version string
	// Topics become commands that print their text, listed as additional help topics.
	Topics []helptopics.Topic
	// Redactor removes known secrets from every diagnostic; nil leaves the text as it is.
	Redactor *redact.Redactor
	// OutputHelp replaces OutputFlagHelp, for example to name the formats of commands with another default.
	OutputHelp string
	// AgentHelp replaces cli.AgentFlagHelp as the help of --agent. It must be one line without control
	// characters.
	AgentHelp string
	// Notice is written to stderr as "<program>: <notice>" once, before a run executes the command, also
	// for --help and --version. It is not written when the command tree is refused. It must be one line
	// without control characters; the Redactor applies to it.
	Notice string
	// Diagnosed is called once after every failing Execute, once the diagnostic is written or deliberately
	// left out by a silent Exit, and never after a success. It must not panic: Execute does not recover.
	Diagnosed func(Diagnosis)
}

// Diagnosis describes a failed run to Options.Diagnosed, for example for the application's audit.
type Diagnosis struct {
	// Command is the path of the resolved command, or the name of the root when none was resolved.
	Command string
	// Err is the error as the command or Execute produced it, including an Exit wrapper. The Redactor is not
	// applied to it, so redact before writing it anywhere.
	Err error
	// Code is the exit code Execute returns.
	Code int
}

// The exit codes an application may choose with Exit; 0, 1 and 2 belong to the kit and the codes from 126
// on have meanings of their own in a shell.
const (
	minExitCode = 3
	maxExitCode = 125
)

// Exit is an error that chooses the process exit code of the run. Execute writes the diagnostic of Err and
// returns Code; with a nil Err it exits silently. A Code outside 3 through 125 is refused: Execute writes a
// runtime diagnostic and returns the runtime exit code instead.
type Exit struct {
	Code int
	Err  error
}

// NewExit checks code, which must be in 3 through 125.
func NewExit(code int, err error) (*Exit, error) {
	if !validExitCode(code) {
		return nil, fmt.Errorf("exit code %d is not allowed: use %d through %d", code, minExitCode, maxExitCode)
	}
	return &Exit{Code: code, Err: err}, nil
}

func (e *Exit) Error() string {
	if e == nil {
		return "invalid exit"
	}
	if e.Err == nil {
		return fmt.Sprintf("exit code %d", e.Code)
	}
	return e.Err.Error()
}

func (e *Exit) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func validExitCode(code int) bool { return code >= minExitCode && code <= maxExitCode }

// singleLine reports whether s is valid text of one line without control characters.
func singleLine(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return false
		}
	}
	return true
}

// Program is an application's command tree with the shared conventions applied. It serves one run.
type Program struct {
	root     *cobra.Command
	name     string
	topics   []helptopics.Topic
	redactor *redact.Redactor
	notice   string
	diagnose func(Diagnosis)

	output string
	agent  bool
	format output.Format

	help     *cobra.Command
	owned    map[*pflag.Flag]bool
	prepared map[*cobra.Command]bool
}

// New applies the conventions to root. Commands may be added to root afterwards; Execute checks the
// complete tree. A root that breaks several conventions is refused with all of them.
func New(root *cobra.Command, opts Options) (*Program, error) {
	if root == nil || root.HasParent() {
		return nil, errRootRequired
	}
	errs := append(checkRoot(root, opts), reservedFlagViolations(root, nil, map[*pflag.Flag]bool{}, outputFlag, agentFlag)...)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	name := root.Name()

	p := &Program{
		root: root, name: name, topics: append([]helptopics.Topic(nil), opts.Topics...), redactor: opts.Redactor,
		notice: opts.Notice, diagnose: opts.Diagnosed,
		owned: map[*pflag.Flag]bool{}, prepared: map[*cobra.Command]bool{},
	}
	root.Version = opts.Version
	root.SetVersionTemplate("{{.Name}} {{.Version}}\n")
	root.SilenceErrors = true
	root.SilenceUsage = true

	help := opts.OutputHelp
	if help == "" {
		help = OutputFlagHelp
	}
	flags := root.PersistentFlags()
	flags.StringVar(&p.output, outputFlag, "", help)
	agentHelp := opts.AgentHelp
	if agentHelp == "" {
		agentHelp = cli.AgentFlagHelp
	}
	flags.BoolVar(&p.agent, agentFlag, false, agentHelp)
	p.own(flags, outputFlag, agentFlag)

	// A command without a run function is a help topic: 'help <topic>' prints its text, and the root help
	// lists it under the additional help topics.
	for _, topic := range p.topics {
		root.AddCommand(&cobra.Command{Use: topic.Name, Short: topic.Short, Long: helptopics.CLIText(topic)})
	}
	p.help = p.helpCommand()
	root.SetHelpCommand(p.help)
	return p, nil
}

// Check reports every convention that the command tree of opts breaks, without a Program and without
// changing root: the violations New refuses, and those of the whole tree that Execute refuses. No flag
// counts as registered by this package, so a tree whose flags were registered by hand is reported in full.
// The violations are joined in the order of the tree.
func Check(root *cobra.Command, opts Options) error {
	if root == nil || root.HasParent() {
		return errRootRequired
	}
	errs := checkRoot(root, opts)
	names := map[string]bool{}
	for _, topic := range opts.Topics {
		names[topic.Name] = true
	}
	// The help command is added when Execute runs and the topics when New runs.
	if len(opts.Topics) > 0 || root.HasSubCommands() {
		names["help"] = true
	}
	return errors.Join(append(errs, checkTree(root, nil, nil, names, map[*pflag.Flag]bool{})...)...)
}

// Check reports every convention that the command tree of this program breaks, without running anything.
// Execute and Manpage apply the same check and refuse a tree it reports on.
func (p *Program) Check() error {
	extra := map[string]bool{}
	if p.root.HasSubCommands() {
		extra["help"] = true
	}
	return errors.Join(checkTree(p.root, p.owned, p.help, extra, map[*pflag.Flag]bool{})...)
}

var errRootRequired = errors.New("a root command is required")

// checkRoot reports what stops a root command from becoming a program; root is neither nil nor a child.
func checkRoot(root *cobra.Command, opts Options) []error {
	var errs []error
	if name := root.Name(); !namePattern.MatchString(name) {
		errs = append(errs, fmt.Errorf("invalid program name %q", name))
	}
	if !validVersion(opts.Version) {
		errs = append(errs, fmt.Errorf("invalid version %q", opts.Version))
	}
	for _, topic := range opts.Topics {
		if !namePattern.MatchString(topic.Name) {
			errs = append(errs, fmt.Errorf("invalid help topic name %q", topic.Name))
		}
		if topic.Short == "" || strings.ContainsAny(topic.Short, "\r\n") {
			errs = append(errs, fmt.Errorf("help topic %q needs a short help of one line", topic.Name))
		}
	}
	if !singleLine(opts.AgentHelp) {
		errs = append(errs, errors.New("the agent help must be one line without control characters"))
	}
	if !singleLine(opts.Notice) {
		errs = append(errs, errors.New("the notice must be one line without control characters"))
	}
	return errs
}

// validVersion keeps the version line to "<program> <version>".
func validVersion(version string) bool {
	if version == "" {
		return false
	}
	for _, r := range version {
		if unicode.IsSpace(r) || unicode.IsControl(r) || !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// Agent reports whether --agent is set.
func (p *Program) Agent() bool { return p.agent }

// Route is the reader that diagnostics and next steps address.
func (p *Program) Route() cli.Route {
	if p.agent {
		return cli.RouteAgent
	}
	return cli.RouteHuman
}

// Format is the output format of this run, resolved before any hook of the command runs.
func (p *Program) Format() output.Format { return p.format }

// OutputExplicit reports whether --output was given, for a command whose default format differs.
func (p *Program) OutputExplicit() bool {
	return p.root.PersistentFlags().Lookup(outputFlag).Changed
}

// Execute runs the command tree with args and returns the process exit code. A failure is written to
// stderr; only help, version and the command's own payload reach stdout. An Exit error chooses the exit
// code; Options.Notice is written before the command runs; Options.Diagnosed is called after a failure.
func (p *Program) Execute(args []string, stdout, stderr io.Writer) int {
	if args == nil {
		// Cobra reads the process arguments for nil.
		args = []string{}
	}
	p.root.SetArgs(args)
	p.root.SetOut(stdout)
	p.root.SetErr(stderr)
	p.root.InitDefaultHelpCmd()
	p.root.InitDefaultCompletionCmd(args...)
	if err := p.prepare(p.root); err != nil {
		return p.fail(stderr, nil, &cli.Failure{Code: output.CodeRuntime, Message: err.Error(), NextStep: brokenNextStep})
	}
	if p.notice != "" {
		notice := p.notice
		if p.redactor != nil {
			notice = p.redactor.Apply(notice)
		}
		// A notice that cannot be written must not stop the run; stderr is the only place to report it.
		_, _ = fmt.Fprintf(stderr, "%s: %s\n", p.name, notice)
	}
	executed, err := p.root.ExecuteC()
	if err == nil {
		return cli.ExitSuccess
	}
	return p.fail(stderr, executed, err)
}

func (p *Program) fail(stderr io.Writer, executed *cobra.Command, err error) int {
	opts := cli.ErrorOptions{Route: p.Route(), ExternalJSON: p.format == output.FormatJSON}
	if p.redactor != nil {
		opts.Redact = p.redactor.Apply
	}
	shown, silent, code := err, false, 0
	var exit *Exit
	if errors.As(err, &exit) {
		switch {
		case exit == nil || !validExitCode(exit.Code):
			message := "the program chose a missing exit"
			if exit != nil {
				message = fmt.Sprintf("the program chose exit code %d, which is reserved or invalid; only %d through %d are allowed",
					exit.Code, minExitCode, maxExitCode)
			}
			shown = &cli.Failure{Code: output.CodeRuntime, Message: message, NextStep: brokenNextStep, Cause: err}
		case exit.Err == nil:
			silent, code = true, exit.Code
		default:
			shown, code = exit.Err, exit.Code
		}
	}
	if code == 0 && !silent {
		code = cli.ExitCode(shown)
	}
	if !silent {
		if executed != nil && isSyntax(shown) {
			opts.Usage = executed.UsageString()
		}
		if werr := cli.WriteError(stderr, p.name, shown, opts); werr != nil {
			_ = cli.WriteError(stderr, p.name, &cli.Failure{
				Code: output.CodeRuntime, Message: "the diagnostic could not be written: " + werr.Error(),
				NextStep: brokenNextStep,
			}, opts)
		}
	}
	if p.diagnose != nil {
		command := p.name
		if executed != nil {
			command = executed.CommandPath()
		}
		p.diagnose(Diagnosis{Command: command, Err: err, Code: code})
	}
	return code
}

// Manpage writes the manpage of the command tree into dir and adds the help topics to the root page.
// generate writes the pages of root into dir, for example with the generator of Cobra's doc package.
func (p *Program) Manpage(dir, intro string, generate func(root *cobra.Command, dir string) error) error {
	if generate == nil {
		return errors.New("manpage tree generator is required")
	}
	if err := p.prepare(p.root); err != nil {
		return err
	}
	p.root.DisableAutoGenTag = true
	return manpage.Generate(dir, p.name, p.root.Version, intro, p.topics,
		func(dir, _ string) error { return generate(p.root, dir) })
}

// Syntax marks err as a syntax error of the command line: it exits with the usage code and is followed
// by the usage of the command.
func Syntax(err error) error {
	if err == nil {
		return nil
	}
	var failure *cli.Failure
	if errors.As(err, &failure) {
		if failure.Syntax {
			return err
		}
		marked := *failure
		marked.Usage, marked.Syntax = true, true
		return &marked
	}
	return &cli.Failure{
		Code: output.CodeUsage, Message: err.Error(), NextStep: syntaxNextStep, Usage: true, Syntax: true, Cause: err,
	}
}

func isSyntax(err error) bool {
	var failure *cli.Failure
	return errors.As(err, &failure) && failure.Syntax
}

func (p *Program) own(flags *pflag.FlagSet, names ...string) {
	for _, name := range names {
		p.owned[flags.Lookup(name)] = true
	}
}

// prepare refuses a tree that breaks a convention and installs the checks of the run on every command.
// It may run again for commands added later.
func (p *Program) prepare(c *cobra.Command) error {
	if err := p.Check(); err != nil {
		return err
	}
	p.installTree(c)
	return nil
}

func (p *Program) installTree(c *cobra.Command) {
	if !p.prepared[c] {
		p.prepared[c] = true
		p.install(c)
	}
	for _, sub := range c.Commands() {
		p.installTree(sub)
	}
}

// checkTree collects the violations of c and its subcommands depth first in command order. owned holds
// the flags the kit registered; skip is a command the kit added itself; reservedNames are subcommand
// names of c that the kit will add or has added elsewhere. seen keeps a flag shared by several flag sets
// from being reported twice.
func checkTree(c *cobra.Command, owned map[*pflag.Flag]bool, skip *cobra.Command, reservedNames map[string]bool, seen map[*pflag.Flag]bool) []error {
	var errs []error
	if strings.ContainsAny(c.Short, "\r\n") {
		errs = append(errs, fmt.Errorf("command %q: the short help must be one line", c.CommandPath()))
	}
	errs = append(errs, reservedFlagViolations(c, owned, seen, outputFlag, agentFlag, fieldsFlag, yesFlag, confirmFlag)...)
	names := map[string]bool{}
	for name := range reservedNames {
		names[name] = true
	}
	var subs []*cobra.Command
	for _, sub := range c.Commands() {
		if sub == skip {
			continue
		}
		subs = append(subs, sub)
		for _, name := range append([]string{sub.Name()}, sub.Aliases...) {
			if names[name] {
				errs = append(errs, fmt.Errorf("command %q: subcommand name %q is used twice", c.CommandPath(), name))
			}
			names[name] = true
		}
	}
	for _, sub := range subs {
		errs = append(errs, checkTree(sub, owned, skip, nil, seen)...)
	}
	return errs
}

// reservedFlagViolations reports the flags of c with a reserved name that the kit did not register, sorted
// by name.
func reservedFlagViolations(c *cobra.Command, owned map[*pflag.Flag]bool, seen map[*pflag.Flag]bool, reserved ...string) []error {
	var found []*pflag.Flag
	visit := func(flag *pflag.Flag) {
		if owned[flag] || seen[flag] || !slices.Contains(reserved, flag.Name) {
			return
		}
		seen[flag] = true
		found = append(found, flag)
	}
	c.Flags().VisitAll(visit)
	c.PersistentFlags().VisitAll(visit)
	slices.SortStableFunc(found, func(a, b *pflag.Flag) int { return strings.Compare(a.Name, b.Name) })
	var errs []error
	for _, flag := range found {
		errs = append(errs, fmt.Errorf("command %q: flag --%s is reserved", c.CommandPath(), flag.Name))
	}
	return errs
}

// install puts the run's checks into the argument check, because Cobra calls it for every runnable
// command before any hook, while a persistent hook of a subcommand would hide the root's.
func (p *Program) install(c *cobra.Command) {
	validate := c.Args
	if validate == nil {
		validate = noArgs
	}
	c.Args = func(cmd *cobra.Command, args []string) error {
		if err := p.resolve(); err != nil {
			return Syntax(err)
		}
		if err := validate(cmd, args); err != nil {
			return Syntax(err)
		}
		if err := cmd.ValidateRequiredFlags(); err != nil {
			return Syntax(err)
		}
		return Syntax(cmd.ValidateFlagGroups())
	}
	// Without a run function Cobra shows the help before it checks the arguments, so an unknown
	// subcommand would succeed.
	if !c.Runnable() && c.HasSubCommands() {
		c.RunE = func(cmd *cobra.Command, _ []string) error { return cmd.Help() }
	}
	inherited := c.FlagErrorFunc()
	c.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error { return Syntax(inherited(cmd, err)) })
}

func (p *Program) resolve() error {
	if p.OutputExplicit() && p.output == "" {
		_, err := output.ParseFormat("")
		return err
	}
	format, err := output.ResolveFormat(p.output, p.agent)
	if err != nil {
		return err
	}
	p.format = format
	return nil
}

func noArgs(c *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	if c.HasSubCommands() {
		return fmt.Errorf("unknown command %q for %q", args[0], c.CommandPath())
	}
	return fmt.Errorf("unexpected argument %q", args[0])
}

// helpCommand replaces Cobra's, which prints an unknown topic as text and succeeds.
func (p *Program) helpCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "help [command]",
		Short: "Help about any command or topic",
		Args:  cobra.ArbitraryArgs,
		RunE: func(_ *cobra.Command, args []string) error {
			target, rest, err := p.root.Find(args)
			if err != nil || target == nil || len(rest) > 0 {
				return Syntax(fmt.Errorf("unknown help topic %q", strings.Join(args, " ")))
			}
			target.InitDefaultHelpFlag()
			target.InitDefaultVersionFlag()
			return target.Help()
		},
	}
}
