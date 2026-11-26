package projectpath

// excludedDirs lists the directories a search never enters: they hold kernel and runtime state, not files.
func excludedDirs() []string { return []string{"/proc", "/sys", "/dev", "/run"} }
