package releasetest_test

import (
	"io"
	"net/http"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release"
	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"golang.org/x/crypto/ssh"
)

func TestSignedReleaseVerifies(t *testing.T) {
	signer := releasetest.NewSigner(t)
	program := []byte("program")
	archives := map[string][]byte{
		"sample_v1_linux_amd64.tar.gz": releasetest.Archive(t, "sample_v1_linux_amd64.tar.gz", releasetest.File{Name: "bin/sample", Body: program}),
		"sample_v1_windows_amd64.zip":  releasetest.Archive(t, "sample_v1_windows_amd64.zip", releasetest.File{Name: "bin/sample.exe", Body: program}),
	}
	assets := releasetest.Release(t, signer, "sample-release", archives)
	server := releasetest.NewServer(t)
	server.Publish("v1.0.0", assets)
	get := func(path string) []byte {
		t.Helper()
		response, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("%s: %s", path, response.Status)
		}
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	get("/latest")
	checksums, signature := get("/download/v1.0.0/checksums.txt"), get("/download/v1.0.0/checksums.txt.sig")
	keys := []ssh.PublicKey{signer.PublicKey()}
	var sums []string
	for name, program := range map[string]string{"sample_v1_linux_amd64.tar.gz": "bin/sample", "sample_v1_windows_amd64.zip": "bin/sample.exe"} {
		archive := get("/download/v1.0.0/" + name)
		if err := release.VerifyArchive(checksums, signature, archive, "sample-release", name, keys); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		sum, err := release.ProgramSHA256(name, archive, program)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		sums = append(sums, sum)
	}
	if sums[0] != sums[1] {
		t.Fatalf("program hashes differ: %v", sums)
	}
	response, err := http.Get(server.URL + "/download/v1.0.0/missing")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("missing asset: %s", response.Status)
	}
}
