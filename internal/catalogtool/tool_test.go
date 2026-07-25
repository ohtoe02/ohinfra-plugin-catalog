package catalogtool

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBuildGroupsAndSortsEntriesDeterministically(t *testing.T) {
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	entries := []Entry{
		validEntry("zeta", "1.1.0", now),
		validEntry("alpha", "2.0.0", now),
		validEntry("zeta", "1.0.0", now),
	}
	first, err := Build(entries, 7, now, now.Add(14*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build([]Entry{entries[2], entries[0], entries[1]}, 7, now, now.Add(14*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("build is not deterministic:\n%s\n%s", first, second)
	}
	var index Index
	if err := json.Unmarshal(first, &index); err != nil {
		t.Fatal(err)
	}
	if index.Plugins[0].Name != "alpha" ||
		index.Plugins[1].Versions[0].Version != "1.0.0" {
		t.Fatalf("index=%#v", index)
	}
}

func TestValidateRejectsPrereleaseHTTPAndManifestMismatch(t *testing.T) {
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	for name, mutate := range map[string]func(*Entry){
		"prerelease": func(entry *Entry) { entry.Version.Version = "1.0.0-rc.1" },
		"http":       func(entry *Entry) { entry.Version.Assets[0].URL = "http://example.com/plugin" },
		"identity":   func(entry *Entry) { entry.Version.Manifest.Version = "9.9.9" },
		"description mismatch": func(entry *Entry) {
			entry.Version.Manifest.Description = "different"
		},
		"description newline": func(entry *Entry) {
			entry.Description = "two\nlines"
			entry.Version.Manifest.Description = "two\nlines"
		},
		"description too long": func(entry *Entry) {
			entry.Description = strings.Repeat("x", 513)
			entry.Version.Manifest.Description = entry.Description
		},
	} {
		t.Run(name, func(t *testing.T) {
			entry := validEntry("sample", "1.0.0", now)
			mutate(&entry)
			if err := ValidateEntry(entry); err == nil {
				t.Fatal("invalid entry accepted")
			}
		})
	}
}

func TestValidateEntryEnforcesProtocolCommandContract(t *testing.T) {
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	tests := map[string]func(*Entry){
		"use path mismatch": func(entry *Entry) {
			entry.Version.Manifest.Commands[0].Use = "different <value>"
		},
		"multiline help": func(entry *Entry) {
			entry.Version.Manifest.Commands[0].Short = "first\nsecond"
		},
		"duplicate command": func(entry *Entry) {
			entry.Version.Manifest.Commands = append(
				entry.Version.Manifest.Commands,
				entry.Version.Manifest.Commands[0],
			)
		},
		"reserved flag": func(entry *Entry) {
			entry.Version.Manifest.Commands[0].Flags = []Flag{{
				Name: "retry-request-id", Type: "string",
			}}
		},
		"duplicate argument": func(entry *Entry) {
			entry.Version.Manifest.Commands[0].Arguments = []Argument{
				{Name: "value", Required: true},
				{Name: "value", Required: true},
			}
		},
		"required after optional": func(entry *Entry) {
			entry.Version.Manifest.Commands[0].Arguments = []Argument{
				{Name: "optional"},
				{Name: "required", Required: true},
			}
		},
		"non-terminal variadic": func(entry *Entry) {
			entry.Version.Manifest.Commands[0].Arguments = []Argument{
				{Name: "values", Variadic: true},
				{Name: "after"},
			}
		},
		"wrong flag default": func(entry *Entry) {
			entry.Version.Manifest.Commands[0].Flags = []Flag{{
				Name: "enabled", Type: "bool", Default: "true",
			}}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			entry := validEntry("sample", "1.0.0", now)
			entry.Version.Manifest.Commands[0].Use = "run"
			entry.Version.Manifest.Commands[0].Short = "Run sample diagnostics"
			mutate(&entry)
			if err := ValidateEntry(entry); err == nil {
				t.Fatal("invalid protocol command accepted")
			}
		})
	}
}

func TestBuildRejectsCrossPackageCommandOwnershipCollision(t *testing.T) {
	now := time.Date(2026, 7, 24, 0, 0, 0, 0, time.UTC)
	first := validEntry("first", "1.0.0", now)
	second := validEntry("second", "1.0.0", now)
	first.Version.Manifest.Commands[0].Path = []string{"shared", "status"}
	first.Version.Manifest.Commands[0].Use = "status"
	second.Version.Manifest.Commands[0].Path = []string{"shared", "status"}
	second.Version.Manifest.Commands[0].Use = "status"

	if _, err := Build(
		[]Entry{first, second},
		7,
		now,
		now.Add(14*24*time.Hour),
	); err == nil || !strings.Contains(err.Error(), "owned by") {
		t.Fatalf("collision error = %v", err)
	}
}

func TestProtocolContractBundleIsCompleteAndChecksummed(t *testing.T) {
	root := filepath.Clean(filepath.Join("..", "..", "contracts", "protocol-v1"))
	required := []string{
		"manifest.schema.json",
		"invocation.schema.json",
		"plan.schema.json",
		"conformance/manifest-valid.json",
		"conformance/manifest-invalid.json",
		"conformance/plan-digest.json",
		"conformance/exit-behavior.json",
	}
	for _, relative := range required {
		encoded, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			t.Errorf("%s: %v", relative, err)
			continue
		}
		var document any
		if err := json.Unmarshal(encoded, &document); err != nil {
			t.Errorf("%s is not JSON: %v", relative, err)
		}
	}
	if err := VerifySHA256SUMS(root); err != nil {
		t.Fatal(err)
	}
	var validVectors struct {
		Cases []struct {
			Name     string   `json:"name"`
			Manifest Manifest `json:"manifest"`
		} `json:"cases"`
	}
	readJSONFile(t, filepath.Join(root, "conformance", "manifest-valid.json"), &validVectors)
	for _, vector := range validVectors.Cases {
		t.Run("valid/"+vector.Name, func(t *testing.T) {
			if err := ValidateEntry(entryForManifest(vector.Manifest)); err != nil {
				t.Fatalf("valid vector rejected: %v", err)
			}
		})
	}
	var invalidVectors struct {
		Cases []struct {
			Name          string   `json:"name"`
			ErrorContains string   `json:"error_contains"`
			Manifest      Manifest `json:"manifest"`
		} `json:"cases"`
	}
	readJSONFile(t, filepath.Join(root, "conformance", "manifest-invalid.json"), &invalidVectors)
	for _, vector := range invalidVectors.Cases {
		t.Run("invalid/"+vector.Name, func(t *testing.T) {
			err := ValidateEntry(entryForManifest(vector.Manifest))
			if err == nil || !strings.Contains(err.Error(), vector.ErrorContains) {
				t.Fatalf("error=%v want substring %q", err, vector.ErrorContains)
			}
		})
	}
	var digestVectors struct {
		Cases []struct {
			Name          string `json:"name"`
			CanonicalJSON string `json:"canonical_json"`
			SHA256        string `json:"sha256"`
		} `json:"cases"`
	}
	readJSONFile(t, filepath.Join(root, "conformance", "plan-digest.json"), &digestVectors)
	for _, vector := range digestVectors.Cases {
		sum := sha256.Sum256([]byte(vector.CanonicalJSON))
		if got := hex.EncodeToString(sum[:]); got != vector.SHA256 {
			t.Errorf("%s digest=%s want=%s", vector.Name, got, vector.SHA256)
		}
	}
}

func TestVerifySHA256SUMSRejectsUnlistedFiles(t *testing.T) {
	root := t.TempDir()
	listed := []byte("{}\n")
	sum := sha256.Sum256(listed)
	if err := os.WriteFile(filepath.Join(root, "listed.json"), listed, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "extra.json"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	line := hex.EncodeToString(sum[:]) + "  listed.json\n"
	if err := os.WriteFile(filepath.Join(root, "SHA256SUMS"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifySHA256SUMS(root); err == nil ||
		!strings.Contains(err.Error(), "not listed") {
		t.Fatalf("unlisted file error = %v", err)
	}
}

func readJSONFile(t *testing.T, path string, target any) {
	t.Helper()
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(encoded, target); err != nil {
		t.Fatal(err)
	}
}

func entryForManifest(manifest Manifest) Entry {
	return Entry{
		Name: manifest.Name, Description: manifest.Description,
		Version: PluginVersion{
			Version: manifest.Version, MinimumOHToolsVersion: "0.3.3",
			PublishedAt: time.Date(2026, 7, 25, 0, 0, 0, 0, time.UTC),
			Manifest:    manifest,
			Assets: []Asset{{
				OS: "linux", Arch: "amd64",
				URL:    "https://github.com/example/plugin/releases/download/v1.0.0/plugin",
				SHA256: strings.Repeat("a", 64), SizeBytes: 1,
			}},
		},
	}
}

func TestSignProducesRotatableEd25519Envelope(t *testing.T) {
	_, first, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	secondPublic, second, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	index := []byte(`{"schema_version":"1","sequence":1}`)
	envelopeBytes, err := Sign(index, []string{
		base64.StdEncoding.EncodeToString(first.Seed()),
		base64.StdEncoding.EncodeToString(second.Seed()),
	})
	if err != nil {
		t.Fatal(err)
	}
	var envelope SignatureEnvelope
	if err := json.Unmarshal(envelopeBytes, &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Signatures) != 2 {
		t.Fatalf("signatures=%#v", envelope.Signatures)
	}
	hash := sha256.Sum256(secondPublic)
	keyID := hex.EncodeToString(hash[:8])
	found := false
	for _, signature := range envelope.Signatures {
		if signature.KeyID == keyID {
			value, decodeErr := base64.StdEncoding.DecodeString(signature.Value)
			if decodeErr != nil || !ed25519.Verify(secondPublic, index, value) {
				t.Fatal("second signature is invalid")
			}
			found = true
		}
	}
	if !found {
		t.Fatal("second key id not present")
	}
}

func TestLoadEntriesUsesStrictYAML(t *testing.T) {
	dir := t.TempDir()
	valid := `name: sample
description: sample plugin
version:
  version: 1.0.0
  minimum_ohtools_version: 0.2.0
  published_at: 2026-07-24T00:00:00Z
  yanked: false
  manifest:
    protocol_version: 1
    name: sample
    version: 1.0.0
    description: sample plugin
    commands:
      - path: [sample, run]
        use: run
        short: run
        category: diagnostic
        arguments: []
        flags: []
        requires_root: false
        requires_force: false
        supports_dry_run: false
        requires_confirmation: false
  assets:
    - os: linux
      arch: amd64
      url: https://github.com/example/sample/releases/download/v1.0.0/sample
      sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      size_bytes: 10
`
	if err := os.WriteFile(filepath.Join(dir, "sample.yaml"), []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := LoadEntries(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%#v error=%v", entries, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte("name: bad\nunknown: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEntries(dir); err == nil {
		t.Fatal("unknown YAML field accepted")
	}
}

func TestLoadEntriesRejectsMissingManifestCommandFields(t *testing.T) {
	dir := t.TempDir()
	entry := `name: sample
description: sample plugin
version:
  version: 1.0.0
  minimum_ohtools_version: 0.3.3
  published_at: 2026-07-25T00:00:00Z
  yanked: false
  manifest:
    protocol_version: 1
    name: sample
    version: 1.0.0
    description: sample plugin
    commands:
      - path: [sample, run]
        use: run
        short: Run sample
        category: diagnostic
        arguments: []
        flags: []
        requires_force: false
        supports_dry_run: false
        requires_confirmation: false
  assets:
    - os: linux
      arch: amd64
      url: https://github.com/example/sample/releases/download/v1.0.0/sample
      sha256: aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
      size_bytes: 10
`
	if err := os.WriteFile(filepath.Join(dir, "sample.yaml"), []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadEntries(dir); err == nil ||
		!strings.Contains(err.Error(), "requires_root") {
		t.Fatalf("missing field error = %v", err)
	}
}

func TestMaterializeVerifiesAssetAndWritesExpectedManifest(t *testing.T) {
	body := []byte("plugin-binary")
	sum := sha256.Sum256(body)
	server := httptest.NewTLSServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, _ = response.Write(body)
	}))
	defer server.Close()
	entry := validEntry("sample", "1.0.0", time.Now().UTC())
	entry.Version.Assets[0].URL = server.URL + "/sample"
	entry.Version.Assets[0].SHA256 = hex.EncodeToString(sum[:])
	entry.Version.Assets[0].SizeBytes = int64(len(body))
	parsed, _ := url.Parse(server.URL)
	output := t.TempDir()
	if err := Materialize(
		context.Background(),
		[]Entry{entry},
		output,
		server.Client(),
		[]string{parsed.Hostname()},
	); err != nil {
		t.Fatal(err)
	}
	binaryPath := filepath.Join(output, "sample", "1.0.0", "plugin")
	if got, err := os.ReadFile(binaryPath); err != nil || string(got) != string(body) {
		t.Fatalf("binary=%q error=%v", got, err)
	}
	manifestPath := filepath.Join(output, "sample", "1.0.0", "manifest.json")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		for _, directory := range []string{
			filepath.Join(output, "sample"),
			filepath.Join(output, "sample", "1.0.0"),
		} {
			info, err := os.Stat(directory)
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0o755 {
				t.Fatalf("verification directory %s mode=%#o, want 0755", directory, got)
			}
		}
	}
}

func TestMaterializeCreatesVerificationDirectoryForEmptyCatalog(t *testing.T) {
	output := filepath.Join(t.TempDir(), "verification")
	if err := Materialize(context.Background(), []Entry{}, output, nil, nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatalf("verification output is not a directory: %s", output)
	}
}

func TestCompareManifestRejectsUnknownOrChangedOutput(t *testing.T) {
	expected := validEntry("sample", "1.0.0", time.Now().UTC()).Version.Manifest
	actual, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	if err := CompareManifest(expected, actual); err != nil {
		t.Fatal(err)
	}
	changed := strings.Replace(string(actual), `"version":"1.0.0"`, `"version":"9.9.9"`, 1)
	if err := CompareManifest(expected, []byte(changed)); err == nil {
		t.Fatal("changed manifest accepted")
	}
	unknown := strings.TrimSuffix(string(actual), "}") + `,"unknown":true}`
	if err := CompareManifest(expected, []byte(unknown)); err == nil {
		t.Fatal("unknown manifest field accepted")
	}
	missingBoolean := strings.Replace(
		string(actual),
		`,"requires_root":false`,
		"",
		1,
	)
	if err := CompareManifest(expected, []byte(missingBoolean)); err == nil {
		t.Fatal("manifest with missing required boolean accepted")
	}
}

func validEntry(name, version string, published time.Time) Entry {
	return Entry{
		Name: name, Description: name + " plugin",
		Version: PluginVersion{
			Version: version, MinimumOHToolsVersion: "0.2.0", PublishedAt: published,
			Manifest: Manifest{
				ProtocolVersion: 1, Name: name, Version: version,
				Description: name + " plugin",
				Commands: []Command{{
					Path: []string{name, "run"}, Category: "diagnostic",
					Arguments: []Argument{}, Flags: []Flag{},
				}},
			},
			Assets: []Asset{{
				OS: "linux", Arch: "amd64",
				URL:    "https://github.com/example/" + name + "/releases/download/v" + version + "/" + name,
				SHA256: strings.Repeat("a", 64), SizeBytes: 10,
			}},
		},
	}
}
