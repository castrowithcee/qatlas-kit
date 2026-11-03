package releasebuild_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release/releasebuild"
	"github.com/castrowithcee/qatlas-kit/sysservice"
)

const serviceUnit = `[Unit]
Description=Sample tool

[Service]
User=sample-tool
Group=sample-tool
ExecStart=/usr/local/bin/sample-tool serve
NoNewPrivileges=yes
ProtectSystem=strict
PrivateTmp=yes
ConfigurationDirectory=sample-tool
StateDirectory=sample-tool
SupplementaryGroups=sample-data
ReadWritePaths=/etc/sample-tool

[Install]
WantedBy=multi-user.target
`

// serviceConfig returns a valid system service Config for m and writes its unit file.
func serviceConfig(t *testing.T, m *module) releasebuild.Config {
	t.Helper()
	m.write(t, "tool.service", serviceUnit)
	c := m.config()
	c.Targets = []string{"linux/arm64", "linux/amd64"}
	c.Service = &releasebuild.Service{Unit: "tool.service", Groups: []string{"sample-data"}, AddInstallingUser: true, WritableConfig: true}
	return c
}

func TestBuildWritesServiceArchives(t *testing.T) {
	m := newModule(t)
	c := serviceConfig(t, m)
	o := m.options(t)
	names, err := releasebuild.Build(context.Background(), c, o)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		releasebuild.ArchiveName(program, version, "linux", "amd64"),
		releasebuild.ArchiveName(program, version, "linux", "arm64"),
		"checksums.txt", "install.sh",
	}
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("assets = %v, want %v", names, want)
	}
	assets := readAssets(t, o.Output)
	if !strings.Contains(string(assets["checksums.txt"]), "  install.sh\n") {
		t.Errorf("checksums.txt does not list install.sh:\n%s", assets["checksums.txt"])
	}
	if !strings.Contains(string(assets["install.sh"]), "unit_entry='"+sysservice.UnitEntry(program)+"'") {
		t.Error("install.sh is not the system variant")
	}
	for _, arch := range []string{"amd64", "arm64"} {
		name := releasebuild.ArchiveName(program, version, "linux", arch)
		unit, ok := listArchive(t, name, assets[name])[sysservice.UnitEntry(program)]
		if !ok || !bytes.Equal(unit.body, []byte(serviceUnit)) || unit.mode != 0o644 {
			t.Errorf("%s: unit = %v, %q", name, ok, unit.body)
		}
	}

	again := o
	again.Output = filepath.Join(t.TempDir(), "again")
	if _, err := releasebuild.Build(context.Background(), c, again); err != nil {
		t.Fatal(err)
	}
	for name, data := range readAssets(t, again.Output) {
		if !bytes.Equal(data, assets[name]) {
			t.Errorf("%s differs between two builds", name)
		}
	}
}

func TestBuildRejectsInvalidService(t *testing.T) {
	tests := map[string]func(t *testing.T, m *module, c *releasebuild.Config){
		"darwin target":  func(t *testing.T, m *module, c *releasebuild.Config) { c.Targets = append(c.Targets, "darwin/arm64") },
		"windows target": func(t *testing.T, m *module, c *releasebuild.Config) { c.Targets = append(c.Targets, "windows/amd64") },
		"missing unit file": func(t *testing.T, m *module, c *releasebuild.Config) {
			c.Service.Unit = "missing.service"
		},
		"unit is a symlink": func(t *testing.T, m *module, c *releasebuild.Config) {
			if runtime.GOOS == "windows" {
				t.Skip("symlinks are not available")
			}
			if err := os.Symlink("tool.service", filepath.Join(m.dir, "link.service")); err != nil {
				t.Skip("symlinks are not available: " + err.Error())
			}
			c.Service.Unit = "link.service"
		},
		"unit path outside module": func(t *testing.T, m *module, c *releasebuild.Config) { c.Service.Unit = "../x" },
		"unit violates a rule": func(t *testing.T, m *module, c *releasebuild.Config) {
			m.write(t, "tool.service", strings.Replace(serviceUnit, "NoNewPrivileges=yes", "NoNewPrivileges=no", 1))
		},
		"group wheel": func(t *testing.T, m *module, c *releasebuild.Config) { c.Service.Groups = []string{"wheel"} },
		"files use the unit entry": func(t *testing.T, m *module, c *releasebuild.Config) {
			c.Files = append(c.Files, releasebuild.File{Source: "LICENSE", Path: sysservice.UnitEntry(program)})
		},
		"files use a directory of the unit entry as file": func(t *testing.T, m *module, c *releasebuild.Config) {
			c.Files = append(c.Files, releasebuild.File{Source: "LICENSE", Path: "lib/systemd/system"})
		},
		"files use a parent of the unit entry as file": func(t *testing.T, m *module, c *releasebuild.Config) {
			c.Files = append(c.Files, releasebuild.File{Source: "LICENSE", Path: "lib/systemd"})
		},
		"files below the unit entry": func(t *testing.T, m *module, c *releasebuild.Config) {
			c.Files = append(c.Files, releasebuild.File{Source: "LICENSE", Path: sysservice.UnitEntry(program) + "/inner"})
		},
		"empty service": func(t *testing.T, m *module, c *releasebuild.Config) { c.Service = &releasebuild.Service{} },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			m := newModule(t)
			c, o := serviceConfig(t, m), m.options(t)
			mutate(t, m, &c)
			if _, err := releasebuild.Build(context.Background(), c, o); err == nil {
				t.Fatal("Build accepted invalid input")
			}
			if _, err := os.Stat(o.Output); !os.IsNotExist(err) {
				t.Errorf("output was created: %v", err)
			}
		})
	}
}

func TestBuildNamesServiceUnitOnViolation(t *testing.T) {
	m := newModule(t)
	c := serviceConfig(t, m)
	m.write(t, "tool.service", strings.Replace(serviceUnit, "PrivateTmp=yes", "PrivateTmp=no", 1))
	_, err := releasebuild.Build(context.Background(), c, m.options(t))
	if err == nil || !strings.Contains(err.Error(), "tool.service") {
		t.Errorf("error = %v, want one that names the unit file", err)
	}
}
