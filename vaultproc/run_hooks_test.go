//go:build linux || darwin || windows

package vaultproc

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"filippo.io/age"

	"github.com/castrowithcee/qatlas-kit/localipc"
)

func allZero(b []byte) bool { return len(bytes.Trim(b, "\x00")) == 0 }

func successorOptions(t *testing.T) RunOptions {
	t.Helper()
	return RunOptions{Config: testConfig(t), Successor: true}
}

func kitState(t *testing.T, s State) []byte {
	t.Helper()
	data, err := encodeState(s)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestKitFormatDoesNotCallDecodeEarlier(t *testing.T) {
	opts := successorOptions(t)
	called := false
	opts.DecodeEarlier = func([]byte) (State, error) { called = true; return State{}, errors.New("no") }
	data := kitState(t, State{Identity: newIdentity(t), Document: []byte(testDocument), LocksAt: time.Now().Add(time.Minute)})
	server, _, _, err := opts.open(data)
	if err != nil || called {
		t.Fatalf("open = %v, hook called = %v", err, called)
	}
	if !allZero(data) {
		t.Error("the bytes read were not overwritten")
	}
	server.wipe()
}

func TestForeignFormatNeedsTheHookAndSuccessor(t *testing.T) {
	id := newIdentity(t)
	foreign := func() []byte {
		return encodeForeign(t, foreignState{Identity: id.String(), Document: testDocument, LocksAt: time.Now().Add(time.Minute)})
	}
	opts := successorOptions(t)
	if _, _, _, err := opts.open(foreign()); err == nil {
		t.Fatal("a foreign format was read without a hook")
	}
	opts.DecodeEarlier = parseForeign
	data := foreign()
	server, _, _, err := opts.open(data)
	if err != nil {
		t.Fatal(err)
	}
	server.wipe()
	if !allZero(data) {
		t.Error("the bytes read were not overwritten")
	}
	opts.Successor = false
	if err := opts.validate(); err == nil {
		t.Fatal("validate accepted DecodeEarlier without Successor")
	}
}

func TestBytesAreOverwrittenAfterTheHook(t *testing.T) {
	id := newIdentity(t)
	opts := successorOptions(t)
	var seen []byte
	opts.DecodeEarlier = func(data []byte) (State, error) {
		seen = data
		st, err := parseForeign(data)
		st.Document = data[:5] // aliases the bytes the kit clears; NewServer must have copied it by then
		return st, err
	}
	data := encodeForeign(t, foreignState{Identity: id.String(), Document: testDocument, LocksAt: time.Now().Add(time.Minute)})
	server, _, _, err := opts.open(data)
	if err != nil {
		t.Fatal(err)
	}
	server.wipe()
	if len(seen) == 0 || !allZero(seen) {
		t.Error("the bytes the hook saw were not overwritten")
	}
}

func TestDecodeEarlierResultIsChecked(t *testing.T) {
	id := newIdentity(t)
	later := time.Now().Add(time.Minute)
	for name, state := range map[string]State{
		"no key":           {Document: []byte("example-secret"), LocksAt: later},
		"no lock time":     {Identity: id, Document: []byte("example-secret")},
		"negative maximum": {Identity: id, Document: []byte("example-secret"), LocksAt: later, MaxUnlock: -1},
	} {
		doc := state.Document
		opts := successorOptions(t)
		opts.DecodeEarlier = func([]byte) (State, error) { return state, nil }
		_, _, _, err := opts.open([]byte("example-secret not a state"))
		if err == nil || strings.Contains(err.Error(), "example-secret") {
			t.Errorf("%s: open = %v", name, err)
		}
		if !allZero(doc) {
			t.Errorf("%s: the rejected document was not overwritten", name)
		}
	}
	opts := successorOptions(t)
	opts.DecodeEarlier = func([]byte) (State, error) { return State{}, errors.New("example-secret failed") }
	if _, _, _, err := opts.open([]byte("x")); err == nil || strings.Contains(err.Error(), "example-secret") {
		t.Errorf("hook error: open = %v", err)
	}
}

func TestConfigureFormsTheConfigFromTheKey(t *testing.T) {
	id := newIdentity(t)
	opts := RunOptions{Config: Config{Endpoint: testEndpoint(t)}}
	if err := opts.validate(); err == nil {
		t.Fatal("an incomplete Config passed without Configure")
	}
	var got *age.X25519Identity
	opts.Configure = func(key *age.X25519Identity, base Config, _ *localipc.Endpoint) (Config, *localipc.Endpoint, error) {
		got = key
		base.Version, base.ProofLabel, base.IdleTimeout, base.Handlers = 1, testLabel, 7*time.Minute, testHandlers()
		return base, nil, nil
	}
	if err := opts.validate(); err != nil {
		t.Fatal(err)
	}
	server, cfg, _, err := opts.open(kitState(t, State{Identity: id, Document: []byte(testDocument)}))
	if err != nil {
		t.Fatal(err)
	}
	defer server.wipe()
	if got == nil || got.String() != id.String() || cfg.IdleTimeout != 7*time.Minute || server.cfg.IdleTimeout != 7*time.Minute {
		t.Fatalf("Configure got %v, config %+v", got, cfg)
	}
}

func TestConfigureResultIsValidatedInFull(t *testing.T) {
	id := newIdentity(t)
	for name, change := range map[string]func(*Config){
		"no label":         func(c *Config) { c.ProofLabel = "" },
		"no version":       func(c *Config) { c.Version = 0 },
		"no idle timeout":  func(c *Config) { c.IdleTimeout = 0 },
		"negative maximum": func(c *Config) { c.MaxUnlock = -time.Second },
		"reserved name":    func(c *Config) { c.Handlers[OpLock] = c.Handlers["get"] },
		"nil handler":      func(c *Config) { c.Handlers["none"] = nil },
	} {
		opts := RunOptions{Config: testConfig(t)}
		opts.Configure = func(_ *age.X25519Identity, base Config, _ *localipc.Endpoint) (Config, *localipc.Endpoint, error) {
			base.Handlers = testHandlers()
			change(&base)
			return base, nil, nil
		}
		doc := []byte(testDocument)
		if _, _, _, err := opts.open(kitState(t, State{Identity: id, Document: doc})); err == nil {
			t.Errorf("%s: open accepted the config", name)
		}
	}
	opts := RunOptions{Config: testConfig(t)}
	opts.Configure = func(*age.X25519Identity, Config, *localipc.Endpoint) (Config, *localipc.Endpoint, error) {
		return Config{}, nil, errors.New("example-secret failed")
	}
	if _, _, _, err := opts.open(kitState(t, State{Identity: id})); err == nil || strings.Contains(err.Error(), "example-secret") {
		t.Errorf("hook error: open = %v", err)
	}
}

func TestConfigureCannotExtendAnUnlock(t *testing.T) {
	id := newIdentity(t)
	opts := successorOptions(t)
	opts.Configure = func(_ *age.X25519Identity, base Config, _ *localipc.Endpoint) (Config, *localipc.Endpoint, error) {
		base.IdleTimeout, base.MaxUnlock = 10*time.Hour, 10*time.Hour
		return base, nil, nil
	}
	unlocked := time.Now().Add(-time.Minute)
	locksAt := time.Now().Add(2 * time.Minute)
	server, _, _, err := opts.open(kitState(t, State{Identity: id, UnlockedAt: unlocked, LocksAt: locksAt,
		MaxUnlock: time.Hour}))
	if err != nil {
		t.Fatal(err)
	}
	defer server.wipe()
	if server.locksAt.After(locksAt) {
		t.Errorf("locksAt %v is after the predecessor's %v", server.locksAt, locksAt)
	}
	if want := unlocked.Add(time.Hour); server.hardAt.After(want) {
		t.Errorf("hardAt %v is after %v", server.hardAt, want)
	}
	// Without a lock time a successor is still refused, whatever Configure returns.
	if _, _, _, err := opts.open(kitState(t, State{Identity: id})); err == nil {
		t.Error("a successor state without a lock time was accepted")
	}
}

func TestConfigureProvidesEarlier(t *testing.T) {
	id := newIdentity(t)
	e := testEndpoint(t)
	earlier := earlierEndpoint(e)
	opts := successorOptions(t)
	opts.Earlier = &e // replaced by what Configure returns
	var base *localipc.Endpoint
	opts.Configure = func(_ *age.X25519Identity, cfg Config, was *localipc.Endpoint) (Config, *localipc.Endpoint, error) {
		base = was
		return cfg, &earlier, nil
	}
	server, _, got, err := opts.open(kitState(t, State{Identity: id, LocksAt: time.Now().Add(time.Minute)}))
	if err != nil {
		t.Fatal(err)
	}
	server.wipe()
	if base != &e || got != &earlier {
		t.Errorf("Configure saw %p and returned %p; want %p and %p", base, got, &e, &earlier)
	}
	opts.Configure = func(_ *age.X25519Identity, cfg Config, _ *localipc.Endpoint) (Config, *localipc.Endpoint, error) {
		return cfg, nil, nil
	}
	server, _, got, err = opts.open(kitState(t, State{Identity: id, LocksAt: time.Now().Add(time.Minute)}))
	if err != nil || got != nil {
		t.Fatalf("Configure could not remove Earlier: %v, %v", got, err)
	}
	server.wipe()
}
