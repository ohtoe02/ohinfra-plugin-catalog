package catalogtool

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const SchemaVersion = "1"

type Index struct {
	SchemaVersion string    `json:"schema_version"`
	Sequence      uint64    `json:"sequence"`
	GeneratedAt   time.Time `json:"generated_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	Plugins       []Plugin  `json:"plugins"`
}

type Entry struct {
	Name        string        `yaml:"name"`
	Description string        `yaml:"description"`
	Homepage    string        `yaml:"homepage,omitempty"`
	Version     PluginVersion `yaml:"version"`
}

type Plugin struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Homepage    string          `json:"homepage,omitempty"`
	Versions    []PluginVersion `json:"versions"`
}

type PluginVersion struct {
	Version               string    `json:"version" yaml:"version"`
	MinimumOHInfraVersion string    `json:"minimum_ohinfra_version" yaml:"minimum_ohinfra_version"`
	PublishedAt           time.Time `json:"published_at" yaml:"published_at"`
	Yanked                bool      `json:"yanked" yaml:"yanked"`
	Manifest              Manifest  `json:"manifest" yaml:"manifest"`
	Assets                []Asset   `json:"assets" yaml:"assets"`
}

type Manifest struct {
	ProtocolVersion int       `json:"protocol_version" yaml:"protocol_version"`
	Name            string    `json:"name" yaml:"name"`
	Version         string    `json:"version" yaml:"version"`
	Description     string    `json:"description,omitempty" yaml:"description,omitempty"`
	Commands        []Command `json:"commands" yaml:"commands"`
}

type Command struct {
	Path                 []string   `json:"path" yaml:"path"`
	Use                  string     `json:"use" yaml:"use"`
	Short                string     `json:"short" yaml:"short"`
	Category             string     `json:"category" yaml:"category"`
	Arguments            []Argument `json:"arguments" yaml:"arguments"`
	Flags                []Flag     `json:"flags" yaml:"flags"`
	RequiresRoot         bool       `json:"requires_root" yaml:"requires_root"`
	RequiresForce        bool       `json:"requires_force" yaml:"requires_force"`
	SupportsDryRun       bool       `json:"supports_dry_run" yaml:"supports_dry_run"`
	RequiresConfirmation bool       `json:"requires_confirmation" yaml:"requires_confirmation"`
}

type Argument struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Required    bool   `json:"required" yaml:"required"`
	Variadic    bool   `json:"variadic" yaml:"variadic"`
}

type Flag struct {
	Name        string `json:"name" yaml:"name"`
	Type        string `json:"type" yaml:"type"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Default     any    `json:"default,omitempty" yaml:"default,omitempty"`
}

type Asset struct {
	OS        string `json:"os" yaml:"os"`
	Arch      string `json:"arch" yaml:"arch"`
	URL       string `json:"url" yaml:"url"`
	SHA256    string `json:"sha256" yaml:"sha256"`
	SizeBytes int64  `json:"size_bytes" yaml:"size_bytes"`
}

type SignatureEnvelope struct {
	SchemaVersion string      `json:"schema_version"`
	Signatures    []Signature `json:"signatures"`
}

type Signature struct {
	KeyID     string `json:"key_id"`
	Algorithm string `json:"algorithm"`
	Value     string `json:"value"`
}

func Build(entries []Entry, sequence uint64, generatedAt, expiresAt time.Time) ([]byte, error) {
	if sequence == 0 || generatedAt.IsZero() || !expiresAt.After(generatedAt) {
		return nil, errors.New("invalid catalog release metadata")
	}
	grouped := map[string]*Plugin{}
	for _, entry := range entries {
		if err := ValidateEntry(entry); err != nil {
			return nil, err
		}
		item, exists := grouped[entry.Name]
		if !exists {
			item = &Plugin{
				Name: entry.Name, Description: entry.Description, Homepage: entry.Homepage,
				Versions: []PluginVersion{},
			}
			grouped[entry.Name] = item
		} else if item.Description != entry.Description || item.Homepage != entry.Homepage {
			return nil, fmt.Errorf("plugin %q metadata differs between versions", entry.Name)
		}
		for _, existing := range item.Versions {
			if existing.Version == entry.Version.Version {
				return nil, fmt.Errorf("plugin %q has duplicate version %q", entry.Name, entry.Version.Version)
			}
		}
		item.Versions = append(item.Versions, entry.Version)
	}
	names := make([]string, 0, len(grouped))
	for name := range grouped {
		names = append(names, name)
	}
	sort.Strings(names)
	plugins := make([]Plugin, 0, len(names))
	for _, name := range names {
		item := grouped[name]
		sort.Slice(item.Versions, func(i, j int) bool {
			return compare(parseVersion(item.Versions[i].Version), parseVersion(item.Versions[j].Version)) < 0
		})
		plugins = append(plugins, *item)
	}
	index := Index{
		SchemaVersion: SchemaVersion, Sequence: sequence,
		GeneratedAt: generatedAt.UTC(), ExpiresAt: expiresAt.UTC(), Plugins: plugins,
	}
	encoded, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func Sign(index []byte, encodedSeeds []string) ([]byte, error) {
	if len(encodedSeeds) == 0 {
		return nil, errors.New("at least one signing key is required")
	}
	signatures := make([]Signature, 0, len(encodedSeeds))
	for _, encoded := range encodedSeeds {
		seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
		if err != nil || len(seed) != ed25519.SeedSize {
			return nil, errors.New("signing key must be a base64 Ed25519 seed")
		}
		privateKey := ed25519.NewKeyFromSeed(seed)
		publicKey := privateKey.Public().(ed25519.PublicKey)
		hash := sha256.Sum256(publicKey)
		signatures = append(signatures, Signature{
			KeyID: hex.EncodeToString(hash[:8]), Algorithm: "ed25519",
			Value: base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, index)),
		})
	}
	sort.Slice(signatures, func(i, j int) bool { return signatures[i].KeyID < signatures[j].KeyID })
	encoded, err := json.MarshalIndent(SignatureEnvelope{
		SchemaVersion: SchemaVersion, Signatures: signatures,
	}, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func ValidateEntry(entry Entry) error {
	if !identifier.MatchString(entry.Name) {
		return fmt.Errorf("invalid plugin name %q", entry.Name)
	}
	if _, err := stableVersion(entry.Version.Version); err != nil {
		return err
	}
	if _, err := stableVersion(entry.Version.MinimumOHInfraVersion); err != nil {
		return err
	}
	if entry.Version.PublishedAt.IsZero() {
		return errors.New("published_at is required")
	}
	manifest := entry.Version.Manifest
	if manifest.ProtocolVersion != 1 || manifest.Name != entry.Name ||
		manifest.Version != entry.Version.Version || len(manifest.Commands) == 0 {
		return errors.New("manifest identity or protocol is invalid")
	}
	for _, command := range manifest.Commands {
		if len(command.Path) < 2 {
			return errors.New("plugin command path must contain at least two segments")
		}
		for _, segment := range command.Path {
			if !identifier.MatchString(segment) {
				return fmt.Errorf("invalid command segment %q", segment)
			}
		}
		switch command.Category {
		case "diagnostic":
		case "operational", "runbook":
			if !command.SupportsDryRun {
				return errors.New("mutating commands must support dry-run")
			}
		default:
			return fmt.Errorf("invalid command category %q", command.Category)
		}
	}
	if len(entry.Version.Assets) == 0 {
		return errors.New("version has no assets")
	}
	platforms := map[string]struct{}{}
	for _, asset := range entry.Version.Assets {
		if asset.OS != "linux" || asset.Arch != "amd64" || asset.SizeBytes <= 0 || asset.SizeBytes > 100<<20 {
			return errors.New("asset platform or size is invalid")
		}
		platform := asset.OS + "/" + asset.Arch
		if _, duplicate := platforms[platform]; duplicate {
			return fmt.Errorf("duplicate asset %s", platform)
		}
		platforms[platform] = struct{}{}
		digest, err := hex.DecodeString(asset.SHA256)
		if err != nil || len(digest) != sha256.Size {
			return errors.New("asset SHA-256 is invalid")
		}
		parsed, err := url.Parse(asset.URL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return errors.New("asset URL must use credential-free HTTPS")
		}
	}
	return nil
}

type version struct {
	major int
	minor int
	patch int
}

func stableVersion(value string) (version, error) {
	if strings.ContainsAny(value, "-+") {
		return version{}, fmt.Errorf("%q is not a stable semantic version", value)
	}
	parts := strings.Split(strings.TrimPrefix(value, "v"), ".")
	if len(parts) != 3 {
		return version{}, fmt.Errorf("%q must use X.Y.Z", value)
	}
	output := version{}
	targets := []*int{&output.major, &output.minor, &output.patch}
	for index, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return version{}, fmt.Errorf("%q is invalid", value)
		}
		parsed, err := strconv.Atoi(part)
		if err != nil || parsed < 0 {
			return version{}, fmt.Errorf("%q is invalid", value)
		}
		*targets[index] = parsed
	}
	return output, nil
}

func parseVersion(value string) version {
	parsed, _ := stableVersion(value)
	return parsed
}

func compare(left, right version) int {
	if left.major != right.major {
		return left.major - right.major
	}
	if left.minor != right.minor {
		return left.minor - right.minor
	}
	return left.patch - right.patch
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
