package cobracli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

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

// Names returns the names selected by --fields, or nil without the flag. The result is a copy. An empty
// selection given on purpose is the single empty name, which no field has, so it never reads as
// "everything" and every check refuses it.
func (f *Fields) Names() []string {
	if f.flag.Changed && len(f.names) == 0 {
		return []string{""}
	}
	if len(f.names) == 0 {
		return nil
	}
	return slices.Clone(f.names)
}

// Project restricts result to the selected fields in their order; without --fields it keeps everything.
// An unknown or repeated field is a usage problem that names the available fields.
func (f *Fields) Project(result output.Result) (output.Result, error) {
	return output.Project(result, f.Names())
}

// ProjectJSON restricts every entry of list, a JSON array of objects, to the selected members in their
// order. available lists the fields the command offers; it decides which names are valid because an empty
// list or sparse entries cannot show them. An unknown or repeated name is a usage problem that names the
// available fields. Without --fields the list is returned unchanged. A selected member that an entry lacks
// is left out of that entry. Values are copied as raw JSON and the result is compact, so the same input
// gives the same bytes. Anything but an array of objects, and an empty available, is refused.
func (f *Fields) ProjectJSON(list json.RawMessage, available []string) (json.RawMessage, error) {
	if len(available) == 0 {
		return nil, errors.New("ProjectJSON needs the available field names")
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(list, &entries); err != nil {
		return nil, fmt.Errorf("the list is not a JSON array of objects: %w", err)
	}
	if entries == nil {
		return nil, errors.New("the list is not a JSON array of objects")
	}
	for i, e := range entries {
		if e == nil {
			return nil, fmt.Errorf("entry %d of the list is not a JSON object", i)
		}
	}
	names := f.Names()
	if names == nil {
		return list, nil
	}
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		if seen[n] {
			return nil, &output.ProjectionError{Field: n, Duplicate: true}
		}
		seen[n] = true
		if !slices.Contains(available, n) {
			return nil, &output.ProjectionError{Field: n, Available: slices.Clone(available)}
		}
	}

	var out bytes.Buffer
	out.WriteByte('[')
	for i, e := range entries {
		if i > 0 {
			out.WriteByte(',')
		}
		out.WriteByte('{')
		first := true
		for _, n := range names {
			v, ok := e[n]
			if !ok {
				continue
			}
			if !first {
				out.WriteByte(',')
			}
			first = false
			key, _ := json.Marshal(n)
			out.Write(key)
			out.WriteByte(':')
			if err := json.Compact(&out, v); err != nil {
				return nil, err
			}
		}
		out.WriteByte('}')
	}
	out.WriteByte(']')
	return out.Bytes(), nil
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
