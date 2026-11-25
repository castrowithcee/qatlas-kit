package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"slices"

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
	flags.StringVar(&prefs.Language, "language", prefs.Language, "start language: english or deutsch")
	flags.StringVar(&prefs.Icons, "icons", prefs.Icons, "start icons: unicode, nerdfont or none")
	flags.BoolVar(&prefs.MouseEnabled, "mouse", prefs.MouseEnabled, "start with the mouse on")
	if err := flags.Parse(args); err != nil {
		return usageError{err}
	}
	if flags.NArg() != 0 {
		return usageError{errors.New("no arguments are accepted")}
	}
	if !slices.Contains([]string{uiprefs.LanguageEnglish, uiprefs.LanguageGerman}, prefs.Language) {
		return usageError{fmt.Errorf("-language %q is neither english nor deutsch", prefs.Language)}
	}
	if !slices.Contains([]string{uiprefs.IconsUnicode, uiprefs.IconsNerdFont, uiprefs.IconsNone}, prefs.Icons) {
		return usageError{fmt.Errorf("-icons %q is none of unicode, nerdfont and none", prefs.Icons)}
	}
	m, err := newModel(prefs, tui.DetectColorProfile(out, environ), tui.DetectCharsetMode(environ))
	if err != nil {
		return err
	}
	if !slices.Contains(m.themes.Names(), prefs.Theme) {
		return usageError{fmt.Errorf("-theme %q is none of %v", prefs.Theme, m.themes.Names())}
	}
	filter := tui.NewInputFilter(nil)
	program := tea.NewProgram(m, tea.WithFilter(filter.Filter), tea.WithOutput(out), tea.WithEnvironment(environ))
	filter.Attach(program.Send)
	_, err = program.Run()
	return err
}
