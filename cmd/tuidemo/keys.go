package main

import (
	"strconv"

	"github.com/castrowithcee/qatlas-kit/tui"
)

// The actions of the key table. A click on a target runs the same action as its key.
const (
	actQuit      tui.ActionID = tui.ActionQuit
	actForceQuit tui.ActionID = tui.ActionForceQuit
	actArea      tui.ActionID = "area"
	actLeft      tui.ActionID = "left"
	actRight     tui.ActionID = "right"
	actZoneNext  tui.ActionID = "zone.next"
	actZonePrev  tui.ActionID = "zone.prev"
	actTheme     tui.ActionID = "set.theme"
	actThemeBack tui.ActionID = "set.theme.back"
	actLanguage  tui.ActionID = "set.language"
	actIcons     tui.ActionID = "set.icons"
	actMouse     tui.ActionID = "set.mouse"
	actCount     tui.ActionID = "count"
	actNavigate  tui.ActionID = "set.navigation"
	actMenu      tui.ActionID = tui.ActionMenu

	actBack     tui.ActionID = "back"
	actEnter    tui.ActionID = "enter"
	actUp       tui.ActionID = "up"
	actDown     tui.ActionID = "down"
	actPageUp   tui.ActionID = "page.up"
	actPageDown tui.ActionID = "page.down"
	actFirst    tui.ActionID = "first"
	actLast     tui.ActionID = "last"
	actHelp     tui.ActionID = "help"
	actHelpF1   tui.ActionID = tui.ActionHelpF1
	actInfo     tui.ActionID = "info"
	actCommands tui.ActionID = tui.ActionCommand
	actAsk      tui.ActionID = "ask"
	actRename   tui.ActionID = "rename"
	actNotice   tui.ActionID = "notice"
	actYes      tui.ActionID = "answer.yes"
	actNo       tui.ActionID = "answer.no"
	actCopy     tui.ActionID = "copy"
	actSearch   tui.ActionID = tui.ActionSearch
	actFilter   tui.ActionID = tui.ActionFilter
	actToggle   tui.ActionID = "toggle"
	actReset    tui.ActionID = "reset"
	actPick     tui.ActionID = "pick"
	actSuggest  tui.ActionID = "suggest"
	actTake     tui.ActionID = "take"

	// The quick replies of the chat; the other actions of the views are the kit's own, see tui.LogViewActions.
	actChatThanks tui.ActionID = "chat.thanks"
	actChatLater  tui.ActionID = "chat.later"
)

// keyTable is the key table of the program. The digits of actArea follow the number of areas, so a new
// group needs no new key.
func keyTable(areas int) []tui.Action {
	digits := make([]string, areas)
	for i := range digits {
		digits[i] = strconv.Itoa(i + 1)
	}
	act := func(id tui.ActionID, level tui.KeyLevel, help tui.TextID, keys ...string) tui.Action {
		return tui.Action{ID: id, Level: level, DefaultKeys: keys, Help: help}
	}
	table := []tui.Action{
		act(actQuit, tui.LevelLetter, tui.TextKeyQuit, "q"),
		act(actForceQuit, tui.LevelGlobal, tui.TextKeyQuit, "ctrl+c"),
		act(actArea, tui.LevelGlobal, textKeyArea, digits...),
		act(actLeft, tui.LevelMove, tui.TextKeyLeft, "left"),
		act(actRight, tui.LevelMove, tui.TextKeyRight, "right"),
		act(actZoneNext, tui.LevelMove, textKeyZone, "tab"),
		act(actZonePrev, tui.LevelMove, textKeyZone, "shift+tab"),
		act(actTheme, tui.LevelLetter, textKeyTheme, "t"),
		act(actThemeBack, tui.LevelLetter, textKeyThemeBack, "T"),
		act(actLanguage, tui.LevelLetter, textKeyLanguage, "l"),
		act(actIcons, tui.LevelLetter, textKeyIcons, "I"),
		act(actMouse, tui.LevelLetter, textKeyMouse, "M"),
		act(actCount, tui.LevelLetter, textKeyCount, "+"),
		act(actNavigate, tui.LevelLetter, textKeyNavigation, "N"),
		act(actMenu, tui.LevelLetter, tui.TextKeyMenu, "m"),
		act(actBack, tui.LevelGlobal, tui.TextKeyBack, "esc"),
		act(actHelp, tui.LevelGlobal, tui.TextKeyHelp, "?"),
		{ID: actHelpF1, Level: tui.LevelGlobal, DefaultKeys: []string{"f1"}, Help: tui.TextKeyHelp, SecondWayOf: actHelp},
		act(actCommands, tui.LevelGlobal, tui.TextKeyCommands, ":"),
		act(actEnter, tui.LevelMove, tui.TextKeyEnter, "enter"),
		act(actUp, tui.LevelMove, tui.TextKeyUp, "up"),
		act(actDown, tui.LevelMove, tui.TextKeyDown, "down"),
		act(actPageUp, tui.LevelMove, tui.TextKeyPageUp, "pgup"),
		act(actPageDown, tui.LevelMove, tui.TextKeyPageDown, "pgdown"),
		act(actFirst, tui.LevelMove, tui.TextKeyFirst, "home"),
		act(actLast, tui.LevelMove, tui.TextKeyLast, "end"),
		act(actSearch, tui.LevelLetter, tui.TextKeySearch, "/"),
		act(actFilter, tui.LevelLetter, tui.TextKeyFilter, "f"),
		act(actToggle, tui.LevelLetter, tui.TextKeyToggle, "space"),
		act(tui.ActionSave, tui.LevelGlobal, tui.TextKeySave, "f2"),
		act(actReset, tui.LevelGlobal, textKeyReset, "ctrl+r"),
		act(actPick, tui.LevelLetter, textKeyPick, "p"),
		act(actSuggest, tui.LevelLetter, textKeySuggest, "s"),
		act(actTake, tui.LevelLetter, textFileTake, "g"),
		act(actInfo, tui.LevelLetter, tui.TextKeyInfo, "i"),
		act(actAsk, tui.LevelLetter, textKeyAsk, "a"),
		act(actRename, tui.LevelLetter, tui.TextKeyRename, "r"),
		act(actNotice, tui.LevelLetter, textKeyNotice, "o"),
		// The answers of a question and the copy key of an explanation are overlay levels, which may repeat a
		// key of the main levels.
		act(actYes, tui.LevelConfirm, tui.TextKeyYes, "y"),
		act(actNo, tui.LevelConfirm, tui.TextKeyNo, "n"),
		act(actCopy, tui.LevelHelp, tui.TextKeyCopy, "c"),
		act(actChatThanks, tui.LevelLetter, textChatThanks, "k"),
		act(actChatLater, tui.LevelLetter, textChatLater, "z"),
	}
	table = append(table, tui.TextViewActions()...)
	for _, a := range tui.LogViewActions() {
		// The default keys of the time column and the period are the workbench's theme switches.
		switch a.ID {
		case tui.ActionLogTimestamps:
			a.DefaultKeys = []string{"h"}
		case tui.ActionLogFilter:
			a.DefaultKeys = []string{"P"}
		}
		table = append(table, a)
	}
	return table
}
