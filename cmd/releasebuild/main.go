// Command releasebuild writes the assets of a signed release for the Go module in the current directory;
// see the releasebuild package for the archive layout and the checks.
//
//	releasebuild -config release.json -program tool -namespace tool-release \
//		-allowed-signers .github/release-allowed-signers \
//		-releases-url https://github.com/owner/tool/releases -version v1.2.3 -out dist
//
// The source date is SOURCE_DATE_EPOCH or, when that is unset, the commit time of the version tag.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/castrowithcee/qatlas-kit/release/releasebuild"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "releasebuild:", err)
		var usage usageError
		if errors.As(err, &usage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

type usageError struct{ error }

func run() error {
	flags := flag.NewFlagSet("releasebuild", flag.ContinueOnError)
	config := flags.String("config", "", "release configuration file (JSON)")
	var o releasebuild.Options
	flags.StringVar(&o.Program, "program", "", "program file name without .exe")
	flags.StringVar(&o.Namespace, "namespace", "", "signature namespace, <program>-release")
	flags.StringVar(&o.AllowedSigners, "allowed-signers", "", "allowed signers file with the release keys")
	flags.StringVar(&o.ReleasesURL, "releases-url", "", "https URL of the release page")
	flags.StringVar(&o.Version, "version", "", "SemVer tag of the release")
	flags.StringVar(&o.Output, "out", "", "empty output directory")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return usageError{err}
	}
	if flags.NArg() != 0 {
		return usageError{errors.New("no arguments are accepted")}
	}
	for name, value := range map[string]string{"config": *config, "program": o.Program, "namespace": o.Namespace,
		"allowed-signers": o.AllowedSigners, "releases-url": o.ReleasesURL, "version": o.Version, "out": o.Output} {
		if value == "" {
			return usageError{fmt.Errorf("-%s is required", name)}
		}
	}
	c, err := releasebuild.LoadConfig(*config)
	if err != nil {
		return err
	}
	ctx := context.Background()
	if o.SourceDate, err = sourceDate(ctx, o.Version); err != nil {
		return err
	}
	o.Log = os.Stderr
	assets, err := releasebuild.Build(ctx, c, o)
	if err != nil {
		return err
	}
	for _, name := range assets {
		fmt.Println(name)
	}
	return nil
}

func sourceDate(ctx context.Context, version string) (time.Time, error) {
	raw := os.Getenv("SOURCE_DATE_EPOCH")
	if raw == "" {
		out, err := exec.CommandContext(ctx, "git", "show", "-s", "--format=%ct", "refs/tags/"+version+"^{commit}").Output()
		if err != nil {
			return time.Time{}, fmt.Errorf("SOURCE_DATE_EPOCH is unset and the commit time of tag %q is unknown: %w", version, err)
		}
		raw = strings.TrimSpace(string(out))
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid SOURCE_DATE_EPOCH %q", raw)
	}
	return time.Unix(seconds, 0), nil
}
