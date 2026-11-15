package releasebuild_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// TestReleaseWorkflowIsolatesSigning checks the reusable workflow for the properties its callers rely on.
func TestReleaseWorkflowIsolatesSigning(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.Contains(text, "\non:\n  workflow_call:\n") || regexp.MustCompile(`(?m)^  (push|pull_request|pull_request_target|workflow_dispatch|schedule):`).MatchString(text) {
		t.Error("the workflow must run only through workflow_call")
	}
	if !strings.Contains(text, "\npermissions: {}\n") {
		t.Error("the workflow must grant no default permissions")
	}
	pinned := regexp.MustCompile(`^\s*(- )?uses: [^@\s]+@[0-9a-f]{40}( #.*)?$`)
	jobHeader := regexp.MustCompile(`^  ([a-z-]+):$`)
	jobs := map[string]string{}
	inJobs, job := false, ""
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "uses:") && !pinned.MatchString(line) {
			t.Errorf("action is not pinned to a commit: %s", strings.TrimSpace(line))
		}
		if line == "jobs:" {
			inJobs = true
		} else if header := jobHeader.FindStringSubmatch(line); inJobs && header != nil {
			job = header[1]
		} else if job != "" {
			jobs[job] += line + "\n"
		}
	}
	if len(jobs) != 3 || jobs["build"] == "" || jobs["sign"] == "" || jobs["publish"] == "" {
		t.Fatalf("jobs = %d, want build, sign and publish", len(jobs))
	}
	sign := jobs["sign"]
	for _, forbidden := range []string{"actions/checkout", "actions/setup-go", "go run", "go build", "go test", "contents: write"} {
		if strings.Contains(sign, forbidden) {
			t.Errorf("the sign job must not use %q", forbidden)
		}
	}
	if !strings.Contains(sign, "environment: ${{ inputs.environment }}") {
		t.Error("the sign job must run in the protected environment")
	}
	for name, body := range jobs {
		if name != "sign" && strings.Contains(body, "secrets.") {
			t.Errorf("job %s reads a secret", name)
		}
	}
	if strings.Count(text, "secrets.") != 1 || !strings.Contains(sign, "${{ secrets.RELEASE_SIGNING_KEY }}") {
		t.Error("only the sign job may read the signing key secret")
	}
	if !strings.Contains(jobs["publish"], "contents: write") || strings.Contains(jobs["build"], "contents: write") {
		t.Error("only the publish job may write contents")
	}
	if !regexp.MustCompile(`(?m)^      signature-asset:\n(        .*\n)*?        required: true\n`).MatchString(text) {
		t.Error("the signature file name must be a required input")
	}
	if !strings.Contains(jobs["build"], `-signature-asset "$SIGNATURE_ASSET"`) {
		t.Error("the build job must pass the signature file name to releasebuild")
	}
	if !strings.Contains(sign, "SIGNATURE_ASSET: ${{ inputs.signature-asset }}") || strings.Contains(sign, "needs.build") {
		t.Error("the sign job must take the signature file name from the workflow input")
	}
	if !strings.Contains(sign, `-s "../signatures/$SIGNATURE_ASSET"`) || !strings.Contains(sign, "path: signatures\n") {
		t.Error("the sign job must verify and upload the file named by the input")
	}
	for _, name := range []string{"previous-namespace", "previous-signature-asset"} {
		if !regexp.MustCompile(`(?m)^      ` + name + `:\n(        .*\n)*?        default: ''\n`).MatchString(text) {
			t.Errorf("input %s must be optional with an empty default", name)
		}
	}
	check := text[strings.Index(text, "- name: Check inputs"):strings.Index(text, "- uses: actions/checkout")]
	for _, want := range []string{
		`[a-z0-9][a-z0-9._-]{0,127}`, `"$PREVIOUS_NAMESPACE" != "$NAMESPACE"`, `"$PREVIOUS_SIGNATURE_ASSET" != "$SIGNATURE_ASSET"`,
		`case "$PREVIOUS_SIGNATURE_ASSET" in checksums.txt|allowed-signers|install.sh|install.ps1)`, "set together",
	} {
		if !strings.Contains(check, want) {
			t.Errorf("Check inputs lacks %q", want)
		}
	}
	if !strings.Contains(jobs["build"], `-previous-namespace "$PREVIOUS_NAMESPACE" -previous-signature-asset "$PREVIOUS_SIGNATURE_ASSET"`) {
		t.Error("the build job must pass the former signature names to releasebuild")
	}
	for _, want := range []string{
		"PREVIOUS_NAMESPACE: ${{ inputs.previous-namespace }}", "PREVIOUS_SIGNATURE_ASSET: ${{ inputs.previous-signature-asset }}",
		`-n "$PREVIOUS_NAMESPACE" checksums.txt`, `-I "$NAMESPACE" -n "$PREVIOUS_NAMESPACE"`,
	} {
		if !strings.Contains(sign, want) {
			t.Errorf("the sign job lacks %q", want)
		}
	}
}

// signStep returns the script of the sign step of the workflow.
func signStep(t *testing.T) string {
	t.Helper()
	return stepScript(t, "Sign checksums.txt")
}

// stepScript returns the run script of the named step of the workflow.
func stepScript(t *testing.T, step string) string {
	t.Helper()
	data, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	_, rest, ok := strings.Cut(text, "      - name: "+step+"\n")
	if !ok {
		t.Fatalf("step %q not found", step)
	}
	_, rest, ok = strings.Cut(rest, "        run: |\n")
	if !ok {
		t.Fatalf("script of step %q not found", step)
	}
	var script strings.Builder
	for _, line := range strings.SplitAfter(rest, "\n") {
		if line != "\n" && !strings.HasPrefix(line, "          ") {
			break
		}
		script.WriteString(strings.TrimPrefix(line, "          "))
	}
	return script.String()
}

// TestSignStepSignsBothNamespaces runs the sign script of the workflow with a throwaway key.
func TestSignStepSignsBothNamespaces(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the sign job runs on Linux")
	}
	keygen, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen is not available")
	}
	script := signStep(t)
	const checksums = "0000000000000000000000000000000000000000000000000000000000000000  sample-tool_v1.0.0_linux_amd64.tar.gz\n"
	tests := map[string]struct {
		asset, previousNS, previousAsset string
		wantErr                          bool
		want                             []string
	}{
		"current only":               {asset: "demo.sig", want: []string{"demo.sig"}},
		"both":                       {asset: "demo.sig", previousNS: "olddemo-release", previousAsset: "olddemo.sig", want: []string{"demo.sig", "olddemo.sig"}},
		"previous is default name":   {asset: "demo.sig", previousNS: "olddemo-release", previousAsset: "checksums.txt.sig", want: []string{"checksums.txt.sig", "demo.sig"}},
		"current is default name":    {asset: "checksums.txt.sig", previousNS: "olddemo-release", previousAsset: "olddemo.sig", want: []string{"checksums.txt.sig", "olddemo.sig"}},
		"same names":                 {asset: "demo.sig", previousNS: "olddemo-release", previousAsset: "demo.sig", wantErr: true},
		"same namespace":             {asset: "demo.sig", previousNS: "sample-release", previousAsset: "olddemo.sig", wantErr: true},
		"previous is checksums":      {asset: "demo.sig", previousNS: "olddemo-release", previousAsset: "checksums.txt", wantErr: true},
		"only previous namespace":    {asset: "demo.sig", previousNS: "olddemo-release", wantErr: true},
		"only previous asset":        {asset: "demo.sig", previousAsset: "olddemo.sig", wantErr: true},
		"invalid previous asset":     {asset: "demo.sig", previousNS: "olddemo-release", previousAsset: "../x", wantErr: true},
		"invalid previous namespace": {asset: "demo.sig", previousNS: "Old Release", previousAsset: "olddemo.sig", wantErr: true},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			key := filepath.Join(dir, "key")
			if out, err := exec.Command(keygen, "-q", "-t", "ed25519", "-N", "", "-f", key).CombinedOutput(); err != nil {
				t.Fatalf("ssh-keygen: %v\n%s", err, out)
			}
			pub, err := os.ReadFile(key + ".pub")
			if err != nil {
				t.Fatal(err)
			}
			privateKey, err := os.ReadFile(key)
			if err != nil {
				t.Fatal(err)
			}
			work := filepath.Join(dir, "work")
			input := filepath.Join(work, "signing-input")
			if err := os.MkdirAll(input, 0o755); err != nil {
				t.Fatal(err)
			}
			fields := strings.Fields(string(pub))
			signers := "sample-release " + fields[0] + " " + fields[1] + "\n"
			for file, body := range map[string]string{"checksums.txt": checksums, "allowed-signers": signers} {
				if err := os.WriteFile(filepath.Join(input, file), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command("bash", "-c", script)
			cmd.Dir = work
			cmd.Env = append(os.Environ(), "NAMESPACE=sample-release", "SIGNATURE_ASSET="+tc.asset, "PREVIOUS_NAMESPACE="+tc.previousNS,
				"PREVIOUS_SIGNATURE_ASSET="+tc.previousAsset, "RUNNER_TEMP="+dir, "RELEASE_SIGNING_KEY="+strings.TrimSpace(string(privateKey)))
			out, err := cmd.CombinedOutput()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("the script succeeded, want failure:\n%s", out)
				}
				return
			}
			if err != nil {
				t.Fatalf("script: %v\n%s", err, out)
			}
			entries, err := os.ReadDir(filepath.Join(work, "signatures"))
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, e := range entries {
				got = append(got, e.Name())
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("signatures = %v, want %v", got, tc.want)
			}
			verify := func(file, namespace string, ok bool) {
				t.Helper()
				cmd := exec.Command(keygen, "-Y", "verify", "-f", "allowed-signers", "-I", "sample-release", "-n", namespace, "-s", filepath.Join("..", "signatures", file))
				cmd.Dir = input
				cmd.Stdin = strings.NewReader(checksums)
				if err := cmd.Run(); (err == nil) != ok {
					t.Errorf("verify %s under %s: err = %v, want ok = %v", file, namespace, err, ok)
				}
			}
			verify(tc.asset, "sample-release", true)
			verify(tc.asset, "olddemo-release", false)
			if tc.previousAsset != "" {
				verify(tc.previousAsset, tc.previousNS, true)
				verify(tc.previousAsset, "sample-release", false)
			}
		})
	}
}

// TestCheckInputsStepChecksNamespace runs the input check of the build job, which must accept the same
// namespaces as Build.
func TestCheckInputsStepChecksNamespace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the build job runs on Linux")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not available")
	}
	script := stepScript(t, "Check inputs")
	tests := map[string]bool{
		"sample-tool-release":                true,
		"sample-suite-release":               true,
		strings.Repeat("a", 64) + "-release": true,
		"Sample-tool-release":                false,
		"sample-tool":                        false,
		"-release":                           false,
		strings.Repeat("a", 65) + "-release": false,
		"sample_tool-release":                false,
		"1sample-release":                    false,
		"x y\nsample-release":                false,
		"sample-release\nx":                  false,
	}
	for namespace, ok := range tests {
		t.Run(strings.ReplaceAll(namespace, "\n", "_"), func(t *testing.T) {
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(), "GITHUB_REF_TYPE=tag", "GITHUB_REF_NAME=v1.2.3", "PROGRAM=sample-tool", "NAMESPACE="+namespace,
				"SIGNATURE_ASSET=demo.sig", "PREVIOUS_NAMESPACE=", "PREVIOUS_SIGNATURE_ASSET=", "ENVIRONMENT=release")
			out, err := cmd.CombinedOutput()
			if (err == nil) != ok {
				t.Errorf("err = %v, want ok = %v\n%s", err, ok, out)
			}
		})
	}
}

// TestDryRunSkipsSigningAndPublishing checks that a dry run reaches neither the signing environment, the
// secret nor the release.
func TestDryRunSkipsSigningAndPublishing(t *testing.T) {
	data, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	_, jobsText, _ := strings.Cut(text, "\njobs:\n")
	build, rest, _ := strings.Cut(jobsText, "\n  sign:\n")
	sign, publish, _ := strings.Cut(rest, "\n  publish:\n")
	if build == "" || sign == "" || publish == "" {
		t.Fatal("jobs build, sign and publish not found")
	}
	jobIf := regexp.MustCompile(`(?m)^    if: \$\{\{ !inputs\.dry-run \}\}$`)
	// The condition stands before the steps, so it belongs to the job and not to a step.
	for name, body := range map[string]string{"sign": sign, "publish": publish} {
		head, _, _ := strings.Cut(body, "    steps:\n")
		if !jobIf.MatchString(head) {
			t.Errorf("job %s must be skipped in a dry run", name)
		}
	}
	if regexp.MustCompile(`(?m)^    if:`).MatchString(build) {
		t.Error("the build job must run in a dry run")
	}
	if !strings.Contains(sign, "\n    environment: ") || strings.Count(text, "environment: ${{") != 1 || strings.Contains(build, "environment:") || strings.Contains(publish, "environment:") {
		t.Error("only the sign job may enter an environment")
	}
	if strings.Contains(build, "secrets.") || strings.Contains(publish, "secrets.") {
		t.Error("only the sign job may read a secret")
	}
	if !strings.Contains(publish, `[ "$GITHUB_REF_TYPE" = tag ]`) {
		t.Error("the publish job must check for a tag itself")
	}
	for _, want := range []string{"      dry-run:\n        description:", "        type: boolean\n        default: false\n", "      version:\n        description:"} {
		if !strings.Contains(text, want) {
			t.Errorf("the workflow lacks the input %q", want)
		}
	}
}

// TestCheckInputsStepChecksDryRun runs the input check of the build job for dry runs and releases.
func TestCheckInputsStepChecksDryRun(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the build job runs on Linux")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash is not available")
	}
	script := stepScript(t, "Check inputs")
	tests := map[string]struct {
		dryRun, refType, version string
		ok                       bool
	}{
		"dry run on a branch":                  {"true", "branch", "v1.2.3", true},
		"dry run with pre-release":             {"true", "branch", "v1.2.3-rc.1", true},
		"dry run on a tag":                     {"true", "tag", "v1.2.3", true},
		"dry run without version":              {"true", "branch", "", false},
		"dry run on a tag without version":     {"true", "tag", "", false},
		"dry run version without v":            {"true", "branch", "1.2.3", false},
		"dry run version without patch":        {"true", "branch", "v1.2", false},
		"dry run version with leading zero":    {"true", "branch", "v01.2.3", false},
		"dry run version with empty suffix":    {"true", "branch", "v1.2.3-", false},
		"dry run version with build metadata":  {"true", "branch", "v1.2.3+b1", false},
		"dry run version with space":           {"true", "branch", "v1.2.3 ", false},
		"dry run version with second line":     {"true", "branch", "v1.2.3\nx", false},
		"dry run version with leading line":    {"true", "branch", "x\nv1.2.3", false},
		"dry run version with path":            {"true", "branch", "v1.2.3/../x", false},
		"release on a tag":                     {"false", "tag", "", true},
		"release on a branch":                  {"false", "branch", "", false},
		"release on a branch with version":     {"false", "branch", "v1.2.3", false},
		"release on a tag with version":        {"false", "tag", "v1.2.3", false},
		"release with unset dry run on branch": {"", "branch", "", false},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command("bash", "-c", script)
			cmd.Env = append(os.Environ(), "GITHUB_REF_TYPE="+tc.refType, "GITHUB_REF_NAME=v1.2.3", "PROGRAM=sample-tool", "NAMESPACE=sample-tool-release",
				"SIGNATURE_ASSET=demo.sig", "PREVIOUS_NAMESPACE=", "PREVIOUS_SIGNATURE_ASSET=", "ENVIRONMENT=release", "DRY_RUN="+tc.dryRun, "VERSION="+tc.version)
			out, err := cmd.CombinedOutput()
			if (err == nil) != tc.ok {
				t.Errorf("err = %v, want ok = %v\n%s", err, tc.ok, out)
			}
		})
	}
	t.Run("dry run still checks the namespace", func(t *testing.T) {
		cmd := exec.Command("bash", "-c", script)
		cmd.Env = append(os.Environ(), "GITHUB_REF_TYPE=branch", "GITHUB_REF_NAME=main", "PROGRAM=sample-tool", "NAMESPACE=sample-tool",
			"SIGNATURE_ASSET=demo.sig", "PREVIOUS_NAMESPACE=", "PREVIOUS_SIGNATURE_ASSET=", "ENVIRONMENT=release", "DRY_RUN=true", "VERSION=v1.2.3")
		if out, err := cmd.CombinedOutput(); err == nil {
			t.Errorf("the script succeeded, want failure\n%s", out)
		}
	})
}

// TestBuildStepVersionAndSourceDate runs the build script with a stand-in for the go command and checks the
// version and source date it passes on.
func TestBuildStepVersionAndSourceDate(t *testing.T) {
	// The script relies on bash 4 or later for an empty array under set -u, as on the Linux runner.
	if runtime.GOOS != "linux" {
		t.Skip("the build job runs on Linux")
	}
	for _, tool := range []string{"bash", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not available", tool)
		}
	}
	script := stepScript(t, "Build release assets")
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := "#!/bin/sh\nprintf '%s\\n' \"$SOURCE_DATE_EPOCH\" \"$@\" > \"$RECORD\"\n"
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(fake), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "HOME="+dir, "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null", "GIT_AUTHOR_NAME=a", "GIT_AUTHOR_EMAIL=a@example.invalid",
		"GIT_COMMITTER_NAME=a", "GIT_COMMITTER_EMAIL=a@example.invalid")
	git := func(date string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		cmd.Env = append(env, "GIT_COMMITTER_DATE="+date, "GIT_AUTHOR_DATE="+date)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("1000000000 +0000", "init", "-q")
	git("1000000000 +0000", "commit", "-q", "--allow-empty", "-m", "first")
	git("1000000000 +0000", "tag", "v1.2.3")
	git("1000000100 +0000", "commit", "-q", "--allow-empty", "-m", "second")
	tests := map[string]struct {
		dryRun, refType, refName, version string
		wantEpoch, wantVersion            string
	}{
		"dry run uses version and HEAD":         {"true", "branch", "main", "v9.8.7", "1000000100", "v9.8.7"},
		"release uses the tag and its commit":   {"false", "tag", "v1.2.3", "", "1000000000", "v1.2.3"},
		"dry run on a tag still uses HEAD":      {"true", "tag", "v1.2.3", "v9.8.7-rc.1", "1000000100", "v9.8.7-rc.1"},
		"release with unset dry run is release": {"", "tag", "v1.2.3", "", "1000000000", "v1.2.3"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			record := filepath.Join(dir, "record")
			os.Remove(record)
			cmd := exec.Command("bash", "-c", script)
			cmd.Dir = repo
			cmd.Env = append(env, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "RECORD="+record, "GITHUB_REF_TYPE="+tc.refType, "GITHUB_REF_NAME="+tc.refName,
				"GITHUB_SERVER_URL=https://example.invalid", "GITHUB_REPOSITORY=example/sample", "PROGRAM=sample-tool", "NAMESPACE=sample-tool-release", "SIGNATURE_ASSET=demo.sig",
				"PREVIOUS_NAMESPACE=", "PREVIOUS_SIGNATURE_ASSET=", "CONFIG=release.json", "ALLOWED_SIGNERS=signers", "DRY_RUN="+tc.dryRun, "VERSION="+tc.version)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("script: %v\n%s", err, out)
			}
			data, err := os.ReadFile(record)
			if err != nil {
				t.Fatal(err)
			}
			lines := strings.Fields(string(data))
			if len(lines) == 0 || lines[0] != tc.wantEpoch {
				t.Errorf("SOURCE_DATE_EPOCH = %v, want %s", lines, tc.wantEpoch)
			}
			i := slices.Index(lines, "-version")
			if i < 0 || i+1 >= len(lines) || lines[i+1] != tc.wantVersion {
				t.Errorf("arguments = %v, want -version %s", lines, tc.wantVersion)
			}
		})
	}
}
