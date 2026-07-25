package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ohtoe02/ohtools-plugin-catalog/internal/catalogtool"
)

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

func run(
	arguments []string,
	getenv func(string) string,
	stdout io.Writer,
	stderr io.Writer,
) int {
	if len(arguments) == 0 {
		_, _ = fmt.Fprintln(
			stderr,
			"usage: catalogctl <validate|build|sign|materialize|compare-manifest>",
		)
		return 2
	}
	switch arguments[0] {
	case "validate":
		flags := flag.NewFlagSet("validate", flag.ContinueOnError)
		flags.SetOutput(stderr)
		plugins := flags.String("plugins", "plugins", "catalog entries directory")
		if err := flags.Parse(arguments[1:]); err != nil {
			return 2
		}
		entries, err := catalogtool.LoadEntries(*plugins)
		if err == nil {
			_, err = catalogtool.Build(entries, 1, time.Unix(1, 0).UTC(), time.Unix(2, 0).UTC())
		}
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		_, _ = fmt.Fprintf(stdout, "validated %d catalog entries\n", len(entries))
		return 0
	case "build":
		flags := flag.NewFlagSet("build", flag.ContinueOnError)
		flags.SetOutput(stderr)
		plugins := flags.String("plugins", "plugins", "catalog entries directory")
		output := flags.String("output", "dist/index-v1.json", "generated index path")
		sequence := flags.String("sequence", "", "positive catalog sequence")
		generated := flags.String("generated-at", "", "RFC3339 generation time")
		expires := flags.String("expires-at", "", "RFC3339 expiration time")
		if err := flags.Parse(arguments[1:]); err != nil {
			return 2
		}
		parsedSequence, err := strconv.ParseUint(*sequence, 10, 64)
		if err != nil || parsedSequence == 0 {
			_, _ = fmt.Fprintln(stderr, "sequence must be a positive integer")
			return 2
		}
		generatedAt, err := time.Parse(time.RFC3339, *generated)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "generated-at must use RFC3339")
			return 2
		}
		expiresAt, err := time.Parse(time.RFC3339, *expires)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "expires-at must use RFC3339")
			return 2
		}
		entries, err := catalogtool.LoadEntries(*plugins)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		encoded, err := catalogtool.Build(entries, parsedSequence, generatedAt, expiresAt)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		if err := writeOutput(*output, encoded); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	case "sign":
		flags := flag.NewFlagSet("sign", flag.ContinueOnError)
		flags.SetOutput(stderr)
		indexPath := flags.String("index", "dist/index-v1.json", "index to sign")
		output := flags.String("output", "dist/index-v1.json.sig", "signature envelope path")
		keyEnvironment := flags.String("key-env", "CATALOG_SIGNING_KEYS", "environment containing base64 seeds")
		if err := flags.Parse(arguments[1:]); err != nil {
			return 2
		}
		index, err := os.ReadFile(*indexPath) // #nosec G304 -- path is an explicit maintainer CLI input.
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		keys := strings.FieldsFunc(getenv(*keyEnvironment), func(character rune) bool {
			return character == ',' || character == ';' || character == '\n' || character == '\r'
		})
		envelope, err := catalogtool.Sign(index, keys)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		if err := writeOutput(*output, envelope); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	case "materialize":
		flags := flag.NewFlagSet("materialize", flag.ContinueOnError)
		flags.SetOutput(stderr)
		plugins := flags.String("plugins", "plugins", "catalog entries directory")
		output := flags.String("output", "verification", "verified assets directory")
		allowed := hostList{
			"github.com",
			"release-assets.githubusercontent.com",
			"objects.githubusercontent.com",
		}
		flags.Var(&allowed, "allowed-host", "allowed asset host; may be repeated")
		if err := flags.Parse(arguments[1:]); err != nil {
			return 2
		}
		entries, err := catalogtool.LoadEntries(*plugins)
		if err == nil {
			err = catalogtool.Materialize(context.Background(), entries, *output, nil, allowed)
		}
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	case "compare-manifest":
		flags := flag.NewFlagSet("compare-manifest", flag.ContinueOnError)
		flags.SetOutput(stderr)
		expectedPath := flags.String("expected", "", "expected manifest JSON")
		actualPath := flags.String("actual", "", "actual plugin manifest JSON")
		if err := flags.Parse(arguments[1:]); err != nil {
			return 2
		}
		expectedBytes, err := os.ReadFile(*expectedPath) // #nosec G304 -- maintainer CLI input.
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		var expected catalogtool.Manifest
		if err := json.Unmarshal(expectedBytes, &expected); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		actual, err := os.ReadFile(*actualPath) // #nosec G304 -- maintainer CLI input.
		if err == nil {
			err = catalogtool.CompareManifest(expected, actual)
		}
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
		return 0
	default:
		_, _ = fmt.Fprintf(stderr, "unknown command %q\n", arguments[0])
		return 2
	}
}

type hostList []string

func (hosts *hostList) String() string {
	return strings.Join(*hosts, ",")
}

func (hosts *hostList) Set(value string) error {
	*hosts = append(*hosts, value)
	return nil
}

func writeOutput(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".catalogctl-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := temp.Chmod(0o644); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
