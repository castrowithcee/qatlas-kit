//go:build dockerintegration && (linux || darwin)

package docker

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestEngineReadOnly reads from a real Engine. The socket comes from DOCKER_TEST_SOCKET; the default
// sockets are refused so that a run never touches an Engine by accident.
func TestEngineReadOnly(t *testing.T) {
	socket := os.Getenv("DOCKER_TEST_SOCKET")
	if socket == "" {
		t.Skip("DOCKER_TEST_SOCKET is not set")
	}
	for _, standard := range []string{"/var/run/docker.sock", "/run/docker.sock"} {
		if filepath.Clean(socket) == standard {
			t.Fatalf("DOCKER_TEST_SOCKET must not be the default socket %s", standard)
		}
		if real, err := filepath.EvalSymlinks(socket); err == nil && real == standard {
			t.Fatalf("DOCKER_TEST_SOCKET must not lead to the default socket %s", standard)
		}
	}
	c, err := New(Options{Socket: socket, Timeout: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	cs, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	projects, loose := Group(cs)
	t.Logf("%d containers, %d projects, %d loose", len(cs), len(projects), len(loose))
}
