package cobracli

import (
	"errors"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/castrowithcee/qatlas-kit/cli"
	"github.com/castrowithcee/qatlas-kit/output"
)

// Fields is the --fields selection of one command.
type Fields struct {
	flag  *pflag.Flag
	names []string
}

// AddFields registers --fields on c.
func (p *Program) AddFields(c *cobra.Command) *Fields {
	f := &Fields{}
	c.Flags().StringSliceVar(&f.names, fieldsFlag, nil, FieldsFlagHelp)
	p.own(c.Flags(), fieldsFlag)
	f.flag = c.Flags().Lookup(fieldsFlag)
	return f
}

// Project restricts result to the selected fields in their order; without --fields it keeps everything.
// An unknown or repeated field is a usage problem that names the available fields.
func (f *Fields) Project(result output.Result) (output.Result, error) {
	names := f.names
	if f.flag.Changed && len(names) == 0 {
		// An empty selection given on purpose must not silently select everything.
		names = []string{""}
	}
	return output.Project(result, names)
}

// Gates are the confirmation flags a command offers.
type Gates struct {
	// Question registers --yes, which answers the command's ordinary question in advance.
	Question bool
	// Change registers --confirm, which every request that changes data needs.
	Change bool
}

// Gate holds the confirmation flags of one command.
type Gate struct {
	program *Program
	gates   Gates
	yes     bool
	confirm bool
}

// AddGate registers the flags of gates on c.
func (p *Program) AddGate(c *cobra.Command, gates Gates) *Gate {
	g := &Gate{program: p, gates: gates}
	if gates.Question {
		c.Flags().BoolVar(&g.yes, yesFlag, false, cli.YesFlagHelp)
		p.own(c.Flags(), yesFlag)
	}
	if gates.Change {
		c.Flags().BoolVar(&g.confirm, confirmFlag, false, cli.ConfirmFlagHelp)
		p.own(c.Flags(), confirmFlag)
	}
	return g
}

// Request describes what one run of a command needs before it takes effect.
type Request struct {
	// Question is asked at a terminal unless --yes answers it; empty asks nothing.
	Question string
	// Changes marks a request that changes data and therefore needs --confirm.
	Changes bool
}

// Run checks the confirmation of req before it invokes action. A request needs the flag its gate offers;
// a question or change the command has no flag for is refused, since nobody could confirm it.
func (g *Gate) Run(req Request, action func() error) error {
	if req.Question != "" && !g.gates.Question {
		return errors.New("the request asks a question, but its command has no --yes flag")
	}
	if req.Changes && !g.gates.Change {
		return errors.New("the request changes data, but its command has no --confirm flag")
	}
	return cli.ConfirmAction(cli.Confirmation{
		Agent: g.program.agent, Yes: g.yes, Confirm: g.confirm,
		RequiresConfirm: req.Changes, Question: req.Question,
	}, action)
}
