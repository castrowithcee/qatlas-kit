package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-kit/uiprefs"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Environ()); err != nil {
		fmt.Fprintln(os.Stderr, "tuidemo:", err)
		var usage usageError
		if errors.As(err, &usage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

type usageError struct{ error }

func run(args []string, out io.Writer, environ []string) error {
	flags := flag.NewFlagSet("tuidemo", flag.ContinueOnError)
	prefs := uiprefs.Default(uiprefs.Schema{DefaultArea: "overview"})
	flags.StringVar(&prefs.Theme, "theme", prefs.Theme, "start theme")
	flags.StringVar(&prefs.Language, "language", prefs.Language, "start language: english, deutsch or system")
	flags.StringVar(&prefs.Icons, "icons", prefs.Icons, "start icons: unicode, nerdfont or none")
	flags.BoolVar(&prefs.MouseEnabled, "mouse", prefs.MouseEnabled, "start with the mouse on")
	if err := flags.Parse(args); err != nil {
		return usageError{err}
	}
	if flags.NArg() != 0 {
		return usageError{errors.New("no arguments are accepted")}
	}
	m, err := newModel(prefs, tui.DetectColorProfile(out, environ), tui.DetectCharsetMode(environ))
	if err != nil {
		return err
	}
	filter := tui.NewInputFilter(nil)
	program := tea.NewProgram(m, tea.WithFilter(filter.Filter), tea.WithOutput(out), tea.WithEnvironment(environ))
	filter.Attach(program.Send)
	_, err = program.Run()
	return err
}
