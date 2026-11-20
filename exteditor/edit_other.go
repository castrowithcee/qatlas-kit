//go:build !linux && !darwin

package exteditor

import (
	"os/exec"

	tea "charm.land/bubbletea/v2"
)

type session struct {
	cmd *exec.Cmd
}

func prepare(Request) (*session, error) { return nil, ErrUnsupported }

func (*session) finish(error) tea.Msg { return FailedMsg{Err: ErrUnsupported} }
