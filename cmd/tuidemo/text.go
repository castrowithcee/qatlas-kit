package main

import (
	"github.com/castrowithcee/qatlas-kit/i18n"
	"github.com/castrowithcee/qatlas-kit/tui"
)

// The texts of the program. The IDs are stable and untranslated; a text names the key of an action as
// "{action}", so a rebinding shows in every text.
const (
	textProduct tui.TextID = "demo.product"

	textKeyArea     tui.TextID = "demo.key.area"
	textKeyZone     tui.TextID = "demo.key.zone"
	textKeyTheme    tui.TextID = "demo.key.theme"
	textKeyLanguage tui.TextID = "demo.key.language"
	textKeyIcons    tui.TextID = "demo.key.icons"
	textKeyMouse    tui.TextID = "demo.key.mouse"
	textKeyCount    tui.TextID = "demo.key.count"

	textHintStep tui.TextID = "demo.hint.step"
	textHintZone tui.TextID = "demo.hint.zone"

	textAreaOverview tui.TextID = "demo.area.overview"
	textAreaFrame    tui.TextID = "demo.area.frame"
	textAreaSettings tui.TextID = "demo.area.settings"

	textTabAbout  tui.TextID = "demo.tab.about"
	textTabSample tui.TextID = "demo.tab.sample"
	textTabTabs   tui.TextID = "demo.tab.tabs"
	textTabMouse  tui.TextID = "demo.tab.mouse"
	textTabLook   tui.TextID = "demo.tab.look"
	textTabInput  tui.TextID = "demo.tab.input"

	textAboutHeading tui.TextID = "demo.about.heading"
	textAboutBody    tui.TextID = "demo.about.body"
	textAboutKeys    tui.TextID = "demo.about.keys"

	textSampleTitle tui.TextID = "demo.sample.title"
	textSampleNote  tui.TextID = "demo.sample.note"
	textSampleName  tui.TextID = "demo.sample.name"
	textSampleBed   tui.TextID = "demo.sample.bed"
	textSampleDate  tui.TextID = "demo.sample.date"

	textTabsHeading tui.TextID = "demo.tabs.heading"
	textTabsBody    tui.TextID = "demo.tabs.body"
	textTabsFocus   tui.TextID = "demo.tabs.focus"

	textMouseHeading tui.TextID = "demo.mouse.heading"
	textMouseBody    tui.TextID = "demo.mouse.body"
	textMouseLast    tui.TextID = "demo.mouse.last"
	textMouseNone    tui.TextID = "demo.mouse.none"
	textMouseWheel   tui.TextID = "demo.mouse.wheel"
	textMouseCount   tui.TextID = "demo.mouse.count"
	textMouseButton  tui.TextID = "demo.mouse.button"

	textSettingsLookBody  tui.TextID = "demo.settings.lookbody"
	textSettingsInputBody tui.TextID = "demo.settings.inputbody"

	textStatusTheme    tui.TextID = "demo.status.theme"
	textStatusLanguage tui.TextID = "demo.status.language"
	textStatusIcons    tui.TextID = "demo.status.icons"
	textStatusMouse    tui.TextID = "demo.status.mouse"

	textKeyAsk    tui.TextID = "demo.key.ask"
	textKeyNotice tui.TextID = "demo.key.notice"

	textAreaDialogs tui.TextID = "demo.area.dialogs"
	textAreaHelp    tui.TextID = "demo.area.help"

	textTabQuestion tui.TextID = "demo.tab.question"
	textTabNotice   tui.TextID = "demo.tab.notice"
	textTabHelp     tui.TextID = "demo.tab.help"
	textTabExplain  tui.TextID = "demo.tab.explain"
	textTabCommands tui.TextID = "demo.tab.commands"

	textBodyQuestion tui.TextID = "demo.body.question"
	textBodyInput    tui.TextID = "demo.body.input"
	textBodyNotice   tui.TextID = "demo.body.notice"
	textBodyHelp     tui.TextID = "demo.body.help"
	textBodyExplain  tui.TextID = "demo.body.explain"
	textBodyCommands tui.TextID = "demo.body.commands"

	textOK tui.TextID = "demo.ok"

	textAskTitle       tui.TextID = "demo.ask.title"
	textAskQuestion    tui.TextID = "demo.ask.question"
	textAskConsequence tui.TextID = "demo.ask.consequence"
	textAskDetail      tui.TextID = "demo.ask.detail"
	textAskDone        tui.TextID = "demo.ask.done"

	textRenameTitle    tui.TextID = "demo.rename.title"
	textRenameQuestion tui.TextID = "demo.rename.question"
	textRenameEmpty    tui.TextID = "demo.rename.empty"
	textRenameDone     tui.TextID = "demo.rename.done"
	textRenameValue    tui.TextID = "demo.rename.value"

	textNoticeTitle   tui.TextID = "demo.notice.title"
	textNoticeLineOne tui.TextID = "demo.notice.lineone"
	textNoticeLineTwo tui.TextID = "demo.notice.linetwo"

	textHelpNameStart tui.TextID = "demo.help.namestart"
	textHelpNameKeys  tui.TextID = "demo.help.namekeys"
	textHelpNameMouse tui.TextID = "demo.help.namemouse"
	textHelpStart     tui.TextID = "demo.help.start"
	textHelpKeys      tui.TextID = "demo.help.keys"
	textHelpMouse     tui.TextID = "demo.help.mouse"
	textHelpNote      tui.TextID = "demo.help.note"

	textExplainTitle tui.TextID = "demo.explain.title"
	textExplainBody  tui.TextID = "demo.explain.body"
	textTermTitle    tui.TextID = "demo.term.title"
	textTermText     tui.TextID = "demo.term.text"

	textCmdOpen tui.TextID = "demo.cmd.open"

	textChanged  tui.TextID = "demo.changed"
	textTooSmall tui.TextID = "demo.toosmall"
)

// catalog returns the texts of the program in English and German.
func catalog() i18n.Catalogs[tui.TextID] {
	return i18n.Catalogs[tui.TextID]{
		i18n.English: {
			textProduct: "Kit workbench",

			textKeyArea:     "open area",
			textKeyZone:     "move focus",
			textKeyTheme:    "switch theme",
			textKeyLanguage: "switch language",
			textKeyIcons:    "switch icons",
			textKeyMouse:    "switch mouse",
			textKeyCount:    "count",

			textHintStep: "step",
			textHintZone: "focus",

			textAreaOverview: "Overview",
			textAreaFrame:    "Frame",
			textAreaSettings: "Settings",

			textTabAbout:  "About",
			textTabSample: "Sample data",
			textTabTabs:   "Tabs",
			textTabMouse:  "Mouse",
			textTabLook:   "Look",
			textTabInput:  "Input",

			textAboutHeading: "Terminal building blocks at work",
			textAboutBody: "Every tab shows one group of blocks with invented data. Open a tab with its digit, " +
				"step with {left} and {right}, move the focus between the tab lines with {zone.next}. " +
				"Everything also works by click while the mouse is on.",
			textAboutKeys: "Switches: {set.theme} theme, {set.language} language, {set.icons} icons, {set.mouse} mouse, {quit} quit.",

			textSampleTitle: "Garden journal",
			textSampleNote:  "invented data",
			textSampleName:  "Plant",
			textSampleBed:   "Bed",
			textSampleDate:  "Watered",

			textTabsHeading: "Tabs and sub-tabs",
			textTabsBody: "The tab line holds one tab per group of blocks, the line below it the sub-tabs of the open " +
				"tab. Both can be clicked. A narrow terminal shortens the inactive tabs to their digit.",
			textTabsFocus: "Focus: %s",

			textMouseHeading: "Mouse probe",
			textMouseBody:    "Click anywhere and turn the wheel: the frame reports what the click hit. {count} or a click on the button counts.",
			textMouseLast:    "Last click: column %d, row %d, hit: %s",
			textMouseNone:    "nothing",
			textMouseWheel:   "Wheel notches: %d",
			textMouseCount:   "Count: %d",
			textMouseButton:  "Count",

			textSettingsLookBody:  "Theme and icons change at once. Press the key or click the row.",
			textSettingsInputBody: "Language and mouse change at once. Press the key or click the row.",

			textStatusTheme:    "theme: %s",
			textStatusLanguage: "language: %s",
			textStatusIcons:    "icons: %s",
			textStatusMouse:    "mouse: %s",

			textKeyAsk:    "ask question",
			textKeyNotice: "show notice",

			textAreaDialogs: "Dialogs",
			textAreaHelp:    "Help",

			textTabQuestion: "Question",
			textTabNotice:   "Notice",
			textTabHelp:     "Help",
			textTabExplain:  "Explanation",
			textTabCommands: "Command line",

			textBodyQuestion: "A question over the screen: {answer.yes} or {answer.no} answers it, {enter} gives the default, {back} cancels. " +
				"The buttons answer by click.",
			textBodyInput:    "A short input: type a name, {enter} takes it, {back} cancels. An empty name is refused inside the dialog.",
			textBodyNotice:   "A calm message without a question, closed with {enter} or {back}.",
			textBodyHelp:     "The help shows topics: {left} and {right} change the topic, {up} and {down} scroll, {help} or {back} closes it.",
			textBodyExplain:  "The explanation of the focused element: {info} or {back} closes it, {copy} hands its text to the terminal for the clipboard.",
			textBodyCommands: "The command line runs commands by name, such as the switches and the tabs: type to filter, {enter} runs, {back} closes.",

			textOK: "OK",

			textAskTitle:       "Reset counter",
			textAskQuestion:    "Reset the counter?",
			textAskConsequence: "The count of the mouse probe goes back to 0.",
			textAskDetail:      "Current count: %d",
			textAskDone:        "Counter reset",

			textRenameTitle:    "Rename journal",
			textRenameQuestion: "Name of the garden journal:",
			textRenameEmpty:    "The name must not be empty.",
			textRenameDone:     "Journal renamed to %s",
			textRenameValue:    "Herb garden",

			textNoticeTitle:   "Notice",
			textNoticeLineOne: "This is a calm message without a question.",
			textNoticeLineTwo: "Nothing was changed.",

			textHelpNameStart: "Start",
			textHelpNameKeys:  "Keys",
			textHelpNameMouse: "Mouse",
			textHelpStart: "About the workbench:\n" +
				"Every tab shows one group of building blocks with invented data. Open a tab with its digit and move between the tabs with the arrow keys.\n" +
				"The switches for theme, language, icons and mouse act at once.",
			textHelpKeys: "Keys:\n" +
				"The key table has one place for every key. The footer names the keys that act on the screen you see, and the list at the end of this topic names all of them.",
			textHelpMouse: "Mouse:\n" +
				"A click does what its key does, with the same checks. Keys, tabs, rows and buttons are click targets. The wheel scrolls where text is longer than its room.",
			textHelpNote: "Invented help topics.",

			textExplainTitle: "Garden journal",
			textExplainBody:  "The sample data of this workbench is an invented journal of herbs and vegetables in a few beds and pots. No real garden is behind it.",
			textTermTitle:    "Raised bed",
			textTermText:     "A raised bed is a garden bed built above the ground. It is a word of the sample data, not of the kit.",

			textCmdOpen: "Open the tab %s",

			textChanged:  "%s: %s",
			textTooSmall: "Terminal too small",
		},
		i18n.German: {
			textProduct: "Kit-Werkbank",

			textKeyArea:     "Bereich öffnen",
			textKeyZone:     "Fokus wechseln",
			textKeyTheme:    "Theme wechseln",
			textKeyLanguage: "Sprache wechseln",
			textKeyIcons:    "Icons wechseln",
			textKeyMouse:    "Maus umschalten",
			textKeyCount:    "zählen",

			textHintStep: "wechseln",
			textHintZone: "Fokus",

			textAreaOverview: "Übersicht",
			textAreaFrame:    "Rahmen",
			textAreaSettings: "Einstellungen",

			textTabAbout:  "Info",
			textTabSample: "Beispieldaten",
			textTabTabs:   "Reiter",
			textTabMouse:  "Maus",
			textTabLook:   "Aussehen",
			textTabInput:  "Eingabe",

			textAboutHeading: "Terminal-Bausteine im Betrieb",
			textAboutBody: "Jeder Reiter zeigt eine Gruppe von Bausteinen mit erfundenen Daten. Öffne einen Reiter mit " +
				"seiner Ziffer, wechsle mit {left} und {right}, bewege den Fokus zwischen den Reiterzeilen mit {zone.next}. " +
				"Alles geht auch per Klick, solange die Maus an ist.",
			textAboutKeys: "Schalter: {set.theme} Theme, {set.language} Sprache, {set.icons} Icons, {set.mouse} Maus, {quit} Beenden.",

			textSampleTitle: "Gartentagebuch",
			textSampleNote:  "erfundene Daten",
			textSampleName:  "Pflanze",
			textSampleBed:   "Beet",
			textSampleDate:  "Gegossen",

			textTabsHeading: "Reiter und Unterreiter",
			textTabsBody: "Die Reiterzeile hält je Gruppe von Bausteinen einen Reiter, die Zeile darunter die Unterreiter " +
				"des offenen Reiters. Beide lassen sich anklicken. Ein schmales Terminal kürzt die inaktiven Reiter auf ihre Ziffer.",
			textTabsFocus: "Fokus: %s",

			textMouseHeading: "Maus-Sonde",
			textMouseBody:    "Klicke irgendwo und drehe das Rad: Der Rahmen meldet, was der Klick getroffen hat. {count} oder ein Klick auf die Schaltfläche zählt.",
			textMouseLast:    "Letzter Klick: Spalte %d, Zeile %d, getroffen: %s",
			textMouseNone:    "nichts",
			textMouseWheel:   "Radschritte: %d",
			textMouseCount:   "Zähler: %d",
			textMouseButton:  "Zählen",

			textSettingsLookBody:  "Theme und Icons wechseln sofort. Drücke die Taste oder klicke die Zeile.",
			textSettingsInputBody: "Sprache und Maus wechseln sofort. Drücke die Taste oder klicke die Zeile.",

			textStatusTheme:    "Theme: %s",
			textStatusLanguage: "Sprache: %s",
			textStatusIcons:    "Icons: %s",
			textStatusMouse:    "Maus: %s",

			textKeyAsk:    "Frage stellen",
			textKeyNotice: "Hinweis zeigen",

			textAreaDialogs: "Dialoge",
			textAreaHelp:    "Hilfe",

			textTabQuestion: "Rückfrage",
			textTabNotice:   "Hinweis",
			textTabHelp:     "Hilfe",
			textTabExplain:  "Erklärung",
			textTabCommands: "Befehlszeile",

			textBodyQuestion: "Eine Rückfrage über dem Bild: {answer.yes} oder {answer.no} beantwortet sie, {enter} gibt die Vorgabe, {back} bricht ab. " +
				"Die Schaltflächen antworten per Klick.",
			textBodyInput:    "Eine kurze Eingabe: Tippe einen Namen, {enter} übernimmt ihn, {back} bricht ab. Ein leerer Name wird im Dialog abgelehnt.",
			textBodyNotice:   "Eine ruhige Meldung ohne Frage, geschlossen mit {enter} oder {back}.",
			textBodyHelp:     "Die Hilfe zeigt Themen: {left} und {right} wechseln das Thema, {up} und {down} scrollen, {help} oder {back} schließt sie.",
			textBodyExplain:  "Die Erklärung des fokussierten Elements: {info} oder {back} schließt sie, {copy} reicht ihren Text für die Zwischenablage ans Terminal.",
			textBodyCommands: "Die Befehlszeile führt Befehle nach Namen aus, etwa die Schalter und die Reiter: Tippen filtert, {enter} führt aus, {back} schließt.",

			textOK: "OK",

			textAskTitle:       "Zähler zurücksetzen",
			textAskQuestion:    "Zähler zurücksetzen?",
			textAskConsequence: "Der Zähler der Maus-Sonde geht auf 0 zurück.",
			textAskDetail:      "Aktueller Stand: %d",
			textAskDone:        "Zähler zurückgesetzt",

			textRenameTitle:    "Tagebuch umbenennen",
			textRenameQuestion: "Name des Gartentagebuchs:",
			textRenameEmpty:    "Der Name darf nicht leer sein.",
			textRenameDone:     "Tagebuch umbenannt in %s",
			textRenameValue:    "Kräutergarten",

			textNoticeTitle:   "Hinweis",
			textNoticeLineOne: "Das ist eine ruhige Meldung ohne Frage.",
			textNoticeLineTwo: "Es wurde nichts geändert.",

			textHelpNameStart: "Start",
			textHelpNameKeys:  "Tasten",
			textHelpNameMouse: "Maus",
			textHelpStart: "Über die Werkbank:\n" +
				"Jeder Reiter zeigt eine Gruppe von Bausteinen mit erfundenen Daten. Öffne einen Reiter mit seiner Ziffer und wechsle mit den Pfeiltasten zwischen den Reitern.\n" +
				"Die Schalter für Theme, Sprache, Icons und Maus wirken sofort.",
			textHelpKeys: "Tasten:\n" +
				"Die Tastentabelle hat für jede Taste einen Platz. Die Fußzeile nennt die Tasten, die auf dem sichtbaren Bild wirken, die Liste am Ende dieses Themas nennt alle.",
			textHelpMouse: "Maus:\n" +
				"Ein Klick tut, was seine Taste tut, mit denselben Prüfungen. Tasten, Reiter, Zeilen und Schaltflächen sind Klickziele. Das Rad scrollt, wo ein Text länger als sein Platz ist.",
			textHelpNote: "Erfundene Hilfethemen.",

			textExplainTitle: "Gartentagebuch",
			textExplainBody:  "Die Beispieldaten dieser Werkbank sind ein erfundenes Tagebuch über Kräuter und Gemüse in einigen Beeten und Töpfen. Kein echter Garten steht dahinter.",
			textTermTitle:    "Hochbeet",
			textTermText:     "Ein Hochbeet ist ein Gartenbeet, das über dem Boden gebaut ist. Es ist ein Wort der Beispieldaten, nicht des Kits.",

			textCmdOpen: "Reiter %s öffnen",

			textChanged:  "%s: %s",
			textTooSmall: "Terminal zu klein",
		},
	}
}
