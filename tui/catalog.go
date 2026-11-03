package tui

import "github.com/castrowithcee/qatlas-kit/i18n"

// The IDs of ModuleCatalog.
const (
	TextKeyUp       TextID = "tui.key.up"
	TextKeyDown     TextID = "tui.key.down"
	TextKeyLeft     TextID = "tui.key.left"
	TextKeyRight    TextID = "tui.key.right"
	TextKeyFirst    TextID = "tui.key.first"
	TextKeyLast     TextID = "tui.key.last"
	TextKeyPageUp   TextID = "tui.key.pageup"
	TextKeyPageDown TextID = "tui.key.pagedown"
	TextKeyNext     TextID = "tui.key.next"
	TextKeyPrevious TextID = "tui.key.previous"
	TextKeyEnter    TextID = "tui.key.enter"
	TextKeyToggle   TextID = "tui.key.toggle"
	TextKeySearch   TextID = "tui.key.search"
	TextKeyFilter   TextID = "tui.key.filter"
	TextKeyBack     TextID = "tui.key.back"
	TextKeyQuit     TextID = "tui.key.quit"
	TextKeyHelp     TextID = "tui.key.help"
	TextKeyInfo     TextID = "tui.key.info"
	TextKeyCommands TextID = "tui.key.commands"
	TextKeySave     TextID = "tui.key.save"
	TextKeyYes      TextID = "tui.key.yes"
	TextKeyNo       TextID = "tui.key.no"
	TextKeyNew      TextID = "tui.key.new"
	TextKeyEdit     TextID = "tui.key.edit"
	TextKeyRename   TextID = "tui.key.rename"
	TextKeyCopy     TextID = "tui.key.copy"
	TextKeyDelete   TextID = "tui.key.delete"
	TextKeyDiscard  TextID = "tui.key.discard"
	TextKeyReload   TextID = "tui.key.reload"
	TextKeyChoose   TextID = "tui.key.choose"

	TextCancelled      TextID = "tui.cancelled"
	TextCopyHandedOver TextID = "tui.copyhandedover"
	TextWarning        TextID = "tui.warning"
	TextError          TextID = "tui.error"
	TextResizeTitle    TextID = "tui.resize.title"
	TextResizeNeed     TextID = "tui.resize.need"
	TextResizeHave     TextID = "tui.resize.have"
	TextScrollPosition TextID = "tui.scroll.position"
	TextListPosition   TextID = "tui.list.position"
	TextListFiltered   TextID = "tui.list.filtered"
	TextListNoMatch    TextID = "tui.list.nomatch"
	TextListNoGroup    TextID = "tui.list.nogroup"
	TextListAll        TextID = "tui.list.all"
	TextSearchKey      TextID = "tui.search.key"
	TextSearchPrompt   TextID = "tui.search.prompt"
	TextFilterAll      TextID = "tui.filter.all"
	TextFilterButton   TextID = "tui.filter.button"
	TextFilterTitle    TextID = "tui.filter.title"
	TextFilterApply    TextID = "tui.filter.apply"

	TextCommandTitle   TextID = "tui.command.title"
	TextCommandPrompt  TextID = "tui.command.prompt"
	TextCommandNoMatch TextID = "tui.command.nomatch"
	TextCommandUnknown TextID = "tui.command.unknown"

	TextKeyCalendar TextID = "tui.key.calendar"

	TextCalTitle     TextID = "tui.calendar.title"
	TextCalToday     TextID = "tui.calendar.today"
	TextCalPrevMonth TextID = "tui.calendar.prevmonth"
	TextCalNextMonth TextID = "tui.calendar.nextmonth"
	TextCalWeekStart TextID = "tui.calendar.weekstart"
	TextCalMonthJan  TextID = "tui.calendar.month.jan"
	TextCalMonthFeb  TextID = "tui.calendar.month.feb"
	TextCalMonthMar  TextID = "tui.calendar.month.mar"
	TextCalMonthApr  TextID = "tui.calendar.month.apr"
	TextCalMonthMay  TextID = "tui.calendar.month.may"
	TextCalMonthJun  TextID = "tui.calendar.month.jun"
	TextCalMonthJul  TextID = "tui.calendar.month.jul"
	TextCalMonthAug  TextID = "tui.calendar.month.aug"
	TextCalMonthSep  TextID = "tui.calendar.month.sep"
	TextCalMonthOct  TextID = "tui.calendar.month.oct"
	TextCalMonthNov  TextID = "tui.calendar.month.nov"
	TextCalMonthDec  TextID = "tui.calendar.month.dec"
	TextCalDaySun    TextID = "tui.calendar.day.sun"
	TextCalDayMon    TextID = "tui.calendar.day.mon"
	TextCalDayTue    TextID = "tui.calendar.day.tue"
	TextCalDayWed    TextID = "tui.calendar.day.wed"
	TextCalDayThu    TextID = "tui.calendar.day.thu"
	TextCalDayFri    TextID = "tui.calendar.day.fri"
	TextCalDaySat    TextID = "tui.calendar.day.sat"

	TextFormEmpty         TextID = "tui.form.empty"
	TextFormMaskedEntered TextID = "tui.form.maskedentered"
	TextFormMaskedKept    TextID = "tui.form.maskedkept"
	TextFormKeyPress      TextID = "tui.form.keypress"
	TextFormSectionClosed TextID = "tui.form.sectionclosed"
	TextFormDateFormat    TextID = "tui.form.dateformat"
	TextFormDateNoSuch    TextID = "tui.form.datenosuch"
	TextFormDateEarly     TextID = "tui.form.dateearly"
	TextFormDateLate      TextID = "tui.form.datelate"
	TextFormDateUseDate   TextID = "tui.form.dateusedate"
	TextFormDateUseTime   TextID = "tui.form.dateusetime"
	TextFormDateFrom      TextID = "tui.form.datefrom"
	TextFormDateUntil     TextID = "tui.form.dateuntil"

	TextLeaveTitle       TextID = "tui.leave.title"
	TextLeaveConsequence TextID = "tui.leave.consequence"

	TextHelpTitle      TextID = "tui.help.title"
	TextInfoTitle      TextID = "tui.info.title"
	TextHelpKeysScreen TextID = "tui.help.keysscreen"
	TextHelpKeysAll    TextID = "tui.help.keysall"
	TextHelpTopic      TextID = "tui.help.topic"
	TextHelpScroll     TextID = "tui.help.scroll"
	TextHelpPage       TextID = "tui.help.page"
	TextHelpClose      TextID = "tui.help.close"
	TextInfoLink       TextID = "tui.info.link"
	TextInfoOpenLink   TextID = "tui.info.openlink"

	TextLogFollow     TextID = "tui.log.follow"
	TextLogNextMatch  TextID = "tui.log.nextmatch"
	TextLogPrevMatch  TextID = "tui.log.prevmatch"
	TextLogWrap       TextID = "tui.log.wrap"
	TextLogTimestamps TextID = "tui.log.timestamps"
	TextLogFollowing  TextID = "tui.log.following"
	TextLogPaused     TextID = "tui.log.paused"
	TextLogPausedNew  TextID = "tui.log.pausednew"
	TextLogMatch      TextID = "tui.log.match"
	TextLogMatches    TextID = "tui.log.matches"
	TextLogNoMatches  TextID = "tui.log.nomatches"
	TextLogCut        TextID = "tui.log.cut"
	TextLogEmpty      TextID = "tui.log.empty"

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
			TextKeyUp:       "up",
			TextKeyDown:     "down",
			TextKeyLeft:     "left",
			TextKeyRight:    "right",
			TextKeyFirst:    "first",
			TextKeyLast:     "last",
			TextKeyPageUp:   "page up",
			TextKeyPageDown: "page down",
			TextKeyNext:     "next zone or field",
			TextKeyPrevious: "previous zone or field",
			TextKeyEnter:    "open or confirm",
			TextKeyToggle:   "toggle",
			TextKeySearch:   "search",
			TextKeyFilter:   "filter",
			TextKeyBack:     "back",
			TextKeyQuit:     "quit",
			TextKeyHelp:     "help",
			TextKeyInfo:     "info",
			TextKeyCommands: "commands",
			TextKeySave:     "save",
			TextKeyYes:      "yes",
			TextKeyNo:       "no",
			TextKeyNew:      "new",
			TextKeyEdit:     "edit",
			TextKeyRename:   "rename",
			TextKeyCopy:     "copy",
			TextKeyDelete:   "delete",
			TextKeyDiscard:  "discard",
			TextKeyReload:   "reload",
			TextKeyChoose:   "choose",

			TextCancelled:      "Cancelled",
			TextCopyHandedOver: "Handed to the terminal for the clipboard; the terminal does not confirm it",
			TextWarning:        "warning: %s",
			TextError:          "error: %s",
			TextResizeTitle:    "Resize terminal",
			TextResizeNeed:     "Need %dx%d",
			TextResizeHave:     "Have %dx%d",
			TextScrollPosition: "lines %d-%d of %d",
			TextListPosition:   "%d-%d of %d",
			TextListFiltered:   "%d/%d (%d total)",
			TextListNoMatch:    "No entry matches %q; %s clears the filter.",
			TextListNoGroup:    "no group",
			TextListAll:        "all",
			TextSearchKey:      "%s search…",
			TextSearchPrompt:   "Type to search",
			TextFilterAll:      "all",
			TextFilterButton:   "Filter (%d)",
			TextFilterTitle:    "Filter",
			TextFilterApply:    "apply",

			TextCommandTitle:   "Commands",
			TextCommandPrompt:  "search or run a command…",
			TextCommandNoMatch: "no command matches",
			TextCommandUnknown: "unknown command %q; clear the line to list them all",

			TextFormEmpty:         "empty",
			TextFormMaskedEntered: "(entered, hidden)",
			TextFormMaskedKept:    "(stored, unchanged)",
			TextFormKeyPress:      "press the new key",
			TextFormSectionClosed: "(closed)",
			TextFormDateFormat:    "not a date",
			TextFormDateNoSuch:    "that date or time does not exist",
			TextFormDateEarly:     "too early",
			TextFormDateLate:      "too late",
			TextFormDateUseDate:   "type it as YYYY-MM-DD",
			TextFormDateUseTime:   "type it as YYYY-MM-DD HH:MM",
			TextFormDateFrom:      "type %s or later",
			TextFormDateUntil:     "type %s or earlier",

			TextLeaveTitle:       "Unsaved changes",
			TextLeaveConsequence: "Leaving drops them.",

			TextHelpTitle:      "Help",
			TextInfoTitle:      "Info",
			TextHelpKeysScreen: "This screen",
			TextHelpKeysAll:    "Everywhere",
			TextHelpTopic:      "topic",
			TextHelpScroll:     "scroll",
			TextHelpPage:       "page",
			TextHelpClose:      "close",
			TextInfoLink:       "Documentation:",
			TextInfoOpenLink:   "open link",

			TextKeyCalendar:  "calendar",
			TextCalTitle:     "%s %d",
			TextCalToday:     "today",
			TextCalPrevMonth: "previous month",
			TextCalNextMonth: "next month",
			TextCalWeekStart: "0",
			TextCalMonthJan:  "January",
			TextCalMonthFeb:  "February",
			TextCalMonthMar:  "March",
			TextCalMonthApr:  "April",
			TextCalMonthMay:  "May",
			TextCalMonthJun:  "June",
			TextCalMonthJul:  "July",
			TextCalMonthAug:  "August",
			TextCalMonthSep:  "September",
			TextCalMonthOct:  "October",
			TextCalMonthNov:  "November",
			TextCalMonthDec:  "December",
			TextCalDaySun:    "Su",
			TextCalDayMon:    "Mo",
			TextCalDayTue:    "Tu",
			TextCalDayWed:    "We",
			TextCalDayThu:    "Th",
			TextCalDayFri:    "Fr",
			TextCalDaySat:    "Sa",

			TextLogFollow:     "follow",
			TextLogNextMatch:  "next match",
			TextLogPrevMatch:  "previous match",
			TextLogWrap:       "wrap lines",
			TextLogTimestamps: "timestamps",
			TextLogFollowing:  "following",
			TextLogPaused:     "paused",
			TextLogPausedNew:  "paused, %d new",
			TextLogMatch:      "match %d/%d",
			TextLogMatches:    "%d matches",
			TextLogNoMatches:  "no matches",
			TextLogCut:        "[cut]",
			TextLogEmpty:      "No lines yet.",

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
			TextKeyUp:       "Hoch",
			TextKeyDown:     "Runter",
			TextKeyLeft:     "Links",
			TextKeyRight:    "Rechts",
			TextKeyFirst:    "Anfang",
			TextKeyLast:     "Ende",
			TextKeyPageUp:   "Seite hoch",
			TextKeyPageDown: "Seite runter",
			TextKeyNext:     "Nächste Zone oder nächstes Feld",
			TextKeyPrevious: "Vorige Zone oder voriges Feld",
			TextKeyEnter:    "Öffnen oder bestätigen",
			TextKeyToggle:   "Umschalten",
			TextKeySearch:   "Suchen",
			TextKeyFilter:   "Filter",
			TextKeyBack:     "Zurück",
			TextKeyQuit:     "Beenden",
			TextKeyHelp:     "Hilfe",
			TextKeyInfo:     "Info",
			TextKeyCommands: "Befehle",
			TextKeySave:     "Speichern",
			TextKeyYes:      "Ja",
			TextKeyNo:       "Nein",
			TextKeyNew:      "Neu",
			TextKeyEdit:     "Bearbeiten",
			TextKeyRename:   "Umbenennen",
			TextKeyCopy:     "Kopieren",
			TextKeyDelete:   "Löschen",
			TextKeyDiscard:  "Verwerfen",
			TextKeyReload:   "Neu laden",
			TextKeyChoose:   "Auswählen",

			TextCancelled:      "Abgebrochen",
			TextCopyHandedOver: "An das Terminal für die Zwischenablage übergeben; das Terminal bestätigt es nicht",
			TextWarning:        "Warnung: %s",
			TextError:          "Fehler: %s",
			TextResizeTitle:    "Terminal vergrößern",
			TextResizeNeed:     "Benötigt: %dx%d",
			TextResizeHave:     "Aktuell: %dx%d",
			TextScrollPosition: "Zeilen %d-%d von %d",
			TextListPosition:   "%d-%d von %d",
			TextListFiltered:   "%d/%d (%d gesamt)",
			TextListNoMatch:    "Kein Eintrag passt zu %q; %s leert den Filter.",
			TextListNoGroup:    "ohne Gruppe",
			TextListAll:        "alle",
			TextSearchKey:      "%s suchen…",
			TextSearchPrompt:   "Zum Suchen tippen",
			TextFilterAll:      "alle",
			TextFilterButton:   "Filter (%d)",
			TextFilterTitle:    "Filter",
			TextFilterApply:    "übernehmen",

			TextCommandTitle:   "Befehle",
			TextCommandPrompt:  "Befehl suchen oder ausführen…",
			TextCommandNoMatch: "Kein Befehl passt",
			TextCommandUnknown: "Unbekannter Befehl %q; Zeile leeren, um alle zu listen",

			TextFormEmpty:         "leer",
			TextFormMaskedEntered: "(eingegeben, verborgen)",
			TextFormMaskedKept:    "(gespeichert, unverändert)",
			TextFormKeyPress:      "neue Taste drücken",
			TextFormSectionClosed: "(zu)",
			TextFormDateFormat:    "kein Datum",
			TextFormDateNoSuch:    "dieses Datum oder diese Uhrzeit gibt es nicht",
			TextFormDateEarly:     "zu früh",
			TextFormDateLate:      "zu spät",
			TextFormDateUseDate:   "als JJJJ-MM-TT eingeben",
			TextFormDateUseTime:   "als JJJJ-MM-TT HH:MM eingeben",
			TextFormDateFrom:      "%s oder später eingeben",
			TextFormDateUntil:     "%s oder früher eingeben",

			TextLeaveTitle:       "Ungespeicherte Änderungen",
			TextLeaveConsequence: "Beim Verlassen gehen sie verloren.",

			TextHelpTitle:      "Hilfe",
			TextInfoTitle:      "Info",
			TextHelpKeysScreen: "Dieser Bildschirm",
			TextHelpKeysAll:    "Überall",
			TextHelpTopic:      "Thema",
			TextHelpScroll:     "Scrollen",
			TextHelpPage:       "Seite",
			TextHelpClose:      "Schließen",
			TextInfoLink:       "Dokumentation:",
			TextInfoOpenLink:   "Link öffnen",

			TextKeyCalendar:  "Kalender",
			TextCalTitle:     "%s %d",
			TextCalToday:     "heute",
			TextCalPrevMonth: "voriger Monat",
			TextCalNextMonth: "nächster Monat",
			TextCalWeekStart: "1",
			TextCalMonthJan:  "Januar",
			TextCalMonthFeb:  "Februar",
			TextCalMonthMar:  "März",
			TextCalMonthApr:  "April",
			TextCalMonthMay:  "Mai",
			TextCalMonthJun:  "Juni",
			TextCalMonthJul:  "Juli",
			TextCalMonthAug:  "August",
			TextCalMonthSep:  "September",
			TextCalMonthOct:  "Oktober",
			TextCalMonthNov:  "November",
			TextCalMonthDec:  "Dezember",
			TextCalDaySun:    "So",
			TextCalDayMon:    "Mo",
			TextCalDayTue:    "Di",
			TextCalDayWed:    "Mi",
			TextCalDayThu:    "Do",
			TextCalDayFri:    "Fr",
			TextCalDaySat:    "Sa",

			TextLogFollow:     "mitlaufen",
			TextLogNextMatch:  "nächster Treffer",
			TextLogPrevMatch:  "voriger Treffer",
			TextLogWrap:       "Zeilen umbrechen",
			TextLogTimestamps: "Zeitstempel",
			TextLogFollowing:  "mitlaufend",
			TextLogPaused:     "angehalten",
			TextLogPausedNew:  "angehalten, %d neu",
			TextLogMatch:      "Treffer %d/%d",
			TextLogMatches:    "%d Treffer",
			TextLogNoMatches:  "keine Treffer",
			TextLogCut:        "[gekürzt]",
			TextLogEmpty:      "Noch keine Zeilen.",

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
