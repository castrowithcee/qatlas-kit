package selfupdate

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"golang.org/x/crypto/ssh"
)

const (
	oldNamespace = "sample-release-old"
	oldSignature = "checksums.txt.old.sig"
)

// transitionClient is a client on v2.0.0 whose releases up to v1.5.0 carry the previous set.
func transitionClient(api *releaseAPI, signer ssh.Signer, executable string) *Client {
	c := api.client(signer, executable)
	c.CurrentVersion = "v2.0.0"
	c.PreviousSignature = &PreviousSignature{LastVersion: "v1.5.0", Namespace: oldNamespace, SignatureAsset: oldSignature}
	return c
}

func archives(t *testing.T, version string) map[string][]byte {
	return map[string][]byte{tarName(version): cliArchive(t, "program "+version, "man")}
}

func publishSigned(t *testing.T, api *releaseAPI, signer ssh.Signer, version, namespace, asset string) {
	api.publish(version, releasetest.Release(t, signer, namespace, asset, archives(t, version)))
}

func isIneligible(err error) bool {
	var target *TargetError
	return errors.As(err, &target) && target.Reason == TargetIneligible
}

func TestPreviousSignatureReleaseAfterBoundaryNeedsCurrentSet(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	publishSigned(t, api, signer, "v2.1.0", oldNamespace, oldSignature)
	publishSigned(t, api, signer, "v1.4.0", testNamespace, testSignatureAsset)
	publishSigned(t, api, signer, "v1.3.0", oldNamespace, "checksums.txt.missing.sig")
	_, executable := installation(t)
	c := transitionClient(api, signer, executable)
	for _, tag := range []string{"v2.1.0", "v1.4.0", "v1.3.0"} {
		if _, err := c.Target(context.Background(), tag); !isIneligible(err) {
			t.Fatalf("%s: %v", tag, err)
		}
	}
	list, err := c.List(context.Background())
	if err != nil || len(list) != 0 {
		t.Fatalf("list: %+v, %v", list, err)
	}
	if result, err := c.Check(context.Background()); err != nil || result.UpdateAvailable {
		t.Fatalf("check: %+v, %v", result, err)
	}
	assertFile(t, executable, "old program")
}

func TestPreviousSignatureMissingFileOfVersionsSetIsIneligible(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	for _, tag := range []string{"v1.2.0", "v2.1.0"} {
		assets := releasetest.Release(t, signer, testNamespace, testSignatureAsset, archives(t, tag))
		delete(assets, testSignatureAsset)
		api.publish(tag, assets)
	}
	_, executable := installation(t)
	c := transitionClient(api, signer, executable)
	for _, tag := range []string{"v1.2.0", "v2.1.0"} {
		if _, err := c.Target(context.Background(), tag); !isIneligible(err) {
			t.Fatalf("%s: %v", tag, err)
		}
	}
}

func TestPreviousSignatureWrongNamespaceFailsPrepare(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	// The previous-named file is signed under the current namespace, the current-named file under the previous one.
	publishSigned(t, api, signer, "v1.4.0", testNamespace, oldSignature)
	publishSigned(t, api, signer, "v2.1.0", oldNamespace, testSignatureAsset)
	_, executable := installation(t)
	c := transitionClient(api, signer, executable)
	for _, tag := range []string{"v1.4.0", "v2.1.0"} {
		target, err := c.Target(context.Background(), tag)
		if err != nil {
			t.Fatalf("%s: %v", tag, err)
		}
		if _, err := c.Prepare(context.Background(), target); err == nil || !strings.Contains(err.Error(), "verify release") {
			t.Fatalf("%s prepared: %v", tag, err)
		}
	}
	assertFile(t, executable, "old program")
}

func TestPreviousSignatureTagMustMatchSignedArchive(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	// Tag after the boundary, signed checksums.txt lists the archive of a version up to the boundary.
	late := releasetest.Release(t, signer, testNamespace, testSignatureAsset, archives(t, "v1.4.0"))
	late[tarName("v2.1.0")] = late[tarName("v1.4.0")]
	api.publish("v2.1.0", late)
	// Tag up to the boundary, signed checksums.txt lists the archive of a later version.
	early := releasetest.Release(t, signer, oldNamespace, oldSignature, archives(t, "v2.1.0"))
	early[tarName("v1.4.0")] = early[tarName("v2.1.0")]
	api.publish("v1.4.0", early)
	_, executable := installation(t)
	c := transitionClient(api, signer, executable)
	for _, tag := range []string{"v2.1.0", "v1.4.0"} {
		target, err := c.Target(context.Background(), tag)
		if err != nil {
			t.Fatalf("%s: %v", tag, err)
		}
		if _, err := c.Prepare(context.Background(), target); err == nil {
			t.Fatalf("%s prepared with a foreign archive listing", tag)
		}
	}
	assertFile(t, executable, "old program")
}

func TestPreviousSignatureRollbackAcrossBoundary(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	publishSigned(t, api, signer, "v1.4.0", oldNamespace, oldSignature)
	publishSigned(t, api, signer, "v2.1.0", testNamespace, testSignatureAsset)
	_, executable := installation(t)
	c := transitionClient(api, signer, executable)
	c.ValidateRollback = func(context.Context, *Prepared) error { return nil }
	var files ReleaseFiles
	c.BeforeReplace = func(_ context.Context, f ReleaseFiles) error {
		files = f
		return nil
	}
	target, err := c.Target(context.Background(), "v1.4.0")
	if err != nil || !target.Older {
		t.Fatalf("target: %+v, %v", target, err)
	}
	p, err := c.Prepare(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.InstallPrepared(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	assertFile(t, executable, "program v1.4.0")
	if filepath.Base(files.Signature) != oldSignature || !files.Rollback {
		t.Fatalf("release files: %+v", files)
	}
}

func TestPreviousSignatureLaterReleaseUsesCurrentSetOnly(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	both := releasetest.Release(t, signer, testNamespace, testSignatureAsset, archives(t, "v2.1.0"))
	both[oldSignature] = []byte("not a signature")
	api.publish("v2.1.0", both)
	brokenCurrent := releasetest.Release(t, signer, testNamespace, testSignatureAsset, archives(t, "v2.2.0"))
	brokenCurrent[testSignatureAsset] = []byte("not a signature")
	brokenCurrent[oldSignature] = releasetest.Sign(t, signer, oldNamespace, "sha512", brokenCurrent["checksums.txt"])
	api.publish("v2.2.0", brokenCurrent)
	_, executable := installation(t)
	c := transitionClient(api, signer, executable)
	c.CurrentVersion = "v1.0.0"
	target, err := c.Target(context.Background(), "v2.1.0")
	if err != nil {
		t.Fatal(err)
	}
	p, err := c.Prepare(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.InstallPrepared(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	assertFile(t, executable, "program v2.1.0")
	target, err = c.Target(context.Background(), "v2.2.0")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Prepare(context.Background(), target); err == nil {
		t.Fatal("broken current signature accepted because the previous one is valid")
	}
}

func TestPreviousSignatureListShowsBothSides(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	publishSigned(t, api, signer, "v1.4.0", oldNamespace, oldSignature)
	publishSigned(t, api, signer, "v1.5.0", oldNamespace, oldSignature)
	publishSigned(t, api, signer, "v2.0.0", testNamespace, testSignatureAsset)
	publishSigned(t, api, signer, "v2.1.0", testNamespace, testSignatureAsset)
	_, executable := installation(t)
	c := transitionClient(api, signer, executable)
	list, err := c.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range list {
		got = append(got, e.Version)
	}
	if strings.Join(got, " ") != "v2.1.0 v2.0.0 v1.5.0 v1.4.0" {
		t.Fatalf("list: %v", got)
	}
}

func TestPreviousSignatureMustBeValid(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	for name, previous := range map[string]PreviousSignature{
		"invalid last version":       {"1.5", oldNamespace, oldSignature},
		"empty last version":         {"", oldNamespace, oldSignature},
		"empty namespace":            {"v1.5.0", "", oldSignature},
		"same namespace":             {"v1.5.0", testNamespace, oldSignature},
		"invalid signature asset":    {"v1.5.0", oldNamespace, "dir/old.sig"},
		"empty signature asset":      {"v1.5.0", oldNamespace, ""},
		"checksums as signature":     {"v1.5.0", oldNamespace, "checksums.txt"},
		"same signature asset":       {"v1.5.0", oldNamespace, testSignatureAsset},
		"whitespace signature asset": {"v1.5.0", oldNamespace, "old sig"},
	} {
		c := transitionClient(api, signer, "/unused")
		c.PreviousSignature = &previous
		if _, err := c.validate(); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := transitionClient(api, signer, "/unused").validate(); err != nil {
		t.Fatal(err)
	}
}

func TestPreviousSignaturePreparedBoundToVersionsSet(t *testing.T) {
	signer := releasetest.NewSigner(t)
	api := newReleaseAPI(t)
	publishSigned(t, api, signer, "v1.4.0", testNamespace, testSignatureAsset)
	prefix, executable := installation(t)
	_ = prefix
	plain := api.client(signer, executable)
	plain.CurrentVersion = "v2.0.0"
	plain.ValidateRollback = func(context.Context, *Prepared) error { return nil }
	target, err := plain.Target(context.Background(), "v1.4.0")
	if err != nil {
		t.Fatal(err)
	}
	p, err := plain.Prepare(context.Background(), target)
	if err != nil {
		t.Fatal(err)
	}
	withOption := transitionClient(api, signer, executable)
	withOption.ValidateRollback = plain.ValidateRollback
	if _, err := withOption.InstallPrepared(context.Background(), p); err == nil || !strings.Contains(err.Error(), "different trust policy") {
		t.Fatalf("install under another set: %v", err)
	}
	assertFile(t, executable, "old program")
	if _, err := plain.InstallPrepared(context.Background(), p); err != nil {
		t.Fatal(err)
	}
}
