package tui

// The IDs of the actions StandardActions defines.
const (
	ActionSave      ActionID = "save"
	ActionHelp      ActionID = "help"
	ActionHelpF1    ActionID = "help.f1"
	ActionCommand   ActionID = "command"
	ActionLock      ActionID = "lock"
	ActionBack      ActionID = "back"
	ActionForceQuit ActionID = "forcequit"
	ActionSearch    ActionID = "search"
	ActionInfo      ActionID = "info"
	ActionMenu      ActionID = "menu"
	ActionQuit      ActionID = "quit"
	ActionCopy      ActionID = "copy"
	ActionClone     ActionID = "clone"
	ActionDelete    ActionID = "delete"
	ActionNew       ActionID = "new"
	ActionEdit      ActionID = "edit"
	ActionRename    ActionID = "rename"
	ActionFilter    ActionID = "filter"
	ActionReload    ActionID = "reload"
	ActionUpdate    ActionID = "update"
)

// StandardActions returns the standard actions, each with one default key, so that every tool feels the
// same. An application adds them to its table with append(StandardActions(), own...); it may leave some out,
// but no other action may hold their keys, see NewKeymap. ActionHelpF1 is the second way of ActionHelp, so a
// table that leaves out the help leaves it out too. Every call returns a new copy.
func StandardActions() []Action {
	return []Action{
		{ID: ActionSave, Level: LevelGlobal, DefaultKeys: []string{"f2"}, Help: TextKeySave, Rebind: RebindReplace},
		{ID: ActionHelp, Level: LevelGlobal, DefaultKeys: []string{"?"}, Help: TextKeyHelp, Rebind: RebindReplace},
		// f1 reaches the help from a text field, where "?" types a character; it is a separate action because
		// a replaceable action has exactly one key, and it is not rebindable.
		{ID: ActionHelpF1, Level: LevelGlobal, DefaultKeys: []string{"f1"}, Help: TextKeyHelp, SecondWayOf: ActionHelp},
		{ID: ActionCommand, Level: LevelGlobal, DefaultKeys: []string{":"}, Help: TextKeyCommands, Rebind: RebindReplace},
		{ID: ActionLock, Level: LevelGlobal, DefaultKeys: []string{"ctrl+l"}, Help: TextKeyLock, Rebind: RebindReplace},
		{ID: ActionBack, Level: LevelGlobal, DefaultKeys: []string{"esc"}, Help: TextKeyBack},
		{ID: ActionForceQuit, Level: LevelGlobal, DefaultKeys: []string{"ctrl+c"}, Help: TextKeyQuit},
		{ID: ActionSearch, Level: LevelLetter, DefaultKeys: []string{"/"}, Help: TextKeySearch},
		{ID: ActionInfo, Level: LevelLetter, DefaultKeys: []string{"i"}, Help: TextKeyInfo, Rebind: RebindExtra},
		{ID: ActionMenu, Level: LevelLetter, DefaultKeys: []string{"m"}, Help: TextKeyMenu},
		{ID: ActionQuit, Level: LevelLetter, DefaultKeys: []string{"q"}, Help: TextKeyQuit},
		{ID: ActionCopy, Level: LevelLetter, DefaultKeys: []string{"c"}, Help: TextKeyCopy},
		{ID: ActionClone, Level: LevelLetter, DefaultKeys: []string{"C"}, Help: TextKeyClone},
		{ID: ActionDelete, Level: LevelLetter, DefaultKeys: []string{"d"}, Help: TextKeyDelete},
		{ID: ActionNew, Level: LevelLetter, DefaultKeys: []string{"n"}, Help: TextKeyNew},
		{ID: ActionEdit, Level: LevelLetter, DefaultKeys: []string{"e"}, Help: TextKeyEdit},
		{ID: ActionRename, Level: LevelLetter, DefaultKeys: []string{"r"}, Help: TextKeyRename},
		{ID: ActionFilter, Level: LevelLetter, DefaultKeys: []string{"f"}, Help: TextKeyFilter},
		{ID: ActionReload, Level: LevelLetter, DefaultKeys: []string{"R"}, Help: TextKeyReload},
		{ID: ActionUpdate, Level: LevelLetter, DefaultKeys: []string{"U"}, Help: TextKeyUpdate},
	}
}
