package tui

import "github.com/castrowithcee/qatlas-kit/i18n"

// The IDs of ModuleCatalog.
const (
	TextKeyUp        TextID = "tui.key.up"
	TextKeyDown      TextID = "tui.key.down"
	TextKeyLeft      TextID = "tui.key.left"
	TextKeyRight     TextID = "tui.key.right"
	TextKeyFirst     TextID = "tui.key.first"
	TextKeyLast      TextID = "tui.key.last"
	TextKeyPageUp    TextID = "tui.key.pageup"
	TextKeyPageDown  TextID = "tui.key.pagedown"
	TextKeyNext      TextID = "tui.key.next"
	TextKeyPrevious  TextID = "tui.key.previous"
	TextKeyNextTopic TextID = "tui.key.nexttopic"
	TextKeyPrevTopic TextID = "tui.key.prevtopic"
	TextKeyEnter     TextID = "tui.key.enter"
	TextKeyToggle    TextID = "tui.key.toggle"
	TextKeySearch    TextID = "tui.key.search"
	TextKeyFilter    TextID = "tui.key.filter"
	TextKeyBack      TextID = "tui.key.back"
	TextKeyQuit      TextID = "tui.key.quit"
	TextKeyHelp      TextID = "tui.key.help"
	TextKeyInfo      TextID = "tui.key.info"
	TextKeyCommands  TextID = "tui.key.commands"
	TextKeySave      TextID = "tui.key.save"
	TextKeyYes       TextID = "tui.key.yes"
	TextKeyNo        TextID = "tui.key.no"
	TextKeyNew       TextID = "tui.key.new"
	TextKeyEdit      TextID = "tui.key.edit"
	TextKeyRename    TextID = "tui.key.rename"
	TextKeyCopy      TextID = "tui.key.copy"
	TextKeyDelete    TextID = "tui.key.delete"
	TextKeyDiscard   TextID = "tui.key.discard"
	TextKeyReload    TextID = "tui.key.reload"
	TextKeyChoose    TextID = "tui.key.choose"

	TextCancelled      TextID = "tui.cancelled"
	TextCopyHandedOver TextID = "tui.copyhandedover"
	TextWarning        TextID = "tui.warning"
	TextError          TextID = "tui.error"
	TextResizeTitle    TextID = "tui.resize.title"
	TextResizeNeed     TextID = "tui.resize.need"
	TextResizeHave     TextID = "tui.resize.have"
	TextScrollPosition TextID = "tui.scroll.position"
	TextListEmpty      TextID = "tui.list.empty"
	TextListPosition   TextID = "tui.list.position"
	TextListFiltered   TextID = "tui.list.filtered"
	TextListNoMatch    TextID = "tui.list.nomatch"
	TextListNoGroup    TextID = "tui.list.nogroup"
	TextSearchKey      TextID = "tui.search.key"
	TextSearchPrompt   TextID = "tui.search.prompt"
	TextFilterAll      TextID = "tui.filter.all"

	TextCommandTitle   TextID = "tui.command.title"
	TextCommandPrompt  TextID = "tui.command.prompt"
	TextCommandNoMatch TextID = "tui.command.nomatch"
	TextCommandUnknown TextID = "tui.command.unknown"

	TextFormEmpty         TextID = "tui.form.empty"
	TextFormMaskedEntered TextID = "tui.form.maskedentered"
	TextFormMaskedKept    TextID = "tui.form.maskedkept"
	TextFormKeyPress      TextID = "tui.form.keypress"
	TextFormSectionClosed TextID = "tui.form.sectionclosed"

	TextLeaveTitle       TextID = "tui.leave.title"
	TextLeaveConsequence TextID = "tui.leave.consequence"

	TextHelpTitle      TextID = "tui.help.title"
	TextInfoTitle      TextID = "tui.info.title"
	TextHelpKeysScreen TextID = "tui.help.keysscreen"
	TextHelpKeysAll    TextID = "tui.help.keysall"

	TextSettingsTitle    TextID = "tui.settings.title"
	TextSettingsGeneral  TextID = "tui.settings.general"
	TextSettingTheme     TextID = "tui.setting.theme"
	TextSettingStart     TextID = "tui.setting.start"
	TextSettingIcons     TextID = "tui.setting.icons"
	TextSettingLanguage  TextID = "tui.setting.language"
	TextSettingMouse     TextID = "tui.setting.mouse"
	TextSettingDefault   TextID = "tui.setting.default"
	TextSettingsSaved    TextID = "tui.settings.saved"
	TextSettingsNotSaved TextID = "tui.settings.notsaved"
	TextValueOn          TextID = "tui.value.on"
	TextValueOff         TextID = "tui.value.off"
	TextValueNone        TextID = "tui.value.none"
	TextThemeUnknown     TextID = "tui.theme.unknown"
)

// ModuleCatalog returns the general texts of this package in English and German. Each call returns a new
// copy.
func ModuleCatalog() i18n.Catalogs[TextID] {
	return i18n.Catalogs[TextID]{
		i18n.English: {
			TextKeyUp:        "up",
			TextKeyDown:      "down",
			TextKeyLeft:      "left",
			TextKeyRight:     "right",
			TextKeyFirst:     "first",
			TextKeyLast:      "last",
			TextKeyPageUp:    "page up",
			TextKeyPageDown:  "page down",
			TextKeyNext:      "next zone or field",
			TextKeyPrevious:  "previous zone or field",
			TextKeyNextTopic: "next topic",
			TextKeyPrevTopic: "previous topic",
			TextKeyEnter:     "open or confirm",
			TextKeyToggle:    "toggle",
			TextKeySearch:    "search",
			TextKeyFilter:    "filter",
			TextKeyBack:      "back",
			TextKeyQuit:      "quit",
			TextKeyHelp:      "help",
			TextKeyInfo:      "info",
			TextKeyCommands:  "commands",
			TextKeySave:      "save",
			TextKeyYes:       "yes",
			TextKeyNo:        "no",
			TextKeyNew:       "new",
			TextKeyEdit:      "edit",
			TextKeyRename:    "rename",
			TextKeyCopy:      "copy",
			TextKeyDelete:    "delete",
			TextKeyDiscard:   "discard",
			TextKeyReload:    "reload",
			TextKeyChoose:    "choose",

			TextCancelled:      "Cancelled",
			TextCopyHandedOver: "Handed to the terminal for the clipboard; the terminal does not confirm it",
			TextWarning:        "warning: %s",
			TextError:          "error: %s",
			TextResizeTitle:    "Resize terminal",
			TextResizeNeed:     "Need %dx%d",
			TextResizeHave:     "Have %dx%d",
			TextScrollPosition: "lines %d-%d of %d",
			TextListEmpty:      "Nothing to show yet.",
			TextListPosition:   "%d-%d of %d",
			TextListFiltered:   "%d/%d (%d total)",
			TextListNoMatch:    "No entry matches %q; %s clears the filter.",
			TextListNoGroup:    "no group",
			TextSearchKey:      "%s search…",
			TextSearchPrompt:   "Type to search",
			TextFilterAll:      "all",

			TextCommandTitle:   "Commands",
			TextCommandPrompt:  "search or run a command…",
			TextCommandNoMatch: "no command matches",
			TextCommandUnknown: "unknown command %q; clear the line to list them all",

			TextFormEmpty:         "empty",
			TextFormMaskedEntered: "(entered, hidden)",
			TextFormMaskedKept:    "(stored, unchanged)",
			TextFormKeyPress:      "press the new key",
			TextFormSectionClosed: "(closed)",

			TextLeaveTitle:       "Unsaved changes",
			TextLeaveConsequence: "Leaving drops them.",

			TextHelpTitle:      "Help",
			TextInfoTitle:      "Info",
			TextHelpKeysScreen: "This screen",
			TextHelpKeysAll:    "Everywhere",

			TextSettingsTitle:    "Settings",
			TextSettingsGeneral:  "General",
			TextSettingTheme:     "theme",
			TextSettingStart:     "start view",
			TextSettingIcons:     "icons",
			TextSettingLanguage:  "language",
			TextSettingMouse:     "mouse",
			TextSettingDefault:   "(default)",
			TextSettingsSaved:    "Saved on this device.",
			TextSettingsNotSaved: "The preferences could not be written.",
			TextValueOn:          "on",
			TextValueOff:         "off",
			TextValueNone:        "none",
			TextThemeUnknown:     "theme %q is not a theme of this version, using %s",
		},
		i18n.German: {
			TextKeyUp:        "Hoch",
			TextKeyDown:      "Runter",
			TextKeyLeft:      "Links",
			TextKeyRight:     "Rechts",
			TextKeyFirst:     "Anfang",
			TextKeyLast:      "Ende",
			TextKeyPageUp:    "Seite hoch",
			TextKeyPageDown:  "Seite runter",
			TextKeyNext:      "Nächste Zone oder nächstes Feld",
			TextKeyPrevious:  "Vorige Zone oder voriges Feld",
			TextKeyNextTopic: "Nächstes Thema",
			TextKeyPrevTopic: "Voriges Thema",
			TextKeyEnter:     "Öffnen oder bestätigen",
			TextKeyToggle:    "Umschalten",
			TextKeySearch:    "Suchen",
			TextKeyFilter:    "Filter",
			TextKeyBack:      "Zurück",
			TextKeyQuit:      "Beenden",
			TextKeyHelp:      "Hilfe",
			TextKeyInfo:      "Info",
			TextKeyCommands:  "Befehle",
			TextKeySave:      "Speichern",
			TextKeyYes:       "Ja",
			TextKeyNo:        "Nein",
			TextKeyNew:       "Neu",
			TextKeyEdit:      "Bearbeiten",
			TextKeyRename:    "Umbenennen",
			TextKeyCopy:      "Kopieren",
			TextKeyDelete:    "Löschen",
			TextKeyDiscard:   "Verwerfen",
			TextKeyReload:    "Neu laden",
			TextKeyChoose:    "Auswählen",

			TextCancelled:      "Abgebrochen",
			TextCopyHandedOver: "An das Terminal für die Zwischenablage übergeben; das Terminal bestätigt es nicht",
			TextWarning:        "Warnung: %s",
			TextError:          "Fehler: %s",
			TextResizeTitle:    "Terminal vergrößern",
			TextResizeNeed:     "Benötigt: %dx%d",
			TextResizeHave:     "Aktuell: %dx%d",
			TextScrollPosition: "Zeilen %d-%d von %d",
			TextListEmpty:      "Noch nichts anzuzeigen.",
			TextListPosition:   "%d-%d von %d",
			TextListFiltered:   "%d/%d (%d gesamt)",
			TextListNoMatch:    "Kein Eintrag passt zu %q; %s leert den Filter.",
			TextListNoGroup:    "ohne Gruppe",
			TextSearchKey:      "%s suchen…",
			TextSearchPrompt:   "Zum Suchen tippen",
			TextFilterAll:      "alle",

			TextCommandTitle:   "Befehle",
			TextCommandPrompt:  "Befehl suchen oder ausführen…",
			TextCommandNoMatch: "Kein Befehl passt",
			TextCommandUnknown: "Unbekannter Befehl %q; Zeile leeren, um alle zu listen",

			TextFormEmpty:         "leer",
			TextFormMaskedEntered: "(eingegeben, verborgen)",
			TextFormMaskedKept:    "(gespeichert, unverändert)",
			TextFormKeyPress:      "neue Taste drücken",
			TextFormSectionClosed: "(zu)",

			TextLeaveTitle:       "Ungespeicherte Änderungen",
			TextLeaveConsequence: "Beim Verlassen gehen sie verloren.",

			TextHelpTitle:      "Hilfe",
			TextInfoTitle:      "Info",
			TextHelpKeysScreen: "Dieser Bildschirm",
			TextHelpKeysAll:    "Überall",

			TextSettingsTitle:    "Einstellungen",
			TextSettingsGeneral:  "Allgemein",
			TextSettingTheme:     "Theme",
			TextSettingStart:     "Startansicht",
			TextSettingIcons:     "Symbole",
			TextSettingLanguage:  "Sprache",
			TextSettingMouse:     "Maus",
			TextSettingDefault:   "(Standard)",
			TextSettingsSaved:    "Auf diesem Gerät gespeichert.",
			TextSettingsNotSaved: "Die Einstellungen ließen sich nicht schreiben.",
			TextValueOn:          "an",
			TextValueOff:         "aus",
			TextValueNone:        "keine",
			TextThemeUnknown:     "Theme %q gibt es in dieser Version nicht, verwendet wird %s",
		},
	}
}
