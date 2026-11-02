package cobracli

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"

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
}

// Program is an application's command tree with the shared conventions applied. It serves one run.
type Program struct {
	root     *cobra.Command
	name     string
	topics   []helptopics.Topic
	redactor *redact.Redactor

	output string
	agent  bool
	format output.Format

	owned    map[*pflag.Flag]bool
	prepared map[*cobra.Command]bool
}

// New applies the conventions to root. Commands may be added to root afterwards; Execute checks the
// complete tree.
func New(root *cobra.Command, opts Options) (*Program, error) {
	if root == nil || root.HasParent() {
		return nil, errors.New("a root command is required")
	}
	name := root.Name()
	if !namePattern.MatchString(name) {
		return nil, fmt.Errorf("invalid program name %q", name)
	}
	if !validVersion(opts.Version) {
		return nil, fmt.Errorf("invalid version %q", opts.Version)
	}
	for _, flag := range []string{outputFlag, agentFlag} {
		if root.Flags().Lookup(flag) != nil || root.PersistentFlags().Lookup(flag) != nil {
			return nil, fmt.Errorf("flag --%s is reserved", flag)
		}
	}
	for _, topic := range opts.Topics {
		if !namePattern.MatchString(topic.Name) {
			return nil, fmt.Errorf("invalid help topic name %q", topic.Name)
		}
		if topic.Short == "" || strings.ContainsAny(topic.Short, "\r\n") {
			return nil, fmt.Errorf("help topic %q needs a short help of one line", topic.Name)
		}
	}

	p := &Program{
		root: root, name: name, topics: append([]helptopics.Topic(nil), opts.Topics...), redactor: opts.Redactor,
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
	flags.BoolVar(&p.agent, agentFlag, false, cli.AgentFlagHelp)
	p.own(flags, outputFlag, agentFlag)

	// A command without a run function is a help topic: 'help <topic>' prints its text, and the root help
	// lists it under the additional help topics.
	for _, topic := range p.topics {
		root.AddCommand(&cobra.Command{Use: topic.Name, Short: topic.Short, Long: helptopics.CLIText(topic)})
	}
	root.SetHelpCommand(p.helpCommand())
	return p, nil
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
// stderr; only help, version and the command's own payload reach stdout.
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
	if executed != nil && isSyntax(err) {
		opts.Usage = executed.UsageString()
	}
	if werr := cli.WriteError(stderr, p.name, err, opts); werr != nil {
		_ = cli.WriteError(stderr, p.name, &cli.Failure{
			Code: output.CodeRuntime, Message: "the diagnostic could not be written: " + werr.Error(),
			NextStep: brokenNextStep,
		}, opts)
	}
	return cli.ExitCode(err)
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

// prepare checks the conventions on c and its subcommands and installs the checks that run before any
// hook of a command. It may run again for commands added later.
func (p *Program) prepare(c *cobra.Command) error {
	if strings.ContainsAny(c.Short, "\r\n") {
		return fmt.Errorf("command %q: the short help must be one line", c.CommandPath())
	}
	var reserved error
	check := func(flag *pflag.Flag) {
		switch flag.Name {
		case outputFlag, agentFlag, fieldsFlag, yesFlag, confirmFlag:
			if !p.owned[flag] && reserved == nil {
				reserved = fmt.Errorf("command %q: flag --%s is reserved", c.CommandPath(), flag.Name)
			}
		}
	}
	c.Flags().VisitAll(check)
	c.PersistentFlags().VisitAll(check)
	if reserved != nil {
		return reserved
	}
	names := map[string]bool{}
	for _, sub := range c.Commands() {
		for _, name := range append([]string{sub.Name()}, sub.Aliases...) {
			if names[name] {
				return fmt.Errorf("command %q: subcommand name %q is used twice", c.CommandPath(), name)
			}
			names[name] = true
		}
	}
	if !p.prepared[c] {
		p.prepared[c] = true
		p.install(c)
	}
	for _, sub := range c.Commands() {
		if err := p.prepare(sub); err != nil {
			return err
		}
	}
	return nil
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
