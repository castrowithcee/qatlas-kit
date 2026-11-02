package releasebuild_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release/releasebuild"
	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"golang.org/x/crypto/ssh"
)

func TestBuildRejectsInvalidInputWithoutOutput(t *testing.T) {
	tests := map[string]func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options){
		"invalid program": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			o.Program, o.Namespace = "Sample", "Sample-release"
		},
		"foreign namespace": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			o.Namespace = "other-release"
		},
		"version without v": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) { o.Version = "1.2.3" },
		"version with leading zero": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			o.Version = "v1.02.3"
		},
		"version with build metadata": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			o.Version = "v1.2.3+build"
		},
		"source date before 1980": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			o.SourceDate = sourceDate.AddDate(-50, 0, 0)
		},
		"no output": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) { o.Output = "" },
		"package outside module": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.Package = "./../other"
		},
		"package not relative": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.Package = "example.invalid/sample"
		},
		"version variable with space": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.VersionVariable = "main.version -X main.other"
		},
		"no target": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) { c.Targets = nil },
		"duplicate target": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.Targets = append(c.Targets, c.Targets[0])
		},
		"unsupported target": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.Targets = append(c.Targets, "linux/386")
		},
		"no manpage command": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.ManpageCommand = nil
		},
		"manpage command without dir": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.ManpageCommand = []string{os.Args[0], "manpage", "out"}
		},
		"manpage command fails": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			t.Setenv(manpageMode, "fail")
		},
		"manpage command writes nothing": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			t.Setenv(manpageMode, "none")
		},
		"manpage command writes other files": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			t.Setenv(manpageMode, "other")
		},
		"absolute file path": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.Files[0].Path = "/etc/sample"
		},
		"file path with dot-dot": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.Files[0].Path = "share/../../sample"
		},
		"file path with dot": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.Files[0].Path = "./share/sample"
		},
		"file path with backslash": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.Files[0].Path = `share\sample`
		},
		"file path in bin": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.Files[0].Path = "bin/" + program
		},
		"file path in manpages": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.Files[0].Path = "share/man/man1/" + program + ".1"
		},
		"duplicate file path": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.Files = append(c.Files, c.Files[0])
		},
		"file path is a directory of another": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.Files = append(c.Files, releasebuild.File{Source: "LICENSE", Path: c.Files[0].Path + "/inner"})
		},
		"source outside module": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.Files[0].Source = "../LICENSE"
		},
		"missing source": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.Files[0].Source = "MISSING"
		},
		"source is a directory": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			if err := os.Mkdir(filepath.Join(m.dir, "docs"), 0o755); err != nil {
				t.Fatal(err)
			}
			c.Files[0].Source = "docs"
		},
		"no allowed signers": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			o.AllowedSigners = ""
		},
		"allowed signers for another namespace": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			m.write(t, "allowed-signers", "other-release "+strings.TrimSpace(string(ssh.MarshalAuthorizedKey(m.signer.PublicKey())))+"\n")
		},
		"http releases URL": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			o.ReleasesURL = "http://example.invalid/releases"
		},
		"installer text on several lines": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			c.Installer.NextStep = "one\ntwo"
		},
		"output not empty": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			if err := os.MkdirAll(o.Output, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(o.Output, "old.txt"), []byte("old"), 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"build fails": func(t *testing.T, m *module, c *releasebuild.Config, o *releasebuild.Options) {
			m.write(t, "main.go", "package main\n\nfunc main() { undefined() }\n")
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			m := newModule(t)
			c, o := m.config(), m.options(t)
			mutate(t, m, &c, &o)
			if _, err := releasebuild.Build(context.Background(), c, o); err == nil {
				t.Fatal("Build accepted invalid input")
			}
			if name == "output not empty" {
				if entries, _ := os.ReadDir(o.Output); len(entries) != 1 {
					t.Errorf("output changed: %v", entries)
				}
			} else if o.Output != "" {
				if _, err := os.Stat(o.Output); !os.IsNotExist(err) {
					t.Errorf("output was created: %v", err)
				}
			}
		})
	}
}

func TestParseAllowedSigners(t *testing.T) {
	signer, other := releasetest.NewSigner(t), releasetest.NewSigner(t)
	key := func(s ssh.Signer) string { return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(s.PublicKey()))) }
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ecdsaPublic, err := ssh.NewPublicKey(&ecdsaKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	valid := "# keys\n\n" + namespace + " " + key(signer) + " current\r\n" + namespace + " " + key(other) + "\n"
	keys, err := releasebuild.ParseAllowedSigners([]byte(valid), namespace)
	if err != nil || len(keys) != 2 {
		t.Fatalf("ParseAllowedSigners = %d keys, %v", len(keys), err)
	}
	rejected := map[string]string{
		"empty":             "# only a comment\n",
		"other principal":   "other-release " + key(signer) + "\n",
		"two principals":    namespace + ",other-release " + key(signer) + "\n",
		"wildcard":          "* " + key(signer) + "\n",
		"options":           namespace + ` namespaces="` + namespace + `" ` + key(signer) + "\n",
		"non-Ed25519 key":   namespace + " " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(ecdsaPublic))) + "\n",
		"damaged key":       namespace + " ssh-ed25519 AAAAnotakey\n",
		"key type mismatch": namespace + " ssh-ed25519 " + strings.Fields(string(ssh.MarshalAuthorizedKey(ecdsaPublic)))[1] + "\n",
		"repeated key":      namespace + " " + key(signer) + "\n" + namespace + " " + key(signer) + " again\n",
		"principal only":    namespace + "\n",
	}
	for name, data := range rejected {
		t.Run(name, func(t *testing.T) {
			if keys, err := releasebuild.ParseAllowedSigners([]byte(data), namespace); err == nil {
				t.Errorf("accepted %d keys", len(keys))
			}
		})
	}
}

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		path := filepath.Join(dir, "release.json")
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	c, err := releasebuild.LoadConfig(write(`{"versionVariable": "main.version", "targets": ["linux/amd64"], "manpageCommand": ["gen", "{dir}"],
		"files": [{"source": "LICENSE", "path": "share/doc/x/LICENSE"}], "installer": {"displayName": "X"}}`))
	if err != nil || c.VersionVariable != "main.version" || len(c.Files) != 1 || c.Installer.DisplayName != "X" {
		t.Fatalf("LoadConfig = %+v, %v", c, err)
	}
	for name, body := range map[string]string{
		"unknown field":  `{"versionVariable": "main.version", "signingKey": "x"}`,
		"trailing data":  `{"versionVariable": "main.version"} {}`,
		"not an object":  `["main.version"]`,
		"wrong type":     `{"targets": "linux/amd64"}`,
		"nested unknown": `{"installer": {"displayName": "X", "trustedKey": "x"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := releasebuild.LoadConfig(write(body)); err == nil {
				t.Error("LoadConfig accepted invalid JSON")
			}
		})
	}
}
