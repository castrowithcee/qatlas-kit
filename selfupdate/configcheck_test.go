package selfupdate

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const checkHelper = "config-check-helper"

// TestMain lets the test binary stand in for the program of a prepared release.
func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == checkHelper {
		os.Exit(configHelper(os.Args[2]))
	}
	os.Exit(m.Run())
}

func configHelper(config string) int {
	if os.Getenv("SAMPLE_SECRET") != "" || os.Getenv("HOME") != filepath.Dir(config) {
		return 4
	}
	body, err := os.ReadFile(config)
	if err != nil {
		return 5
	}
	switch string(body) {
	case "readable":
		return 0
	case "hang":
		time.Sleep(time.Minute)
	}
	return 3
}

func helperRelease(t *testing.T) *Prepared {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	program, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	return &Prepared{version: "v1.0.0", executable: program, trust: trustBinding{program: "sample"}}
}

func helperArgs(config string) []string { return []string{checkHelper, config} }

func TestReadsConfigRunsProgramIsolated(t *testing.T) {
	t.Setenv("SAMPLE_SECRET", "secret value")
	p := helperRelease(t)
	reads, err := p.ReadsConfig(context.Background(), ConfigCheck{Config: []byte("readable"), FileName: "config.yaml", Args: helperArgs})
	if err != nil || !reads {
		t.Fatalf("readable configuration: %v, %v", reads, err)
	}
	reads, err = p.ReadsConfig(context.Background(), ConfigCheck{Config: []byte("unknown format"), FileName: "config.yaml", Args: helperArgs})
	if err != nil || reads {
		t.Fatalf("unreadable configuration: %v, %v", reads, err)
	}
}

func TestReadsConfigFailsWithoutAnswer(t *testing.T) {
	p := helperRelease(t)
	if _, err := p.ReadsConfig(context.Background(), ConfigCheck{Config: []byte("hang"), FileName: "config.yaml", Args: helperArgs, Timeout: 500 * time.Millisecond}); err == nil {
		t.Fatal("hanging program gave an answer")
	}
	broken := &Prepared{version: "v1.0.0", executable: []byte("not a program"), trust: p.trust}
	if _, err := broken.ReadsConfig(context.Background(), ConfigCheck{Config: []byte("readable"), FileName: "config.yaml", Args: helperArgs}); err == nil {
		t.Fatal("program that cannot start gave an answer")
	}
	for _, check := range []ConfigCheck{
		{FileName: "config.yaml"},
		{FileName: "", Args: helperArgs},
		{FileName: "../config.yaml", Args: helperArgs},
		{FileName: programFile("sample"), Args: helperArgs},
	} {
		if _, err := p.ReadsConfig(context.Background(), check); err == nil {
			t.Errorf("check %+v accepted", check)
		}
	}
}
