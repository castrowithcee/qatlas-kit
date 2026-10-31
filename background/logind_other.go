//go:build !linux

package background

// LogoutBehavior is LogoutUnknown here: only Linux with systemd-logind is inspected.
func LogoutBehavior() LogoutOutcome { return LogoutUnknown }
