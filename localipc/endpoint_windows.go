//go:build windows

package localipc

import (
	"fmt"
	"strings"
	"unicode/utf16"
)

const pipeNamespace = `\\.\pipe\`

func endpointPath(e Endpoint) (string, error) {
	if !pipeComponent(e.PipePrefix) || !pipeComponent(e.SocketBase) {
		return "", fmt.Errorf("%s: pipe prefix or socket base is invalid", e.label())
	}
	sid, err := currentUserSID()
	if err != nil {
		return "", fmt.Errorf("%s: %w", e.label(), err)
	}
	path := pipeNamespace + e.PipePrefix + sid.String() + "-" + e.SocketBase
	if len(utf16.Encode([]rune(path))) >= 256 {
		return "", fmt.Errorf("%s: pipe name is too long", e.label())
	}
	return path, nil
}

func pipeComponent(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `\/:*?"<>|`) && !strings.ContainsRune(s, 0)
}
