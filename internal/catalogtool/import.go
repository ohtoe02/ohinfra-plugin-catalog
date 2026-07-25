package catalogtool

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

const releaseMetadataLimit = 1 << 20

// ReleaseMetadata is the strict, language-neutral sidecar emitted next to a
// plugin release asset. It is converted to the existing catalog entry v1
// shape; it does not introduce a new signed catalog format.
type ReleaseMetadata struct {
	SchemaVersion         string    `json:"schema_version"`
	Name                  string    `json:"name"`
	Description           string    `json:"description,omitempty"`
	Homepage              string    `json:"homepage,omitempty"`
	Version               string    `json:"version"`
	MinimumOHToolsVersion string    `json:"minimum_ohtools_version"`
	PublishedAt           time.Time `json:"published_at"`
	Asset                 Asset     `json:"asset"`
	Manifest              Manifest  `json:"manifest"`
}

func ImportRelease(metadataPath, binaryPath, pluginsDirectory string) (string, error) {
	if runtime.GOOS != "linux" {
		return "", errors.New("production release import is supported only on Linux")
	}
	return ImportReleaseWithSandbox(
		metadataPath,
		binaryPath,
		pluginsDirectory,
		DefaultDockerManifestSandbox(),
	)
}

func ImportReleaseWithSandbox(
	metadataPath,
	binaryPath,
	pluginsDirectory string,
	sandbox ManifestSandbox,
) (string, error) {
	if sandbox == nil {
		return "", errors.New("release manifest sandbox is required")
	}
	if runtime.GOOS != "linux" {
		if _, testOnly := sandbox.(testOnlyManifestSandbox); !testOnly {
			return "", errors.New("production release import is supported only on Linux")
		}
	}
	metadataBytes, err := readRegularFile(metadataPath, releaseMetadataLimit)
	if err != nil {
		return "", fmt.Errorf("release metadata: %w", err)
	}
	if err := rejectDuplicateJSONFields(metadataBytes); err != nil {
		return "", fmt.Errorf("decode release metadata: %w", err)
	}
	if err := validateReleaseMetadataPresence(metadataBytes); err != nil {
		return "", fmt.Errorf("decode release metadata: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(metadataBytes))
	decoder.DisallowUnknownFields()
	var metadata ReleaseMetadata
	if err := decoder.Decode(&metadata); err != nil {
		return "", fmt.Errorf("decode release metadata: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return "", errors.New("decode release metadata: multiple JSON documents")
		}
		return "", fmt.Errorf("decode release metadata: %w", err)
	}
	if metadata.SchemaVersion != "1" {
		return "", fmt.Errorf("unsupported release metadata schema %q", metadata.SchemaVersion)
	}
	binary, err := readRegularFile(binaryPath, 100<<20)
	if err != nil {
		return "", fmt.Errorf("release binary: %w", err)
	}
	sum := sha256.Sum256(binary)
	if int64(len(binary)) != metadata.Asset.SizeBytes {
		return "", fmt.Errorf(
			"release binary size mismatch: expected %d, got %d",
			metadata.Asset.SizeBytes,
			len(binary),
		)
	}
	if got := hex.EncodeToString(sum[:]); !strings.EqualFold(got, metadata.Asset.SHA256) {
		return "", errors.New("release binary SHA-256 mismatch")
	}
	entry := Entry{
		Name: metadata.Name, Description: metadata.Description, Homepage: metadata.Homepage,
		Version: PluginVersion{
			Version: metadata.Version, MinimumOHToolsVersion: metadata.MinimumOHToolsVersion,
			PublishedAt: metadata.PublishedAt, Yanked: false, Manifest: metadata.Manifest,
			Assets: []Asset{metadata.Asset},
		},
	}
	if err := ValidateEntry(entry); err != nil {
		return "", fmt.Errorf("release metadata: %w", err)
	}
	if err := validateOutputRoot(pluginsDirectory); err != nil {
		return "", err
	}
	output := filepath.Join(pluginsDirectory, entry.Name, entry.Version.Version+".yaml")
	relative, err := filepath.Rel(pluginsDirectory, output)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("release metadata resolves outside plugins directory")
	}
	ctx, cancel := context.WithTimeout(context.Background(), releaseManifestTimeout)
	defer cancel()
	actualManifest, err := sandbox.Manifest(ctx, binary)
	if err != nil {
		return "", fmt.Errorf("release binary manifest sandbox: %w", err)
	}
	if len(actualManifest) > releaseManifestOutputLimit {
		return "", fmt.Errorf("release binary manifest exceeds %d bytes", releaseManifestOutputLimit)
	}
	if err := CompareManifest(metadata.Manifest, actualManifest); err != nil {
		return "", fmt.Errorf("release binary manifest: %w", err)
	}
	encoded, err := yaml.Marshal(entry)
	if err != nil {
		return "", err
	}
	if err := writeCatalogEntryAtomic(
		pluginsDirectory,
		entry.Name,
		entry.Version.Version+".yaml",
		encoded,
	); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("catalog entry %s already exists: %w", output, os.ErrExist)
		}
		return "", err
	}
	return output, nil
}

func validateReleaseMetadataPresence(encoded []byte) error {
	var root map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &root); err != nil {
		return err
	}
	if err := requireJSONFields(root, "release metadata",
		"schema_version", "name", "version", "minimum_ohtools_version",
		"published_at", "asset", "manifest",
	); err != nil {
		return err
	}
	var asset map[string]json.RawMessage
	if err := json.Unmarshal(root["asset"], &asset); err != nil {
		return fmt.Errorf("asset: %w", err)
	}
	if err := requireJSONFields(
		asset,
		"asset",
		"os",
		"arch",
		"url",
		"sha256",
		"size_bytes",
	); err != nil {
		return err
	}
	return validateManifestPresence(root["manifest"])
}

func validateManifestPresence(encoded []byte) error {
	var manifest map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &manifest); err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	if err := requireJSONFields(
		manifest,
		"manifest",
		"protocol_version",
		"name",
		"version",
		"commands",
	); err != nil {
		return err
	}
	var commands []json.RawMessage
	if err := json.Unmarshal(manifest["commands"], &commands); err != nil {
		return fmt.Errorf("manifest.commands: %w", err)
	}
	for commandIndex, encodedCommand := range commands {
		var command map[string]json.RawMessage
		if err := json.Unmarshal(encodedCommand, &command); err != nil {
			return fmt.Errorf("manifest.commands[%d]: %w", commandIndex, err)
		}
		context := fmt.Sprintf("manifest.commands[%d]", commandIndex)
		if err := requireJSONFields(
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
		if err := validateNestedPresence(
			command["arguments"],
			context+".arguments",
			"name",
			"required",
			"variadic",
		); err != nil {
			return err
		}
		if err := validateNestedPresence(
			command["flags"],
			context+".flags",
			"name",
			"type",
		); err != nil {
			return err
		}
		if err := rejectExplicitNullJSONField(
			command["flags"],
			context+".flags",
			"default",
		); err != nil {
			return err
		}
	}
	return nil
}

func rejectExplicitNullJSONField(
	encoded json.RawMessage,
	context,
	field string,
) error {
	var values []json.RawMessage
	if err := json.Unmarshal(encoded, &values); err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}
	for index, encodedValue := range values {
		var value map[string]json.RawMessage
		if err := json.Unmarshal(encodedValue, &value); err != nil {
			return fmt.Errorf("%s[%d]: %w", context, index, err)
		}
		if raw, present := value[field]; present &&
			bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return fmt.Errorf("%s[%d].%s must be omitted instead of null", context, index, field)
		}
	}
	return nil
}

func validateNestedPresence(encoded json.RawMessage, context string, required ...string) error {
	var values []json.RawMessage
	if err := json.Unmarshal(encoded, &values); err != nil {
		return fmt.Errorf("%s: %w", context, err)
	}
	for index, encodedValue := range values {
		var value map[string]json.RawMessage
		if err := json.Unmarshal(encodedValue, &value); err != nil {
			return fmt.Errorf("%s[%d]: %w", context, index, err)
		}
		if err := requireJSONFields(
			value,
			fmt.Sprintf("%s[%d]", context, index),
			required...,
		); err != nil {
			return err
		}
	}
	return nil
}

func requireJSONFields(
	object map[string]json.RawMessage,
	context string,
	fields ...string,
) error {
	for _, field := range fields {
		if _, present := object[field]; !present {
			return fmt.Errorf("missing JSON field %s.%s", context, field)
		}
	}
	return nil
}

func rejectDuplicateJSONFields(encoded []byte) error {
	if !utf8.Valid(encoded) {
		return errors.New("JSON document must be valid UTF-8")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	if err := inspectJSONValue(decoder); err != nil {
		return err
	}
	if token, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("trailing JSON token %v", token)
		}
		return err
	}
	return nil
}

func inspectJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("JSON object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON field %q", key)
			}
			seen[key] = struct{}{}
			if err := inspectJSONValue(decoder); err != nil {
				return err
			}
		}
		closeToken, err := decoder.Token()
		if err != nil {
			return err
		}
		if closeToken != json.Delim('}') {
			return errors.New("JSON object is not closed")
		}
	case '[':
		for decoder.More() {
			if err := inspectJSONValue(decoder); err != nil {
				return err
			}
		}
		closeToken, err := decoder.Token()
		if err != nil {
			return err
		}
		if closeToken != json.Delim(']') {
			return errors.New("JSON array is not closed")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	return nil
}

func validateOutputRoot(root string) error {
	if strings.TrimSpace(root) == "" {
		return errors.New("plugins directory is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	if filepath.Clean(absolute) == filepath.VolumeName(absolute)+string(filepath.Separator) {
		return errors.New("plugins directory must not be a filesystem root")
	}
	return nil
}

type testOnlyManifestSandbox interface {
	testOnlyManifestSandbox()
}
