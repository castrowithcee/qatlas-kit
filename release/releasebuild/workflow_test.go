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
	data, err := os.ReadFile("../../.github/workflows/release.yml")
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(data), "      - name: Sign checksums.txt\n")
	if !ok {
		t.Fatal("sign step not found")
	}
	_, rest, ok = strings.Cut(rest, "        run: |\n")
	if !ok {
		t.Fatal("sign script not found")
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
