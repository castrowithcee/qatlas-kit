package uiprefs

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"

	"github.com/castrowithcee/qatlas-kit/configfile"
	"github.com/castrowithcee/qatlas-kit/i18n"
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

// The values of the navigation preference: the tab line, the path line, the path in the header and the choice
// field in the header of tui.NavForm.
const (
	NavigationTabs         = "tabs"
	NavigationPathLine     = "pathline"
	NavigationPathHeader   = "header"
	NavigationHeaderChoice = "choice"
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
	// Navigations are the navigation forms the application offers: NavigationTabs, NavigationPathLine,
	// NavigationPathHeader and NavigationHeaderChoice.
	// Empty means the application does not offer the setting; Prefs.Navigation is then empty and the
	// application chooses the form itself.
	Navigations []string
	// DefaultNavigation is the form that holds without a choice; it is one of Navigations, and empty where
	// there are none.
	DefaultNavigation string
	// Rewrite changes the keys of the file before any value is checked, so a value the application wrote
	// in an earlier form can load without a warning. It gets every key of the file, also keys this package
	// does not know and the keys section as a map[string]any, and may add, change or delete entries. A value
	// it leaves invalid still gets the usual warning. Nil leaves the file as it is.
	Rewrite func(raw map[string]any)
	// WriteKeys are extra keys with fixed values that every save writes, such as a format marker. A name
	// must not be one of the keys of this package. Loading ignores them.
	WriteKeys map[string]any
}

// reservedKeys are the keys of the file that this package owns.
var reservedKeys = []string{"theme", "start", "icons", "language", "mouse", "editor", "navigation", "keys"}

func (s Schema) check() error {
	var errs []error
	if !slices.Contains(s.Themes, tui.ThemeTerminal) {
		errs = append(errs, fmt.Errorf("the themes lack %q", tui.ThemeTerminal))
	}
	errs = append(errs, namesError("theme", s.Themes), namesError("area", s.Areas))
	if !slices.Contains(s.Areas, s.DefaultArea) {
		errs = append(errs, fmt.Errorf("the default area %q is none of the areas", s.DefaultArea))
	}
	errs = append(errs, namesError("navigation", s.Navigations))
	for _, n := range s.Navigations {
		if n != NavigationTabs && n != NavigationPathLine && n != NavigationPathHeader &&
			n != NavigationHeaderChoice {
			errs = append(errs, fmt.Errorf("the navigation %q is unknown", n))
		}
	}
	if len(s.Navigations) == 0 && s.DefaultNavigation != "" {
		errs = append(errs, fmt.Errorf("the default navigation %q is set but no navigation is offered", s.DefaultNavigation))
	}
	if len(s.Navigations) > 0 && !slices.Contains(s.Navigations, s.DefaultNavigation) {
		errs = append(errs, fmt.Errorf("the default navigation %q is none of the navigations", s.DefaultNavigation))
	}
	for name := range s.WriteKeys {
		if name == "" || slices.Contains(reservedKeys, name) {
			errs = append(errs, fmt.Errorf("the extra key %q is empty or a key of this package", name))
		}
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
	// Editor is the command line of the external editor, as exteditor.Resolve takes it; empty leaves
	// the choice to the environment.
	Editor string
	// Navigation is the navigation form, one of Schema.Navigations; empty where the schema offers none.
	Navigation string
}

// Default returns the preferences of a device that has none.
func Default(schema Schema) Prefs {
	return Prefs{Theme: tui.ThemeTerminal, Start: schema.DefaultArea, Icons: IconsUnicode,
		Language: LanguageEnglish, MouseEnabled: true, Navigation: schema.DefaultNavigation}
}

// Path returns the preferences file in the folder dir of the application.
func Path(dir string) string { return filepath.Join(dir, FileName) }

// fileForm is the encoding a save writes.
type fileForm struct {
	Theme    string `yaml:"theme"`
	Start    string `yaml:"start"`
	Icons    string `yaml:"icons"`
	Language string `yaml:"language"`
	Mouse    bool   `yaml:"mouse"`
	Editor   string `yaml:"editor,omitempty"`
	// Navigation is left out while the schema offers none.
	Navigation string            `yaml:"navigation,omitempty"`
	Keys       map[string]string `yaml:"keys,omitempty"`
	Extra      map[string]any    `yaml:",inline"`
}

// Warning is one warning of loading, as a text of tui.ModuleCatalog and the values its format verbs take. A
// warning about a value of the file also names the file in Path, which is not a format value: the full text
// is Path, ": " and the text of ID. A tool shows it in its language with Format, or with
// tui.Translator.Lookup(w.ID, w.Args...) and the Path prefix itself. Values that come from the file are
// masked by the %q verbs of the texts.
type Warning struct {
	ID   tui.TextID
	Args []any
	// Path is the preferences file; empty where the text names the file itself or no file is involved.
	Path string
}

// String returns the English text of the warning.
func (w Warning) String() string {
	return w.prefixed(fmt.Sprintf(tui.ModuleCatalog()[i18n.English][w.ID], w.Args...))
}

// Format returns the text of the warning in the language of tr.
func (w Warning) Format(tr tui.Translator) string { return w.prefixed(tr.Lookup(w.ID, w.Args...)) }

func (w Warning) prefixed(text string) string {
	if w.Path == "" {
		return text
	}
	return w.Path + ": " + text
}

// Load reads the preferences at path. It never fails: a missing file gives the defaults without a warning;
// an unreadable or malformed file gives the defaults and a value that is not valid gives the default for
// that part, each with a warning that names it. The warnings are English texts; LoadWarnings returns them
// as translatable values.
func Load(path string, schema Schema) (Prefs, []string) {
	prefs, warnings := LoadWarnings(path, schema)
	var texts []string
	for _, w := range warnings {
		texts = append(texts, w.String())
	}
	return prefs, texts
}

// LoadWarnings is Load with the warnings as values a tool can translate.
func LoadWarnings(path string, schema Schema) (Prefs, []Warning) {
	prefs := Default(schema)
	if err := schema.check(); err != nil {
		// check wraps the joined reasons once; the text of the warning carries the lead-in itself.
		return prefs, []Warning{{ID: tui.TextPrefsSchemaInvalid, Args: []any{errors.Unwrap(err).Error()}}}
	}
	store, err := configfile.NewStore(path, codec(schema))
	if err != nil {
		return prefs, []Warning{{ID: tui.TextPrefsUnreadable, Args: []any{path, err.Error()}}}
	}
	data, _, err := store.Snapshot()
	if errors.Is(err, fs.ErrNotExist) {
		return prefs, nil
	}
	if err != nil {
		return prefs, []Warning{{ID: tui.TextPrefsUnreadable, Args: []any{path, err.Error()}}}
	}
	prefs, warnings, err := parse(data, schema)
	if err != nil {
		return Default(schema), []Warning{{ID: tui.TextPrefsUnparsable, Args: []any{path, err.Error()}}}
	}
	for i := range warnings {
		warnings[i].Path = path
	}
	return prefs, warnings
}

// parse reads the values of data one by one. A value that is not valid keeps its default and adds a
// warning; keys this version does not know, also action IDs under keys, are ignored.
func parse(data []byte, schema Schema) (Prefs, []Warning, error) {
	prefs := Default(schema)
	data, err := rewrite(data, schema)
	if err != nil {
		return prefs, nil, err
	}
	var raw struct {
		Theme      *string           `yaml:"theme"`
		Start      *string           `yaml:"start"`
		Icons      *string           `yaml:"icons"`
		Language   *string           `yaml:"language"`
		Mouse      *string           `yaml:"mouse"`
		Editor     *string           `yaml:"editor"`
		Navigation *string           `yaml:"navigation"`
		Keys       map[string]string `yaml:"keys"`
	}
	if err = yaml.Unmarshal(data, &raw); err != nil {
		return prefs, nil, err
	}
	var warnings []Warning
	choose := func(name string, value *string, valid []string, target *string) {
		if value == nil {
			return
		}
		if !slices.Contains(valid, *value) {
			warnings = append(warnings, Warning{ID: tui.TextPrefsNotOneOf, Args: []any{name, *value, valid, *target}})
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
	if raw.Navigation != nil {
		if len(schema.Navigations) == 0 {
			warnings = append(warnings, Warning{ID: tui.TextPrefsNavigationNotOffered})
		} else {
			choose("navigation", raw.Navigation, schema.Navigations, &prefs.Navigation)
		}
	}
	if raw.Editor != nil {
		if checkEditor(*raw.Editor) != nil {
			warnings = append(warnings, Warning{ID: tui.TextPrefsEditorInvalid, Args: []any{*raw.Editor}})
		} else {
			prefs.Editor = *raw.Editor
		}
	}
	prefs.Keys, warnings = loadKeys(raw.Keys, schema.Keys, warnings)
	return prefs, warnings, nil
}

// rewrite applies Schema.Rewrite to the keys of data; without one the bytes pass through untouched.
func rewrite(data []byte, schema Schema) ([]byte, error) {
	if schema.Rewrite == nil {
		return data, nil
	}
	raw := map[string]any{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		raw = map[string]any{}
	}
	schema.Rewrite(raw)
	return yaml.Marshal(raw)
}

// loadKeys keeps the bindings the key table accepts. A binding that breaks a rule or collides keeps the
// default of its action and adds a warning; the result never collides.
func loadKeys(given map[string]string, keymap *tui.Keymap, warnings []Warning) (map[tui.ActionID]string, []Warning) {
	if len(given) == 0 {
		return nil, warnings
	}
	if keymap == nil {
		return nil, append(warnings, Warning{ID: tui.TextPrefsKeysNone})
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
		w := Warning{ID: tui.TextPrefsKeyInvalidNoExtra, Args: []any{string(err.Action), err.Error()}}
		for _, a := range actions {
			if a.ID == err.Action && a.Rebind != tui.RebindExtra {
				w = Warning{ID: tui.TextPrefsKeyInvalid, Args: []any{string(err.Action), err.Error(), keymap.Label(a.ID)}}
			}
		}
		warnings = append(warnings, w)
	}
	return usable, warnings
}

// Validate checks prefs against schema exactly as Save does, for preferences that come from flags or
// live only in memory. An invalid schema is the only error reported then; otherwise every invalid value
// is, bundled.
func Validate(prefs Prefs, schema Schema) error {
	if err := schema.check(); err != nil {
		return err
	}
	return validate(prefs, schema)
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
	if err := checkEditor(prefs.Editor); err != nil {
		errs = append(errs, err)
	}
	if len(schema.Navigations) > 0 {
		check("navigation", prefs.Navigation, schema.Navigations)
	} else if prefs.Navigation != "" {
		errs = append(errs, fmt.Errorf("navigation %q: this application does not offer the setting", prefs.Navigation))
	}
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

// checkEditor refuses an editor command line that is not valid UTF-8 or holds a control character.
func checkEditor(editor string) error {
	if !utf8.ValidString(editor) || strings.IndexFunc(editor, unicode.IsControl) >= 0 {
		return fmt.Errorf("editor %q holds a control character or invalid UTF-8", editor)
	}
	return nil
}

func codec(schema Schema) configfile.Codec[Prefs] {
	return configfile.Codec[Prefs]{
		Validate: func(p Prefs) error { return validate(p, schema) },
		Encode: func(p Prefs) ([]byte, error) {
			form := fileForm{Theme: p.Theme, Start: p.Start, Icons: p.Icons, Language: p.Language, Mouse: p.MouseEnabled,
				Editor: p.Editor, Navigation: p.Navigation, Extra: schema.WriteKeys}
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
				return Prefs{}, errors.New(warnings[0].String())
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
	// Checked before the store takes its lock, which leaves a lock file behind.
	if err := Validate(prefs, schema); err != nil {
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
