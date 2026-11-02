//go:build linux

package localipc

import (
	"context"
	"errors"
	"net"
	"os"
	"os/user"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

func testServiceEndpoint(t *testing.T) ServiceEndpoint {
	t.Helper()
	me, err := user.Current()
	if err != nil {
		t.Skipf("current user unknown: %v", err)
	}
	group, err := user.LookupGroupId(strconv.Itoa(os.Getgid()))
	if err != nil {
		t.Skipf("primary group unknown: %v", err)
	}
	dir, err := os.MkdirTemp("/tmp", "svc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, gid, _ := ownerOf(info); gid != uint32(os.Getgid()) {
		t.Skip("temporary directory does not get the primary group")
	}
	return ServiceEndpoint{Dir: dir, SocketBase: "control", ServiceUser: me.Username, Group: group.Name,
		ErrorName: "example"}
}

func TestServiceChannel(t *testing.T) {
	e := testServiceEndpoint(t)
	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	path, err := e.Path()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil || info.Mode().Perm() != serviceSocketMode {
		t.Fatalf("socket = %v, %v", info, err)
	}
	accepted := make(chan error, 1)
	go func() {
		c, err := l.Accept()
		if c != nil {
			if uid, uidErr := PeerUID(c); uidErr != nil || uid != uint32(os.Getuid()) {
				err = errors.Join(err, uidErr, errors.New("wrong peer user"))
			}
			_ = c.Close()
		}
		accepted <- err
	}()
	conn, err := e.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := <-accepted; err != nil {
		t.Fatal(err)
	}
	if _, err := e.ListenWith(ListenOptions{Successor: true}); !errors.Is(err, errors.ErrUnsupported) {
		t.Fatalf("service successor = %v", err)
	}
	if _, err := e.Listen(); !errors.Is(err, ErrRunning) {
		t.Fatalf("second service listener = %v", err)
	}
}

func TestServiceRejectsInvalidNamesAndOpenDirectory(t *testing.T) {
	e := testServiceEndpoint(t)
	for name, bad := range map[string]ServiceEndpoint{
		"relative dir": {Dir: "run/example", SocketBase: "control", ServiceUser: "example", Group: "example"},
		"no group":     {Dir: e.Dir, SocketBase: "control", ServiceUser: "example"},
		"no user":      {Dir: e.Dir, SocketBase: "control", Group: "example"},
		"base":         {Dir: e.Dir, SocketBase: "a/b", ServiceUser: "example", Group: "example"},
	} {
		if _, err := bad.Path(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if err := checkServiceDir(e.Dir, uint32(os.Getgid())+1); !errors.Is(err, ErrRefused) {
		t.Fatalf("directory of another group accepted: %v", err)
	}
	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := os.Chmod(e.Dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := e.Dial(context.Background()); !errors.Is(err, ErrRefused) {
		t.Fatalf("client accepted a directory open to others: %v", err)
	}
	_ = l.Close()
	if _, err := e.Listen(); !errors.Is(err, ErrRefused) {
		t.Fatalf("service listened in a directory open to others: %v", err)
	}
}

func TestServiceClientRejectsForeignServer(t *testing.T) {
	e := testServiceEndpoint(t)
	l, err := e.Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		if c, err := l.Accept(); err == nil {
			_ = c.Close()
		}
	}()
	path, _ := e.Path()
	stranger := []uint32{uint32(os.Getuid()) + 1}
	if err := checkServiceFiles(path, stranger); !errors.Is(err, ErrRefused) {
		t.Fatalf("socket of a stranger accepted: %v", err)
	}
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if pid, err := checkServer(conn, stranger); !errors.Is(err, ErrRefused) || pid != os.Getpid() {
		t.Fatalf("server of a stranger accepted: %d, %v", pid, err)
	}
}

func TestServiceAdmission(t *testing.T) {
	const group = 4242
	member := func(int32, uint32, uint32) ([]uint32, error) { return []uint32{7, group}, nil }
	outsider := func(int32, uint32, uint32) ([]uint32, error) { return []uint32{7}, nil }
	unreadable := func(int32, uint32, uint32) ([]uint32, error) { return nil, errors.New("hidden") }
	for name, tc := range map[string]struct {
		cred   syscall.Ucred
		groups groupsReader
		ok     bool
	}{
		"root":                {syscall.Ucred{Pid: 10, Uid: 0, Gid: 0}, unreadable, true},
		"primary group":       {syscall.Ucred{Pid: 10, Uid: 1000, Gid: group}, unreadable, true},
		"supplementary group": {syscall.Ucred{Pid: 10, Uid: 1000, Gid: 1000}, member, true},
		"outsider":            {syscall.Ucred{Pid: 10, Uid: 1000, Gid: 1000}, outsider, false},
		"unreadable groups":   {syscall.Ucred{Pid: 10, Uid: 1000, Gid: 1000}, unreadable, false},
		"unknown process":     {syscall.Ucred{Pid: 0, Uid: 1000, Gid: 1000}, member, false},
	} {
		err := admit(&tc.cred, group, tc.groups)
		if tc.ok && err != nil || !tc.ok && !errors.Is(err, ErrRefused) {
			t.Errorf("%s: admit() = %v", name, err)
		}
	}
}

func TestStatusGroups(t *testing.T) {
	const status = "Name:\tapp\nUid:\t1000\t1001\t1001\t1001\nGid:\t50\t51\t51\t51\nGroups:\t7 51 4242 \n"
	groups, err := statusGroups(strings.NewReader(status), 1001, 51)
	if err != nil || len(groups) != 3 || groups[2] != 4242 {
		t.Fatalf("statusGroups() = %v, %v", groups, err)
	}
	for name, tc := range map[string]struct {
		status   string
		uid, gid uint32
	}{
		"reused uid":    {status, 1000, 51},
		"reused gid":    {status, 1001, 50},
		"malformed":     {"Uid:\t1 1 1 1\nGid:\t1 1 1 1\nGroups:\t7 x\n", 1, 1},
		"no group list": {"Uid:\t1 1 1 1\nGid:\t1 1 1 1\n", 1, 1},
	} {
		if _, err := statusGroups(strings.NewReader(tc.status), tc.uid, tc.gid); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if groups, err := procGroups(int32(os.Getpid()), uint32(os.Geteuid()), uint32(os.Getegid())); err != nil {
		t.Fatalf("own groups = %v, %v", groups, err)
	}
}
