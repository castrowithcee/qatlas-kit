package background

// LogoutOutcome says whether a process survives the end of the login session that started it.
type LogoutOutcome int

const (
	// LogoutUnknown means the platform or its configuration does not tell.
	LogoutUnknown LogoutOutcome = iota
	// LogoutKills means the system ends the user's processes when the last session closes.
	LogoutKills
	// LogoutSpares means the system leaves a detached process running at logout.
	LogoutSpares
)

func (o LogoutOutcome) String() string {
	switch o {
	case LogoutKills:
		return "kills"
	case LogoutSpares:
		return "spares"
	}
	return "unknown"
}
