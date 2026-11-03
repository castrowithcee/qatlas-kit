package localipc

import (
	"context"
	"net"
)

// ServiceEndpoint is the channel of a system service that runs as its own
// user and serves root and the members of one group. Dir is the absolute
// directory the service manager creates for the socket; ServiceUser and Group
// are names from the system's user and group databases. It exists on Linux
// only; other platforms return an error wrapping errors.ErrUnsupported.
type ServiceEndpoint struct {
	Dir         string
	SocketBase  string
	ServiceUser string
	Group       string
	ErrorName   string
}

// Path returns the socket path without creating it.
func (e ServiceEndpoint) Path() (string, error) { return servicePath(e) }

// Listen owns the channel until the returned listener is closed. Dir must
// belong to this process's user and to Group and give others no access; the
// socket is open to the group. Accept admits root and processes whose primary
// or supplementary group is Group, and refuses every other peer.
func (e ServiceEndpoint) Listen() (net.Listener, error) { return listenService(e, ListenOptions{}) }

// ListenWith is Listen with options. A service channel has no successor and
// no CrossVersion check; a service is restarted by its service manager instead.
func (e ServiceEndpoint) ListenWith(opts ListenOptions) (net.Listener, error) {
	return listenService(e, opts)
}

// Dial connects to the service and requires that the listening process runs
// as ServiceUser or as this process's user, and that Dir and the socket belong
// to one of them and give others no access. A refusal is a *PeerError.
func (e ServiceEndpoint) Dial(ctx context.Context) (net.Conn, error) { return dialService(ctx, e) }

func (e ServiceEndpoint) label() string {
	if e.ErrorName != "" {
		return e.ErrorName
	}
	return "service channel"
}
