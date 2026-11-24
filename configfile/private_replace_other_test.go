//go:build !windows

package configfile

import "testing"

// Permissions are mode bits here, which the shared tests check.
func widenACL(*testing.T, string)         {}
func assertPrivateACL(*testing.T, string) {}
func assertWideACL(*testing.T, string)    {}
