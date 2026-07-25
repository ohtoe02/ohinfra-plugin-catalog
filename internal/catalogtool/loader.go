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
		if err := validateEntryYAMLPresence(encoded); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
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

func validateEntryYAMLPresence(encoded []byte) error {
	var document yaml.Node
	if err := yaml.Unmarshal(encoded, &document); err != nil {
		return err
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return errors.New("catalog entry must be one YAML mapping")
	}
	root := document.Content[0]
	if err := requireYAMLFields(root, "entry", "name", "version"); err != nil {
		return err
	}
	version, _ := yamlMappingValue(root, "version")
	if version.Kind != yaml.MappingNode {
		return errors.New("version must be a YAML mapping")
	}
	if err := requireYAMLFields(
		version,
		"version",
		"version",
		"minimum_ohtools_version",
		"published_at",
		"yanked",
		"manifest",
		"assets",
	); err != nil {
		return err
	}
	manifest, _ := yamlMappingValue(version, "manifest")
	if manifest.Kind != yaml.MappingNode {
		return errors.New("manifest must be a YAML mapping")
	}
	if err := requireYAMLFields(
		manifest,
		"manifest",
		"protocol_version",
		"name",
		"version",
		"commands",
	); err != nil {
		return err
	}
	commands, _ := yamlMappingValue(manifest, "commands")
	if commands.Kind != yaml.SequenceNode {
		return errors.New("manifest.commands must be a YAML sequence")
	}
	for commandIndex, command := range commands.Content {
		context := fmt.Sprintf("manifest.commands[%d]", commandIndex)
		if command.Kind != yaml.MappingNode {
			return fmt.Errorf("%s must be a YAML mapping", context)
		}
		if err := requireYAMLFields(
			command,
			context,
			"path",
			"use",
			"short",
			"category",
			"arguments",
			"flags",
			"requires_root",
			"requires_force",
			"supports_dry_run",
			"requires_confirmation",
		); err != nil {
			return err
		}
		arguments, _ := yamlMappingValue(command, "arguments")
		if err := requireYAMLSequenceFields(
			arguments,
			context+".arguments",
			"name",
			"required",
			"variadic",
		); err != nil {
			return err
		}
		flags, _ := yamlMappingValue(command, "flags")
		if err := requireYAMLSequenceFields(
			flags,
			context+".flags",
			"name",
			"type",
		); err != nil {
			return err
		}
	}
	assets, _ := yamlMappingValue(version, "assets")
	if err := requireYAMLSequenceFields(
		assets,
		"version.assets",
		"os",
		"arch",
		"url",
		"sha256",
		"size_bytes",
	); err != nil {
		return err
	}
	return nil
}

func requireYAMLSequenceFields(
	sequence *yaml.Node,
	context string,
	fields ...string,
) error {
	if sequence == nil || sequence.Kind != yaml.SequenceNode {
		return fmt.Errorf("%s must be a YAML sequence", context)
	}
	for index, value := range sequence.Content {
		itemContext := fmt.Sprintf("%s[%d]", context, index)
		if value.Kind != yaml.MappingNode {
			return fmt.Errorf("%s must be a YAML mapping", itemContext)
		}
		if err := requireYAMLFields(value, itemContext, fields...); err != nil {
			return err
		}
	}
	return nil
}

func requireYAMLFields(mapping *yaml.Node, context string, fields ...string) error {
	for _, field := range fields {
		if _, exists := yamlMappingValue(mapping, field); !exists {
			return fmt.Errorf("missing YAML field %s.%s", context, field)
		}
	}
	return nil
}

func yamlMappingValue(mapping *yaml.Node, key string) (*yaml.Node, bool) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return nil, false
	}
	for index := 0; index+1 < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == key {
			return mapping.Content[index+1], true
		}
	}
	return nil, false
}
