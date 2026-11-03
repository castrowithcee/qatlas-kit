package docker

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-kit/output"
)

// Limits on what is read from the Engine and on how many containers List inspects.
const (
	MaxContainers   = 1024
	listLimit       = 8 << 20
	inspectLimit    = 2 << 20
	labelProject    = "com.docker.compose.project"
	labelWorkingDir = "com.docker.compose.project.working_dir"
	labelConfig     = "com.docker.compose.project.config_files"
)

// Container is one container as List reports it. All text comes from the Engine and its images and is not
// trustworthy: a caller masks it before it shows it.
type Container struct {
	ID    string
	Name  string // without the leading slash
	Image string
	// State is the Engine's state word, such as running or exited.
	State string
	// Status is the Engine's human readable text; empty in Details.
	Status string
	// Health is empty without a health check, otherwise starting, healthy or unhealthy.
	Health       string
	StartedAt    time.Time // zero if the container never started
	RestartCount int
	Labels       map[string]string
}

// Port is a port of a container; PublicPort and HostIP are zero or empty if it is not published.
type Port struct {
	PrivatePort int
	PublicPort  int
	Protocol    string
	HostIP      string
}

// Mount is a mount of a container. Type is volume, bind, tmpfs or another type the Engine reports.
type Mount struct {
	Type        string
	Name        string // volume name, if any
	Source      string
	Destination string
	ReadOnly    bool
}

// Network is a network a container is attached to.
type Network struct {
	Name      string
	IPAddress string
}

// Details is a Container with what Inspect adds. It holds the names of the environment variables but has no
// place for their values.
type Details struct {
	Container
	Ports    []Port
	Mounts   []Mount
	Networks []Network
	EnvNames []string
}

// envName keeps only the name of an "NAME=value" entry; the value is dropped while decoding.
type envName string

var errEnvEntry = errors.New("docker: invalid environment entry")

func (n *envName) UnmarshalJSON(data []byte) error {
	var entry string
	if err := json.Unmarshal(data, &entry); err != nil {
		return errEnvEntry
	}
	name, _, _ := strings.Cut(entry, "=")
	*n = envName(name)
	return nil
}

type listItem struct {
	ID     string            `json:"Id"`
	Names  []string          `json:"Names"`
	Image  string            `json:"Image"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
}

type inspectAnswer struct {
	ID           string `json:"Id"`
	Name         string `json:"Name"`
	Image        string `json:"Image"`
	RestartCount int    `json:"RestartCount"`
	State        struct {
		Status    string `json:"Status"`
		StartedAt string `json:"StartedAt"`
		Health    *struct {
			Status string `json:"Status"`
		} `json:"Health"`
	} `json:"State"`
	Config struct {
		Image  string            `json:"Image"`
		Env    []envName         `json:"Env"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	Mounts []struct {
		Type        string `json:"Type"`
		Name        string `json:"Name"`
		Source      string `json:"Source"`
		Destination string `json:"Destination"`
		RW          *bool  `json:"RW"`
	} `json:"Mounts"`
	NetworkSettings struct {
		Ports map[string][]struct {
			HostIP   string `json:"HostIp"`
			HostPort string `json:"HostPort"`
		} `json:"Ports"`
		Networks map[string]struct {
			IPAddress string `json:"IPAddress"`
		} `json:"Networks"`
	} `json:"NetworkSettings"`
}

func (a *inspectAnswer) details() Details {
	d := Details{Container: Container{
		ID:           a.ID,
		Name:         strings.TrimPrefix(a.Name, "/"),
		Image:        a.Config.Image,
		State:        a.State.Status,
		RestartCount: a.RestartCount,
		Labels:       a.Config.Labels,
	}}
	if d.Image == "" {
		d.Image = a.Image
	}
	if a.State.Health != nil && a.State.Health.Status != "none" {
		d.Health = a.State.Health.Status
	}
	if t, err := time.Parse(time.RFC3339Nano, a.State.StartedAt); err == nil {
		d.StartedAt = t
	}
	for _, m := range a.Mounts {
		d.Mounts = append(d.Mounts, Mount{Type: m.Type, Name: m.Name, Source: m.Source,
			Destination: m.Destination, ReadOnly: m.RW != nil && !*m.RW})
	}
	for spec, bindings := range a.NetworkSettings.Ports {
		number, protocol, _ := strings.Cut(spec, "/")
		private, err := strconv.Atoi(number)
		if err != nil {
			continue
		}
		if len(bindings) == 0 {
			d.Ports = append(d.Ports, Port{PrivatePort: private, Protocol: protocol})
		}
		for _, b := range bindings {
			public, _ := strconv.Atoi(b.HostPort)
			d.Ports = append(d.Ports, Port{PrivatePort: private, PublicPort: public, Protocol: protocol, HostIP: b.HostIP})
		}
	}
	sort.Slice(d.Ports, func(i, j int) bool {
		a, b := d.Ports[i], d.Ports[j]
		if a.PrivatePort != b.PrivatePort {
			return a.PrivatePort < b.PrivatePort
		}
		if a.Protocol != b.Protocol {
			return a.Protocol < b.Protocol
		}
		if a.PublicPort != b.PublicPort {
			return a.PublicPort < b.PublicPort
		}
		return a.HostIP < b.HostIP
	})
	for name, n := range a.NetworkSettings.Networks {
		d.Networks = append(d.Networks, Network{Name: name, IPAddress: n.IPAddress})
	}
	sort.Slice(d.Networks, func(i, j int) bool { return d.Networks[i].Name < d.Networks[j].Name })
	for _, n := range a.Config.Env {
		if n != "" {
			d.EnvNames = append(d.EnvNames, string(n))
		}
	}
	sort.Strings(d.EnvNames)
	return d
}

// itemName picks the container's own name: the one with a single leading slash, not a link alias.
func itemName(names []string) string {
	for _, n := range names {
		if strings.HasPrefix(n, "/") && !strings.Contains(n[1:], "/") {
			return n[1:]
		}
	}
	if len(names) > 0 {
		return strings.TrimPrefix(names[0], "/")
	}
	return ""
}

func sortContainers(cs []Container) {
	sort.Slice(cs, func(i, j int) bool {
		if cs[i].Name != cs[j].Name {
			return cs[i].Name < cs[j].Name
		}
		return cs[i].ID < cs[j].ID
	})
}

func (c *Client) inspect(ctx context.Context, ref string) (inspectAnswer, error) {
	segment, err := checkRef(ref)
	if err != nil {
		return inspectAnswer{}, err
	}
	var answer inspectAnswer
	err = c.getJSON(ctx, "inspect", "/containers/"+segment+"/json", nil, inspectLimit, &answer)
	return answer, err
}

// Inspect returns the details of a container named by ID or name. Status of the result is empty.
func (c *Client) Inspect(ctx context.Context, ref string) (Details, error) {
	answer, err := c.inspect(ctx, ref)
	if err != nil {
		return Details{}, err
	}
	return answer.details(), nil
}

// listDetails lists every container, stopped ones included, and inspects each. A container that vanished
// in between is left out. More than MaxContainers is an error rather than a silent cut.
func (c *Client) listDetails(ctx context.Context) ([]Details, error) {
	var items []listItem
	if err := c.getJSON(ctx, "list", "/containers/json", url.Values{"all": {"1"}}, listLimit, &items); err != nil {
		return nil, err
	}
	if len(items) > MaxContainers {
		return nil, &Error{Code: output.CodeRuntime, Message: "the " + subject + " reports too many containers"}
	}
	out := make([]Details, 0, len(items))
	for _, item := range items {
		answer, err := c.inspect(ctx, item.ID)
		var failure *Error
		switch {
		case errors.As(err, &failure) && failure.Code == output.CodeNotFound:
			continue
		case errors.As(err, &failure) && failure.Code == output.CodeInvalidRequest:
			return nil, &Error{Code: output.CodeRuntime, Message: "the " + subject + " returned an invalid response"}
		case err != nil:
			return nil, err
		}
		d := answer.details()
		d.ID = item.ID
		d.Name = itemName(item.Names)
		d.Image = item.Image
		d.State = item.State
		d.Status = item.Status
		d.Labels = item.Labels
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// List returns all containers, also stopped ones, sorted by name. It inspects each container for its health,
// start time and restart count.
func (c *Client) List(ctx context.Context) ([]Container, error) {
	details, err := c.listDetails(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Container, len(details))
	for i, d := range details {
		out[i] = d.Container
	}
	return out, nil
}

// UsingPath returns the containers that use dir: a mount source or the Compose working directory is dir or
// lies below it. dir must be absolute.
func (c *Client) UsingPath(ctx context.Context, dir string) ([]Details, error) {
	if !path.IsAbs(dir) || strings.ContainsAny(dir, "\x00\n\r") {
		return nil, &Error{Code: output.CodeInvalidRequest, Message: "the path must be absolute"}
	}
	details, err := c.listDetails(ctx)
	if err != nil {
		return nil, err
	}
	var out []Details
	for _, d := range details {
		if d.UsesPath(dir) {
			out = append(out, d)
		}
	}
	return out, nil
}

// UsesPath reports whether a mount source or the Compose working directory of d is dir or lies below it.
// Paths are compared lexically as cleaned slash paths, with a boundary at a separator, so /srv/app does not
// include /srv/apple. Symbolic links are not resolved. A relative dir matches nothing.
func (d Details) UsesPath(dir string) bool {
	if !path.IsAbs(dir) {
		return false
	}
	dir = path.Clean(dir)
	under := func(p string) bool {
		if !path.IsAbs(p) {
			return false
		}
		p = path.Clean(p)
		return p == dir || strings.HasPrefix(p, strings.TrimSuffix(dir, "/")+"/")
	}
	if under(d.Labels[labelWorkingDir]) {
		return true
	}
	for _, m := range d.Mounts {
		if under(m.Source) {
			return true
		}
	}
	return false
}
