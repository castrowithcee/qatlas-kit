package tui

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-kit/i18n"
)

// TextID is the stable, untranslated key of one catalog text.
type TextID string

// ActionID is the stable, untranslated name of one action of the key table.
type ActionID string

// modulePrefix marks the IDs of ModuleCatalog. Reserving it keeps a later module text from colliding with
// an application text that already exists.
const modulePrefix = "tui."

// KeyLabels resolves an action to the keys bound to it, spelt as a footer shows them. It reports false for
// an action it does not know.
type KeyLabels interface {
	KeyLabel(id ActionID) (string, bool)
}

// Translator returns the texts of one language.
type Translator interface {
	// Lookup returns the text of id with its key placeholders resolved and args formatted into it. A text
	// without args is returned unformatted. An ID without a text in the language or in English is returned
	// as it is.
	Lookup(id TextID, args ...any) string
}

type translator struct {
	lang  i18n.Lang
	texts i18n.Catalogs[TextID]
	keys  KeyLabels
}

// NewTranslator merges ModuleCatalog and the application catalog app for lang. keys resolves the key
// placeholders; it may be nil when no text holds one. The catalogs are copied, so a later change of app has
// no effect.
func NewTranslator(lang i18n.Lang, app i18n.Catalogs[TextID], keys KeyLabels) (Translator, error) {
	texts := ModuleCatalog()
	var errs []error
	for _, l := range sortedLangs(app) {
		for _, id := range sortedIDs(app[l]) {
			switch {
			case id == "":
				errs = append(errs, fmt.Errorf("%s catalog holds an empty text ID", l))
				continue
			case strings.HasPrefix(string(id), modulePrefix):
				errs = append(errs, fmt.Errorf("text %q uses the reserved prefix %q", id, modulePrefix))
				continue
			}
			if texts[l] == nil {
				texts[l] = map[TextID]string{}
			}
			texts[l][id] = app[l][id]
		}
	}
	errs = append(errs, structureErrors(texts)...)
	for _, l := range sortedLangs(texts) {
		for _, id := range sortedIDs(texts[l]) {
			_, unknown := resolvePlaceholders(texts[l][id], keys)
			for _, name := range unknown {
				errs = append(errs, fmt.Errorf("%s text %q names the unknown action placeholder {%s}", l, id, name))
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &translator{lang: lang, texts: texts, keys: keys}, nil
}

func (t *translator) Lookup(id TextID, args ...any) string {
	text := i18n.Lookup(t.texts, t.lang, id)
	if len(args) == 0 {
		text, _ = resolvePlaceholders(text, t.keys)
		return text
	}
	// A key label is literal text, so a "%" in it must not become a verb of the format.
	text, _ = resolvePlaceholders(text, percentEscaped{t.keys})
	return fmt.Sprintf(text, args...)
}

type percentEscaped struct{ keys KeyLabels }

func (p percentEscaped) KeyLabel(id ActionID) (string, bool) {
	if p.keys == nil {
		return "", false
	}
	label, ok := p.keys.KeyLabel(id)
	return strings.ReplaceAll(label, "%", "%%"), ok
}

// CheckCatalogs reports every text that keeps catalogs from being complete: an ID that English and German
// or any other language present do not both hold, an empty text, and format verbs that differ from the
// English text. Applications run it in their tests; at run time a missing translation falls back to English.
func CheckCatalogs(catalogs i18n.Catalogs[TextID]) error {
	var errs []error
	langs := sortedLangs(catalogs)
	if !slices.Contains(langs, i18n.German) {
		langs = append(langs, i18n.German)
	}
	for _, l := range langs {
		if l == i18n.English {
			continue
		}
		for _, id := range sortedIDs(catalogs[i18n.English]) {
			if _, ok := catalogs[l][id]; !ok {
				errs = append(errs, fmt.Errorf("text %q has no %s text", id, l))
			}
		}
	}
	for _, l := range sortedLangs(catalogs) {
		for _, id := range sortedIDs(catalogs[l]) {
			if strings.TrimSpace(catalogs[l][id]) == "" {
				errs = append(errs, fmt.Errorf("%s text %q is empty", l, id))
			}
		}
	}
	errs = append(errs, structureErrors(catalogs)...)
	return errors.Join(errs...)
}

// structureErrors reports the texts that would draw wrong text at run time: a translation without an
// English text, and a translation whose format verbs differ from the English ones.
func structureErrors(catalogs i18n.Catalogs[TextID]) []error {
	var errs []error
	english := catalogs[i18n.English]
	for _, l := range sortedLangs(catalogs) {
		if l == i18n.English {
			continue
		}
		for _, id := range sortedIDs(catalogs[l]) {
			base, ok := english[id]
			if !ok {
				errs = append(errs, fmt.Errorf("%s text %q has no English text", l, id))
				continue
			}
			if want, got := formatVerbs(base), formatVerbs(catalogs[l][id]); !slices.Equal(want, got) {
				errs = append(errs, fmt.Errorf("%s text %q has the format verbs %v, English has %v", l, id, got, want))
			}
		}
	}
	return errs
}

// formatVerbs lists the fmt verbs of text in order, with their flags, width, precision and argument index.
func formatVerbs(text string) []string {
	var verbs []string
	for i := 0; i < len(text); i++ {
		if text[i] != '%' {
			continue
		}
		j := i + 1
		for j < len(text) && strings.IndexByte("+-# 0123456789.*[]", text[j]) >= 0 {
			j++
		}
		if j < len(text) {
			j++
		}
		verbs = append(verbs, text[i:j])
		i = j - 1
	}
	return verbs
}

// resolvePlaceholders replaces every placeholder of text whose action keys knows and returns the names of
// the placeholders it could not resolve.
func resolvePlaceholders(text string, keys KeyLabels) (string, []string) {
	if !strings.Contains(text, "{") {
		return text, nil
	}
	var out strings.Builder
	var unknown []string
	for {
		open := strings.IndexByte(text, '{')
		if open < 0 {
			break
		}
		end := strings.IndexByte(text[open+1:], '}')
		if end < 0 {
			break
		}
		name := text[open+1 : open+1+end]
		if !isPlaceholderName(name) {
			out.WriteString(text[:open+1])
			text = text[open+1:]
			continue
		}
		out.WriteString(text[:open])
		text = text[open+end+2:]
		if keys != nil {
			if label, ok := keys.KeyLabel(ActionID(name)); ok {
				out.WriteString(label)
				continue
			}
		}
		unknown = append(unknown, name)
		out.WriteString("{" + name + "}")
	}
	out.WriteString(text)
	return out.String(), unknown
}

func isPlaceholderName(name string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for i := 1; i < len(name); i++ {
		c := name[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

func sortedLangs(catalogs i18n.Catalogs[TextID]) []i18n.Lang {
	langs := make([]i18n.Lang, 0, len(catalogs))
	for l := range catalogs {
		langs = append(langs, l)
	}
	slices.Sort(langs)
	return langs
}

func sortedIDs(texts map[TextID]string) []TextID {
	ids := make([]TextID, 0, len(texts))
	for id := range texts {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}
