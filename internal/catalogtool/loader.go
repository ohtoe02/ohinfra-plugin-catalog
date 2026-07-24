package catalogtool

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"go.yaml.in/yaml/v3"
)

func LoadEntries(directory string) ([]Entry, error) {
	paths := []string{}
	err := filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() && (filepath.Ext(path) == ".yaml" || filepath.Ext(path) == ".yml") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	entries := make([]Entry, 0, len(paths))
	for _, path := range paths {
		encoded, readErr := os.ReadFile(path) // #nosec G304 -- files are discovered inside the requested catalog tree.
		if readErr != nil {
			return nil, readErr
		}
		decoder := yaml.NewDecoder(bytes.NewReader(encoded))
		decoder.KnownFields(true)
		var entry Entry
		if err := decoder.Decode(&entry); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		var extra any
		if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
			if err == nil {
				return nil, fmt.Errorf("%s: multiple YAML documents are not allowed", path)
			}
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if err := ValidateEntry(entry); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}
