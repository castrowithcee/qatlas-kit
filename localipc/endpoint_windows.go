//go:build windows

package localipc

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf16"

	"golang.org/x/sys/windows"
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

func running(e Endpoint) ([]string, error) {
	if !pipeComponent(e.PipePrefix) {
		return nil, fmt.Errorf("%s: pipe prefix is invalid", e.label())
	}
	sid, err := currentUserSID()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.label(), err)
	}
	prefix := e.PipePrefix + sid.String() + "-"
	pattern, err := windows.UTF16PtrFromString(pipeNamespace + "*")
	if err != nil {
		return nil, err
	}
	bases := []string{}
	var data windows.Win32finddata
	h, err := windows.FindFirstFile(pattern, &data)
	if err != nil {
		if errors.Is(err, windows.ERROR_FILE_NOT_FOUND) {
			return bases, nil
		}
		return nil, fmt.Errorf("%s: cannot list local pipes: %w", e.label(), err)
	}
	defer windows.FindClose(h)
	for {
		name := windows.UTF16ToString(data.FileName[:])
		// Pipe names compare case-insensitively, so the prefix must too: the
		// system treats a differently cased name as the same pipe.
		if len(name) > len(prefix) && strings.EqualFold(name[:len(prefix)], prefix) {
			if base := name[len(prefix):]; pipeComponent(base) {
				bases = append(bases, base)
			}
		}
		if err := windows.FindNextFile(h, &data); err != nil {
			if errors.Is(err, windows.ERROR_NO_MORE_FILES) {
				break
			}
			return nil, fmt.Errorf("%s: cannot list local pipes: %w", e.label(), err)
		}
	}
	slices.Sort(bases)
	return slices.Compact(bases), nil
}

func pipeComponent(s string) bool {
	return s != "" && s != "." && s != ".." && !strings.ContainsAny(s, `\/:*?"<>|`) && !strings.ContainsRune(s, 0)
}
