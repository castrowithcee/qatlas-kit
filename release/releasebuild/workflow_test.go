package releasebuild_test

import (
	"os"
	"regexp"
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
	if !strings.Contains(sign, `-s "$SIGNATURE_ASSET"`) || !strings.Contains(sign, "path: signing-input/${{ inputs.signature-asset }}") {
		t.Error("the sign job must verify and upload the file named by the input")
	}
}
