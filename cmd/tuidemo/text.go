package main

import (
	"github.com/castrowithcee/qatlas-kit/i18n"
	"github.com/castrowithcee/qatlas-kit/tui"
)

// The texts of the program. The IDs are stable and untranslated; a text names the key of an action as
// "{action}", so a rebinding shows in every text.
const (
	textProduct tui.TextID = "demo.product"

	textKeyArea         tui.TextID = "demo.key.area"
	textKeyZone         tui.TextID = "demo.key.zone"
	textKeyTheme        tui.TextID = "demo.key.theme"
	textKeyLanguage     tui.TextID = "demo.key.language"
	textKeyThemeBack    tui.TextID = "demo.key.theme.back"
	textThemeTerminal   tui.TextID = "demo.theme.terminal"
	textThemeDark       tui.TextID = "demo.theme.demo-dark"
	textThemeLight      tui.TextID = "demo.theme.demo-light"
	textIconsUnicode    tui.TextID = "demo.icons.unicode"
	textIconsNerdFont   tui.TextID = "demo.icons.nerdfont"
	textIconsNone       tui.TextID = "demo.icons.none"
	textLanguageEnglish tui.TextID = "demo.language.english"
	textLanguageGerman  tui.TextID = "demo.language.deutsch"
	textLanguageSystem  tui.TextID = "demo.language.system"
	textKeyIcons        tui.TextID = "demo.key.icons"
	textKeyMouse        tui.TextID = "demo.key.mouse"
	textKeyCount        tui.TextID = "demo.key.count"
	textKeyNavigation   tui.TextID = "demo.key.navigation"
	textSettingNav      tui.TextID = "demo.setting.navigation"
	textNavTabs         tui.TextID = "demo.navigation.tabs"
	textNavPathLine     tui.TextID = "demo.navigation.pathline"
	textNavHeader       tui.TextID = "demo.navigation.header"
	textNavChoice       tui.TextID = "demo.navigation.choice"

	textHintStep tui.TextID = "demo.hint.step"
	textHintZone tui.TextID = "demo.hint.zone"
	textHintOpen tui.TextID = "demo.hint.open"

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

	textHelpNote tui.TextID = "demo.help.note"

	textExplainTitle tui.TextID = "demo.explain.title"
	textExplainBody  tui.TextID = "demo.explain.body"
	textTermTitle    tui.TextID = "demo.term.title"
	textTermText     tui.TextID = "demo.term.text"

	textCmdOpen tui.TextID = "demo.cmd.open"

	textChanged  tui.TextID = "demo.changed"
	textTooSmall tui.TextID = "demo.toosmall"

	textAreaLists     tui.TextID = "demo.area.lists"
	textTabListFilter tui.TextID = "demo.tab.list.filter"
	textTabListGroups tui.TextID = "demo.tab.list.groups"
	textTabListTicks  tui.TextID = "demo.tab.list.ticks"

	textNoteListFilter tui.TextID = "demo.note.list.filter"
	textNoteListGroups tui.TextID = "demo.note.list.groups"
	textNoteListTicks  tui.TextID = "demo.note.list.ticks"

	textListTickRecent tui.TextID = "demo.list.tick.recent"
	textListTickPot    tui.TextID = "demo.list.tick.pot"

	textListEmpty     tui.TextID = "demo.list.empty"
	textListBarGroup  tui.TextID = "demo.list.bar.group"
	textListBarNone   tui.TextID = "demo.list.bar.none"
	textListBarPlace  tui.TextID = "demo.list.bar.place"
	textListBarWater  tui.TextID = "demo.list.bar.water"
	textListBarAny    tui.TextID = "demo.list.bar.any"
	textListBarRecent tui.TextID = "demo.list.bar.recent"
	textListOpened    tui.TextID = "demo.list.opened"
	textListTicked    tui.TextID = "demo.list.ticked"

	textAreaForms     tui.TextID = "demo.area.forms"
	textTabFormFields tui.TextID = "demo.tab.form.fields"
	textTabFormDates  tui.TextID = "demo.tab.form.dates"
	textTabFormPaths  tui.TextID = "demo.tab.form.paths"
	textKeyReset      tui.TextID = "demo.key.reset"

	textNoteFormFields tui.TextID = "demo.note.form.fields"
	textNoteFormDates  tui.TextID = "demo.note.form.dates"
	textNoteFormPaths  tui.TextID = "demo.note.form.paths"

	textFormBedTitle         tui.TextID = "demo.form.bed.title"
	textFormDatesTitle       tui.TextID = "demo.form.dates.title"
	textFormPathsTitle       tui.TextID = "demo.form.paths.title"
	textFormFolder           tui.TextID = "demo.form.folder"
	textFormFolderHint       tui.TextID = "demo.form.folder.hint"
	textFormFile             tui.TextID = "demo.form.file"
	textFormFileHint         tui.TextID = "demo.form.file.hint"
	textFormPathsSaved       tui.TextID = "demo.form.paths.saved"
	textFormSave             tui.TextID = "demo.form.save"
	textFormReset            tui.TextID = "demo.form.reset"
	textFormName             tui.TextID = "demo.form.name"
	textFormNameHint         tui.TextID = "demo.form.name.hint"
	textFormNote             tui.TextID = "demo.form.note"
	textFormPlace            tui.TextID = "demo.form.place"
	textFormPlaceEast        tui.TextID = "demo.form.place.east"
	textFormPlaceSouth       tui.TextID = "demo.form.place.south"
	textFormPlaceNorth       tui.TextID = "demo.form.place.north"
	textFormExtras           tui.TextID = "demo.form.extras"
	textFormExtraMulch       tui.TextID = "demo.form.extra.mulch"
	textFormExtraNet         tui.TextID = "demo.form.extra.net"
	textFormExtraFrame       tui.TextID = "demo.form.extra.frame"
	textFormCode             tui.TextID = "demo.form.code"
	textFormCodeHint         tui.TextID = "demo.form.code.hint"
	textFormPlanted          tui.TextID = "demo.form.planted"
	textFormPlantedHint      tui.TextID = "demo.form.planted.hint"
	textFormHarvest          tui.TextID = "demo.form.harvest"
	textFormHarvestHint      tui.TextID = "demo.form.harvest.hint"
	textFormNeedName         tui.TextID = "demo.form.need.name"
	textFormNeedNameNext     tui.TextID = "demo.form.need.name.next"
	textFormNeedDay          tui.TextID = "demo.form.need.day"
	textFormNeedDayNext      tui.TextID = "demo.form.need.day.next"
	textFormHarvestEarly     tui.TextID = "demo.form.harvest.early"
	textFormHarvestEarlyNext tui.TextID = "demo.form.harvest.early.next"
	textFormBedSaved         tui.TextID = "demo.form.bed.saved"
	textFormDatesSaved       tui.TextID = "demo.form.dates.saved"
	textFormRefused          tui.TextID = "demo.form.refused"
	textFormCleared          tui.TextID = "demo.form.cleared"

	textAreaPickers     tui.TextID = "demo.area.pickers"
	textTabPickChoose   tui.TextID = "demo.tab.pick.choose"
	textTabPickFiles    tui.TextID = "demo.tab.pick.files"
	textTabPickSuggest  tui.TextID = "demo.tab.pick.suggest"
	textKeyPick         tui.TextID = "demo.key.pick"
	textKeySuggest      tui.TextID = "demo.key.suggest"
	textNotePickChoose  tui.TextID = "demo.note.pick.choose"
	textNotePickFiles   tui.TextID = "demo.note.pick.files"
	textNotePickSuggest tui.TextID = "demo.note.pick.suggest"
	textNone            tui.TextID = "demo.none"

	textPickTitle tui.TextID = "demo.pick.title"
	textPickBed   tui.TextID = "demo.pick.bed"
	textPickDone  tui.TextID = "demo.pick.done"

	textFileRoot      tui.TextID = "demo.file.root"
	textFileTake      tui.TextID = "demo.file.take"
	textFileChosen    tui.TextID = "demo.file.chosen"
	textFileChosenMsg tui.TextID = "demo.file.chosen.msg"
	textFileNeedFile  tui.TextID = "demo.file.need"

	textSuggestTitle    tui.TextID = "demo.suggest.title"
	textSuggestQuestion tui.TextID = "demo.suggest.question"
	textSuggestEmpty    tui.TextID = "demo.suggest.empty"
	textSuggestDone     tui.TextID = "demo.suggest.done"
	textSuggestPath     tui.TextID = "demo.suggest.path"

	textAreaViews    tui.TextID = "demo.area.views"
	textTabViewText  tui.TextID = "demo.tab.view.text"
	textTabViewLog   tui.TextID = "demo.tab.view.log"
	textTabViewChat  tui.TextID = "demo.tab.view.chat"
	textNoteViewText tui.TextID = "demo.note.view.text"
	textNoteViewLog  tui.TextID = "demo.note.view.log"
	textNoteViewChat tui.TextID = "demo.note.view.chat"

	textRevealTitle       tui.TextID = "demo.reveal.title"
	textRevealQuestion    tui.TextID = "demo.reveal.question"
	textRevealConsequence tui.TextID = "demo.reveal.consequence"
	textRevealDone        tui.TextID = "demo.reveal.done"

	textLogReloaded tui.TextID = "demo.log.reloaded"

	textChatEmpty      tui.TextID = "demo.chat.empty"
	textChatThanks     tui.TextID = "demo.chat.thanks"
	textChatLater      tui.TextID = "demo.chat.later"
	textChatThanksText tui.TextID = "demo.chat.thanks.text"
	textChatLaterText  tui.TextID = "demo.chat.later.text"
	textChatYou        tui.TextID = "demo.chat.you"
	textChatDelivered  tui.TextID = "demo.chat.delivered"
	textChatAnswer     tui.TextID = "demo.chat.answer"
	textChatSent       tui.TextID = "demo.chat.sent"
)

// catalog returns the texts of the program in English and German.
func catalog() i18n.Catalogs[tui.TextID] {
	return i18n.Catalogs[tui.TextID]{
		i18n.English: {
			textProduct: "Kit workbench",

			textKeyArea:         "open area",
			textKeyZone:         "move focus",
			textKeyTheme:        "switch theme",
			textKeyThemeBack:    "switch theme back",
			textThemeTerminal:   "terminal",
			textThemeDark:       "demo-dark",
			textThemeLight:      "demo-light",
			textIconsUnicode:    "unicode",
			textIconsNerdFont:   "nerdfont",
			textIconsNone:       "none",
			textLanguageEnglish: "english",
			textLanguageGerman:  "deutsch",
			textLanguageSystem:  "system",
			textKeyLanguage:     "switch language",
			textKeyIcons:        "switch icons",
			textKeyMouse:        "switch mouse",
			textKeyCount:        "count",
			textKeyNavigation:   "switch navigation",
			textSettingNav:      "navigation",
			textNavTabs:         "tabs",
			textNavPathLine:     "path line",
			textNavHeader:       "path in header",
			textNavChoice:       "choice in header",

			textHintStep: "step",
			textHintZone: "focus",
			textHintOpen: "open",

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

			textSettingsLookBody:  "Theme, icons and navigation change at once. Pick a value with left and right or a click, or press the key of a switch.",
			textSettingsInputBody: "Language and mouse change at once. Pick a value with left and right or a click, or press the key of a switch. Switched off, the mouse comes back only with M.",

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

			textHelpNote: "Invented help pages.",

			textExplainTitle: "Garden journal",
			textExplainBody:  "The sample data of this workbench is an invented journal of herbs and vegetables in a few beds and pots. No real garden is behind it.",
			textTermTitle:    "Raised bed",
			textTermText:     "A raised bed is a garden bed built above the ground. It is a word of the sample data, not of the kit.",

			textCmdOpen: "Open the tab %s",

			textChanged:  "%s: %s",
			textTooSmall: "Terminal too small",

			textAreaLists:     "Lists",
			textTabListFilter: "Filter",
			textTabListGroups: "Groups",
			textTabListTicks:  "Ticks",

			textNoteListFilter: "{search} types a filter, {filter} moves to the filter bar: {left} and {right} change a value, " +
				"{enter} opens its values. {enter} or a click on the marked row opens it.",
			textNoteListGroups: "{left} and {right} fold a group, so do {enter} and a click on its row. {search} types a filter; " +
				"a filter opens the groups with hits. A click on an entry opens it at once.",
			textNoteListTicks: "Typing filters. {toggle} ticks the marked row, a group or the row of all; a click on a box does the same. " +
				"The rows under the row of all tick by a trait across the groups; Thyme stays out of the pot row. {back} clears the filter.",

			textListTickRecent: "Recently watered",
			textListTickPot:    "In a pot",

			textListEmpty:     "No plants here; {back} clears the filter.",
			textListBarGroup:  "Group",
			textListBarNone:   "none",
			textListBarPlace:  "by place",
			textListBarWater:  "Watered",
			textListBarAny:    "any time",
			textListBarRecent: "recently",
			textListOpened:    "Opened %s",
			textListTicked:    "Ticked: %d",

			textAreaForms:     "Forms",
			textTabFormFields: "Fields",
			textTabFormDates:  "Dates",
			textTabFormPaths:  "Paths",
			textKeyReset:      "reset",

			textNoteFormFields: "A bed to add: {enter} goes on, opens the values of a choice, and {toggle} ticks an extra. {save} checks the form " +
				"and reports what is missing; the name is required. The code is a masked field: what is typed never shows.",
			textNoteFormDates: "{enter}, or a click on the calendar symbol, opens the month: {left} and {right} move a day, {up} and {down} a week, " +
				"{page.up} and {page.down} a month, {first} goes to today. The planting day is required; a harvest has a time.",
			textNoteFormPaths: "Type a path: the entries of the level show below the field, the known folders first. {up} and {down} mark an entry, " +
				"{zone.next} takes it, and with a * the field searches over several levels. The entries come from the folders of this machine.",

			textFormBedTitle:         "Add a bed",
			textFormDatesTitle:       "Planting dates",
			textFormPathsTitle:       "Garden paths",
			textFormFolder:           "Folder",
			textFormFolderHint:       "Type a path: the entries of the level show below. Add * to search, as in ~/*notes.",
			textFormFile:             "File",
			textFormFileHint:         "Folders to go on in and files; a * searches below the folder typed before it.",
			textFormPathsSaved:       "Paths taken: folder %s, file %s",
			textFormSave:             "Save",
			textFormReset:            "Reset",
			textFormName:             "Name",
			textFormNameHint:         "Required, for example Herb corner.",
			textFormNote:             "Note",
			textFormPlace:            "Place",
			textFormPlaceEast:        "East side",
			textFormPlaceSouth:       "South side",
			textFormPlaceNorth:       "North side",
			textFormExtras:           "Extras",
			textFormExtraMulch:       "Mulch",
			textFormExtraNet:         "Net",
			textFormExtraFrame:       "Frame",
			textFormCode:             "Shed code",
			textFormCodeHint:         "An invented code; it is never shown.",
			textFormPlanted:          "Planting day",
			textFormPlantedHint:      "Required, as YYYY-MM-DD, or pick it in the calendar.",
			textFormHarvest:          "Harvest",
			textFormHarvestHint:      "Optional, as YYYY-MM-DD HH:MM, after the planting day.",
			textFormNeedName:         "The name is empty.",
			textFormNeedNameNext:     "Type a name for the bed.",
			textFormNeedDay:          "The planting day is empty.",
			textFormNeedDayNext:      "Type a day or pick one in the calendar.",
			textFormHarvestEarly:     "The harvest is before the planting day.",
			textFormHarvestEarlyNext: "Pick a later time.",
			textFormBedSaved:         "Added bed %s with %d extras",
			textFormDatesSaved:       "Planting on %s saved",
			textFormRefused:          "Not saved: see the form",
			textFormCleared:          "Form cleared",

			textAreaPickers:     "Pickers",
			textTabPickChoose:   "Choice",
			textTabPickFiles:    "Files",
			textTabPickSuggest:  "Suggestions",
			textKeyPick:         "choose bed",
			textKeySuggest:      "enter path",
			textNotePickChoose:  "{pick} opens the list of beds over the screen: typing filters, {up} and {down} move, {enter} or a click on the marked row takes it, {back} closes.",
			textNotePickFiles:   "Folders and files of a sample folder that the program wrote for this run and removes at the end. {enter} opens a folder or takes a file, {back} goes up, {take} takes the marked file.",
			textNotePickSuggest: "{suggest} opens an input that offers sample paths while you type: {up} and {down} mark one, {zone.next} completes it, {enter} takes the marked one; a folder keeps the input open.",
			textNone:            "none yet",

			textPickTitle: "Choose a bed",
			textPickBed:   "Bed: %s",
			textPickDone:  "Bed %s chosen",

			textFileRoot:      "sample folder",
			textFileTake:      "choose file",
			textFileChosen:    "File: %s",
			textFileChosenMsg: "File %s chosen",
			textFileNeedFile:  "Mark a file, not a folder.",

			textSuggestTitle:    "Enter a path",
			textSuggestQuestion: "Path in the sample folder:",
			textSuggestEmpty:    "The path must not be empty.",
			textSuggestDone:     "Path %s taken",
			textSuggestPath:     "Path: %s",

			textAreaViews:   "Views",
			textTabViewText: "Text",
			textTabViewLog:  "Log",
			textTabViewChat: "Chat",

			textNoteViewText: "A read-only text with syntax colours. {search} searches, {log.next} and {log.prev} jump between matches, " +
				"{text.numbers} switches the line numbers, {log.wrap} the wrapping. The secret is masked: {text.reveal} asks before it shows it, " +
				"and a second press hides it again.",
			textNoteViewLog: "Lines over time with a fixed clock. {log.follow} follows the newest line, {search} searches, {log.wrap} wraps, " +
				"{log.time} shows the time column. {log.filter} opens the period: all, the last n units or a range with dates and calendar.",
			textNoteViewChat: "Messages above a field for several lines: {enter} adds a line break, {zone.next} leads to the send button, " +
				"where {enter} sends; {chat.thanks} and {chat.later} answer the selected message, {enter} on a message unfolds its details.",

			textRevealTitle:       "Show the secret",
			textRevealQuestion:    "Show the masked secret?",
			textRevealConsequence: "It stays on the screen until you hide it again.",
			textRevealDone:        "Secret shown",

			textLogReloaded: "Period applied: %d of %d lines",

			textChatEmpty:      "No messages yet; type one and send it.",
			textChatThanks:     "thanks",
			textChatLater:      "later",
			textChatThanksText: "Thanks, that helps.",
			textChatLaterText:  "Later, please.",
			textChatYou:        "You",
			textChatDelivered:  "delivered",
			textChatAnswer:     "Noted: %s",
			textChatSent:       "Message sent",
		},
		i18n.German: {
			textProduct: "Kit-Werkbank",

			textKeyArea:         "Bereich öffnen",
			textKeyZone:         "Fokus wechseln",
			textKeyTheme:        "Theme wechseln",
			textKeyThemeBack:    "Theme zurück wechseln",
			textThemeTerminal:   "terminal",
			textThemeDark:       "demo-dark",
			textThemeLight:      "demo-light",
			textIconsUnicode:    "unicode",
			textIconsNerdFont:   "nerdfont",
			textIconsNone:       "none",
			textLanguageEnglish: "english",
			textLanguageGerman:  "deutsch",
			textLanguageSystem:  "System",
			textKeyLanguage:     "Sprache wechseln",
			textKeyIcons:        "Icons wechseln",
			textKeyMouse:        "Maus umschalten",
			textKeyCount:        "zählen",
			textKeyNavigation:   "Navigation wechseln",
			textSettingNav:      "Navigation",
			textNavTabs:         "Reiter",
			textNavPathLine:     "Pfadzeile",
			textNavHeader:       "Pfad in der Kopfzeile",
			textNavChoice:       "Auswahl in der Kopfzeile",

			textHintStep: "wechseln",
			textHintZone: "Fokus",
			textHintOpen: "öffnen",

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

			textSettingsLookBody:  "Theme, Icons und Navigation wechseln sofort. Wähle einen Wert mit links und rechts oder per Klick, oder drücke die Taste eines Schalters.",
			textSettingsInputBody: "Sprache und Maus wechseln sofort. Wähle einen Wert mit links und rechts oder per Klick, oder drücke die Taste eines Schalters. Ist die Maus aus, kommt sie nur mit M zurück.",

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

			textHelpNote: "Erfundene Hilfeseiten.",

			textExplainTitle: "Gartentagebuch",
			textExplainBody:  "Die Beispieldaten dieser Werkbank sind ein erfundenes Tagebuch über Kräuter und Gemüse in einigen Beeten und Töpfen. Kein echter Garten steht dahinter.",
			textTermTitle:    "Hochbeet",
			textTermText:     "Ein Hochbeet ist ein Gartenbeet, das über dem Boden gebaut ist. Es ist ein Wort der Beispieldaten, nicht des Kits.",

			textCmdOpen: "Reiter %s öffnen",

			textChanged:  "%s: %s",
			textTooSmall: "Terminal zu klein",

			textAreaLists:     "Listen",
			textTabListFilter: "Filter",
			textTabListGroups: "Gruppen",
			textTabListTicks:  "Ankreuzen",

			textNoteListFilter: "{search} tippt einen Filter, {filter} geht zur Filterleiste: {left} und {right} ändern einen Wert, " +
				"{enter} öffnet seine Werte. {enter} oder ein Klick auf die markierte Zeile öffnet sie.",
			textNoteListGroups: "{left} und {right} falten eine Gruppe, ebenso {enter} und ein Klick auf ihre Zeile. {search} tippt einen Filter; " +
				"ein Filter öffnet die Gruppen mit Treffern. Ein Klick auf einen Eintrag öffnet ihn sofort.",
			textNoteListTicks: "Tippen filtert. {toggle} kreuzt die markierte Zeile, eine Gruppe oder die Zeile für alle an; ein Klick auf ein Kästchen tut dasselbe. " +
				"Die Zeilen unter der Zeile für alle kreuzen nach einem Merkmal quer durch die Gruppen an, die Topfzeile lässt Thyme aus. {back} löscht den Filter.",

			textListTickRecent: "Zuletzt gegossen",
			textListTickPot:    "Im Topf",

			textListEmpty:     "Hier gibt es keine Pflanzen; {back} löscht den Filter.",
			textListBarGroup:  "Gruppe",
			textListBarNone:   "keine",
			textListBarPlace:  "nach Ort",
			textListBarWater:  "Gegossen",
			textListBarAny:    "jederzeit",
			textListBarRecent: "zuletzt",
			textListOpened:    "Geöffnet: %s",
			textListTicked:    "Angekreuzt: %d",

			textAreaForms:     "Formulare",
			textTabFormFields: "Felder",
			textTabFormDates:  "Datum",
			textTabFormPaths:  "Pfade",
			textKeyReset:      "zurücksetzen",

			textNoteFormFields: "Ein Beet anlegen: {enter} geht weiter und öffnet die Werte eines Wahlfelds, {toggle} kreuzt ein Extra an. " +
				"{save} prüft das Formular und meldet, was fehlt; der Name ist Pflicht. Der Code ist ein maskiertes Feld: Getipptes erscheint nie.",
			textNoteFormDates: "{enter} oder ein Klick auf das Kalendersymbol öffnet den Monat: {left} und {right} bewegen einen Tag, {up} und {down} eine Woche, " +
				"{page.up} und {page.down} einen Monat, {first} geht zu heute. Der Pflanztag ist Pflicht; die Ernte hat eine Uhrzeit.",
			textNoteFormPaths: "Einen Pfad tippen: unter dem Feld stehen die Einträge der Ebene, zuerst die bekannten Ordner. {up} und {down} markieren einen Eintrag, " +
				"{zone.next} übernimmt ihn, und mit einem * sucht das Feld über mehrere Ebenen. Die Einträge stammen aus den Ordnern dieses Rechners.",

			textFormBedTitle:         "Beet anlegen",
			textFormDatesTitle:       "Pflanzdaten",
			textFormPathsTitle:       "Gartenpfade",
			textFormFolder:           "Ordner",
			textFormFolderHint:       "Pfad tippen: darunter stehen die Einträge der Ebene. Mit * wird gesucht, etwa ~/*notizen.",
			textFormFile:             "Datei",
			textFormFileHint:         "Ordner zum Weitergehen und Dateien; ein * sucht unterhalb des davor getippten Ordners.",
			textFormPathsSaved:       "Pfade übernommen: Ordner %s, Datei %s",
			textFormSave:             "Speichern",
			textFormReset:            "Zurücksetzen",
			textFormName:             "Name",
			textFormNameHint:         "Pflicht, zum Beispiel Kräuterecke.",
			textFormNote:             "Notiz",
			textFormPlace:            "Lage",
			textFormPlaceEast:        "Ostseite",
			textFormPlaceSouth:       "Südseite",
			textFormPlaceNorth:       "Nordseite",
			textFormExtras:           "Extras",
			textFormExtraMulch:       "Mulch",
			textFormExtraNet:         "Netz",
			textFormExtraFrame:       "Rahmen",
			textFormCode:             "Schuppencode",
			textFormCodeHint:         "Ein erfundener Code; er wird nie gezeigt.",
			textFormPlanted:          "Pflanztag",
			textFormPlantedHint:      "Pflicht, als JJJJ-MM-TT, oder im Kalender wählen.",
			textFormHarvest:          "Ernte",
			textFormHarvestHint:      "Optional, als JJJJ-MM-TT HH:MM, nach dem Pflanztag.",
			textFormNeedName:         "Der Name ist leer.",
			textFormNeedNameNext:     "Gib dem Beet einen Namen.",
			textFormNeedDay:          "Der Pflanztag ist leer.",
			textFormNeedDayNext:      "Tippe einen Tag oder wähle ihn im Kalender.",
			textFormHarvestEarly:     "Die Ernte liegt vor dem Pflanztag.",
			textFormHarvestEarlyNext: "Wähle einen späteren Zeitpunkt.",
			textFormBedSaved:         "Beet %s mit %d Extras angelegt",
			textFormDatesSaved:       "Pflanzung am %s gespeichert",
			textFormRefused:          "Nicht gespeichert: siehe Formular",
			textFormCleared:          "Formular geleert",

			textAreaPickers:     "Picker",
			textTabPickChoose:   "Auswahl",
			textTabPickFiles:    "Dateien",
			textTabPickSuggest:  "Vorschlag",
			textKeyPick:         "Beet wählen",
			textKeySuggest:      "Pfad eingeben",
			textNotePickChoose:  "{pick} öffnet die Liste der Beete über dem Bild: Tippen filtert, {up} und {down} bewegen, {enter} oder ein Klick auf die markierte Zeile übernimmt sie, {back} schließt.",
			textNotePickFiles:   "Ordner und Dateien eines Beispielordners, den das Programm für diesen Lauf schreibt und am Ende entfernt. {enter} öffnet einen Ordner oder wählt eine Datei, {back} geht hoch, {take} wählt die markierte Datei.",
			textNotePickSuggest: "{suggest} öffnet eine Eingabe, die beim Tippen Beispielpfade anbietet: {up} und {down} markieren einen, {zone.next} vervollständigt ihn, {enter} übernimmt den markierten; ein Ordner hält die Eingabe offen.",
			textNone:            "noch keiner",

			textPickTitle: "Beet wählen",
			textPickBed:   "Beet: %s",
			textPickDone:  "Beet %s gewählt",

			textFileRoot:      "Beispielordner",
			textFileTake:      "Datei wählen",
			textFileChosen:    "Datei: %s",
			textFileChosenMsg: "Datei %s gewählt",
			textFileNeedFile:  "Markiere eine Datei, keinen Ordner.",

			textSuggestTitle:    "Pfad eingeben",
			textSuggestQuestion: "Pfad im Beispielordner:",
			textSuggestEmpty:    "Der Pfad darf nicht leer sein.",
			textSuggestDone:     "Pfad %s übernommen",
			textSuggestPath:     "Pfad: %s",

			textAreaViews:   "Ansichten",
			textTabViewText: "Text",
			textTabViewLog:  "Protokoll",
			textTabViewChat: "Chat",

			textNoteViewText: "Ein Text zum Lesen mit Syntaxfarben. {search} sucht, {log.next} und {log.prev} springen zwischen Treffern, " +
				"{text.numbers} schaltet die Zeilennummern um, {log.wrap} den Umbruch. Das Geheimnis ist maskiert: {text.reveal} fragt, bevor es zeigt, " +
				"ein zweiter Druck verbirgt es wieder.",
			textNoteViewLog: "Zeilen über die Zeit mit fester Uhr. {log.follow} folgt der neuesten Zeile, {search} sucht, {log.wrap} bricht um, " +
				"{log.time} zeigt die Zeitspalte. {log.filter} öffnet den Zeitraum: alle, die letzten n Einheiten oder ein Bereich mit Datum und Kalender.",
			textNoteViewChat: "Nachrichten über einem Feld für mehrere Zeilen: {enter} macht einen Zeilenumbruch, {zone.next} führt zur Sendetaste, " +
				"dort sendet {enter}; {chat.thanks} und {chat.later} antworten auf die markierte Nachricht, {enter} auf einer Nachricht klappt ihre Details auf.",

			textRevealTitle:       "Geheimnis zeigen",
			textRevealQuestion:    "Das maskierte Geheimnis zeigen?",
			textRevealConsequence: "Es bleibt auf dem Bildschirm, bis du es wieder verbirgst.",
			textRevealDone:        "Geheimnis gezeigt",

			textLogReloaded: "Zeitraum übernommen: %d von %d Zeilen",

			textChatEmpty:      "Noch keine Nachrichten; schreibe eine und sende sie.",
			textChatThanks:     "danke",
			textChatLater:      "später",
			textChatThanksText: "Danke, das hilft.",
			textChatLaterText:  "Später bitte.",
			textChatYou:        "Du",
			textChatDelivered:  "zugestellt",
			textChatAnswer:     "Notiert: %s",
			textChatSent:       "Nachricht gesendet",
		},
	}
}
