package catalogtool

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAuthorDocumentationLayout(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", ".."))
	required := []string{
		"docs/plugin-authoring.md",
		"docs/protocol-v1.md",
		"docs/catalog-entry.md",
		"docs/validation.md",
		"examples/minimal-go/go.mod",
		"examples/minimal-go/main.go",
		"examples/minimal-go/main_test.go",
		"examples/minimal-go/Makefile",
		"examples/minimal-go/.goreleaser.yaml",
		"examples/minimal-go/.github/workflows/release.yml",
		"examples/minimal-go/README.md",
		"examples/minimal-go/testdata/invocation.json",
		"examples/minimal-go/testdata/manifest.json",
		"examples/minimal-go/catalog/example-plugin/1.0.0.yaml",
	}
	for _, name := range required {
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Errorf("required authoring file %s: %v", name, err)
			continue
		}
		if !info.Mode().IsRegular() {
			t.Errorf("required authoring path %s is not a regular file", name)
		}
	}

	readme, err := os.ReadFile(filepath.Join(root, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(readme), "docs/plugin-authoring.md") {
		t.Error("README must link to docs/plugin-authoring.md")
	}

	exampleEntry := filepath.Join(root, "examples", "minimal-go", "catalog", "example-plugin", "1.0.0.yaml")
	relative, err := filepath.Rel(filepath.Join(root, "plugins"), exampleEntry)
	if err != nil {
		t.Fatal(err)
	}
	if relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Error("example catalog entry must not be stored under the production plugins directory")
	}
}

func TestExampleCatalogManifestMatchesGolden(t *testing.T) {
	t.Parallel()

	root := filepath.Clean(filepath.Join("..", ".."))
	entries, err := LoadEntries(filepath.Join(
		root, "examples", "minimal-go", "catalog",
	))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("entry count = %d, want 1", len(entries))
	}

	encoded, err := os.ReadFile(filepath.Join(
		root, "examples", "minimal-go", "testdata", "manifest.json",
	))
	if err != nil {
		t.Fatal(err)
	}
	var golden Manifest
	if err := json.Unmarshal(encoded, &golden); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(entries[0].Version.Manifest, golden) {
		t.Fatalf("catalog manifest does not match example golden\ncatalog: %#v\ngolden: %#v",
			entries[0].Version.Manifest, golden)
	}
}
