package uiprefs

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"

	"go.yaml.in/yaml/v3"

	"github.com/castrowithcee/qatlas-kit/configfile"
	"github.com/castrowithcee/qatlas-kit/tui"
)

// FileName is the name of the preferences file inside the folder of the application.
const FileName = "tui.yaml"

// The values of the icons preference.
const (
	IconsUnicode  = "unicode"
	IconsNerdFont = "nerdfont"
	IconsNone     = "none"
)

// saveAttempts bounds how often Save starts over when another writer replaced the file meanwhile.
const saveAttempts = 3

// Schema holds the names an application accepts.
type Schema struct {
	// Themes are the theme names, such as tui.ThemeSet.Names; tui.ThemeTerminal is one of them and the
	// default.
	Themes []string
	// Areas are the IDs of the areas a TUI can start on; DefaultArea is one of them.
	Areas       []string
	DefaultArea string
	// Keys is the key table whose rebindable actions the keys section binds; nil allows no keys section.
	Keys *tui.Keymap
}

func (s Schema) check() error {
	var errs []error
	if !slices.Contains(s.Themes, tui.ThemeTerminal) {
		errs = append(errs, fmt.Errorf("the themes lack %q", tui.ThemeTerminal))
	}
	errs = append(errs, namesError("theme", s.Themes), namesError("area", s.Areas))
	if !slices.Contains(s.Areas, s.DefaultArea) {
		errs = append(errs, fmt.Errorf("the default area %q is none of the areas", s.DefaultArea))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("invalid preferences schema: %w", err)
	}
	return nil
}

func namesError(kind string, names []string) error {
	for i, name := range names {
		if name == "" {
			return fmt.Errorf("an empty %s name", kind)
		}
		if slices.Contains(names[:i], name) {
			return fmt.Errorf("the %s %q is listed twice", kind, name)
		}
	}
	return nil
}

// Prefs are the preferences of one device.
type Prefs struct {
	Theme, Start, Icons, Language string
	MouseEnabled                  bool
	// Keys holds the keys that differ from the default, as tui.Keymap.Bind takes them; nil holds none.
	Keys map[tui.ActionID]string
}

// Default returns the preferences of a device that has none.
func Default(schema Schema) Prefs {
	return Prefs{Theme: tui.ThemeTerminal, Start: schema.DefaultArea, Icons: IconsUnicode,
		Language: LanguageEnglish, MouseEnabled: true}
}

// Path returns the preferences file in the folder dir of the application.
func Path(dir string) string { return filepath.Join(dir, FileName) }

// fileForm is the encoding a save writes.
type fileForm struct {
	Theme    string            `yaml:"theme"`
	Start    string            `yaml:"start"`
	Icons    string            `yaml:"icons"`
	Language string            `yaml:"language"`
	Mouse    bool              `yaml:"mouse"`
	Keys     map[string]string `yaml:"keys,omitempty"`
}

// Load reads the preferences at path. It never fails: a missing file gives the defaults without a warning;
// an unreadable or malformed file gives the defaults and a value that is not valid gives the default for
// that part, each with a warning that names it.
func Load(path string, schema Schema) (Prefs, []string) {
	prefs := Default(schema)
	if err := schema.check(); err != nil {
		return prefs, []string{err.Error()}
	}
	store, err := configfile.NewStore(path, codec(schema))
	if err != nil {
		return prefs, []string{fmt.Sprintf("cannot read %s, using the defaults: %v", path, err)}
	}
	data, _, err := store.Snapshot()
	if errors.Is(err, fs.ErrNotExist) {
		return prefs, nil
	}
	if err != nil {
		return prefs, []string{fmt.Sprintf("cannot read %s, using the defaults: %v", path, err)}
	}
	prefs, warnings, err := parse(data, schema)
	if err != nil {
		return Default(schema), []string{fmt.Sprintf("cannot parse %s, using the defaults: %v", path, err)}
	}
	for i, w := range warnings {
		warnings[i] = path + ": " + w
	}
	return prefs, warnings
}

// parse reads the values of data one by one. A value that is not valid keeps its default and adds a
// warning; keys this version does not know, also action IDs under keys, are ignored.
func parse(data []byte, schema Schema) (Prefs, []string, error) {
	prefs := Default(schema)
	var raw struct {
		Theme    *string           `yaml:"theme"`
		Start    *string           `yaml:"start"`
		Icons    *string           `yaml:"icons"`
		Language *string           `yaml:"language"`
		Mouse    *string           `yaml:"mouse"`
		Keys     map[string]string `yaml:"keys"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return prefs, nil, err
	}
	var warnings []string
	choose := func(name string, value *string, valid []string, target *string) {
		if value == nil {
			return
		}
		if !slices.Contains(valid, *value) {
			warnings = append(warnings, fmt.Sprintf("%s %q is not one of %q, using %s", name, *value, valid, *target))
			return
		}
		*target = *value
	}
	choose("theme", raw.Theme, schema.Themes, &prefs.Theme)
	choose("start", raw.Start, schema.Areas, &prefs.Start)
	choose("icons", raw.Icons, []string{IconsUnicode, IconsNerdFont, IconsNone}, &prefs.Icons)
	choose("language", raw.Language, []string{LanguageEnglish, LanguageGerman, LanguageSystem}, &prefs.Language)
	if raw.Mouse != nil {
		mouse := "true"
		choose("mouse", raw.Mouse, []string{"true", "false"}, &mouse)
		prefs.MouseEnabled = mouse == "true"
	}
	prefs.Keys, warnings = loadKeys(raw.Keys, schema.Keys, warnings)
	return prefs, warnings, nil
}

// loadKeys keeps the bindings the key table accepts. A binding that breaks a rule or collides keeps the
// default of its action and adds a warning; the result never collides.
func loadKeys(given map[string]string, keymap *tui.Keymap, warnings []string) (map[tui.ActionID]string, []string) {
	if len(given) == 0 {
		return nil, warnings
	}
	if keymap == nil {
		return nil, append(warnings, "keys: this application has no keys to rebind, using the defaults")
	}
	actions := keymap.Actions()
	overrides := map[tui.ActionID]string{}
	for _, a := range actions {
		if v, ok := given[string(a.ID)]; ok {
			overrides[a.ID] = v
		}
	}
	usable, errs := keymap.Resolve(overrides)
	for _, err := range errs {
		standard := "no extra key"
		for _, a := range actions {
			if a.ID == err.Action && a.Rebind != tui.RebindExtra {
				standard = keymap.Label(a.ID)
			}
		}
		warnings = append(warnings, fmt.Sprintf("keys.%s %v, using %s", err.Action, err, standard))
	}
	return usable, warnings
}

func validate(prefs Prefs, schema Schema) error {
	var errs []error
	check := func(name, value string, valid []string) {
		if !slices.Contains(valid, value) {
			errs = append(errs, fmt.Errorf("%s %q is not one of %q", name, value, valid))
		}
	}
	check("theme", prefs.Theme, schema.Themes)
	check("start", prefs.Start, schema.Areas)
	check("icons", prefs.Icons, []string{IconsUnicode, IconsNerdFont, IconsNone})
	check("language", prefs.Language, []string{LanguageEnglish, LanguageGerman, LanguageSystem})
	if len(prefs.Keys) > 0 {
		if schema.Keys == nil {
			errs = append(errs, errors.New("keys: this application has no keys to rebind"))
		} else {
			_, keyErrs := schema.Keys.Resolve(prefs.Keys)
			for _, err := range keyErrs {
				errs = append(errs, fmt.Errorf("keys.%s %w", err.Action, err))
			}
		}
	}
	return errors.Join(errs...)
}

func codec(schema Schema) configfile.Codec[Prefs] {
	return configfile.Codec[Prefs]{
		Validate: func(p Prefs) error { return validate(p, schema) },
		Encode: func(p Prefs) ([]byte, error) {
			form := fileForm{Theme: p.Theme, Start: p.Start, Icons: p.Icons, Language: p.Language, Mouse: p.MouseEnabled}
			if schema.Keys != nil {
				usable, _ := schema.Keys.Resolve(p.Keys)
				for id, key := range usable {
					if form.Keys == nil {
						form.Keys = map[string]string{}
					}
					form.Keys[string(id)] = key
				}
			}
			var buf bytes.Buffer
			enc := yaml.NewEncoder(&buf)
			enc.SetIndent(2)
			if err := enc.Encode(form); err != nil {
				return nil, err
			}
			if err := enc.Close(); err != nil {
				return nil, err
			}
			return buf.Bytes(), nil
		},
		Decode: func(data []byte) (Prefs, error) {
			p, warnings, err := parse(data, schema)
			if err != nil {
				return Prefs{}, err
			}
			if len(warnings) > 0 {
				return Prefs{}, fmt.Errorf("%s", warnings[0])
			}
			return p, nil
		},
	}
}

// Save writes prefs to path through the configfile package: atomically, under a lock, with a backup of
// the file it replaces, and private to the user for a new file. A value that is not valid is refused and
// nothing is written. When another writer replaced the file meanwhile, Save starts over, so the last
// writer wins.
func Save(path string, prefs Prefs, schema Schema) error {
	if err := schema.check(); err != nil {
		return err
	}
	// Checked before the store takes its lock, which leaves a lock file behind.
	if err := validate(prefs, schema); err != nil {
		return err
	}
	store, err := configfile.NewStore(path, codec(schema))
	if err != nil {
		return err
	}
	for attempt := 1; ; attempt++ {
		_, rev, err := store.Snapshot()
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		_, err = store.SaveIfUnchanged(prefs, rev)
		if !errors.Is(err, configfile.ErrConflict) || attempt == saveAttempts {
			return err
		}
	}
}
