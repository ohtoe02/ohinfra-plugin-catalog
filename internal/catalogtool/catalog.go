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
	"unicode"
	"unicode/utf8"
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
	Description string        `yaml:"description,omitempty"`
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
	MinimumOHToolsVersion string    `json:"minimum_ohtools_version" yaml:"minimum_ohtools_version"`
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
	commandOwners := map[string]string{}
	for _, entry := range entries {
		if err := ValidateEntry(entry); err != nil {
			return nil, err
		}
		for _, command := range entry.Version.Manifest.Commands {
			key := strings.Join(command.Path, " ")
			if owner, exists := commandOwners[key]; exists && owner != entry.Name {
				return nil, fmt.Errorf(
					"command %q is owned by both %q and %q",
					key,
					owner,
					entry.Name,
				)
			}
			commandOwners[key] = entry.Name
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
	if err := validateDescription(entry.Description); err != nil {
		return fmt.Errorf("plugin description: %w", err)
	}
	if _, err := stableVersion(entry.Version.Version); err != nil {
		return err
	}
	if _, err := stableVersion(entry.Version.MinimumOHToolsVersion); err != nil {
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
	if manifest.Description != entry.Description {
		return errors.New("catalog description must exactly match manifest description")
	}
	if len(manifest.Commands) > 128 {
		return errors.New("manifest command count exceeds 128")
	}
	commands := map[string]struct{}{}
	for _, command := range manifest.Commands {
		if len(command.Path) < 2 {
			return errors.New("plugin command path must contain at least two segments")
		}
		for _, segment := range command.Path {
			if !identifier.MatchString(segment) {
				return fmt.Errorf("invalid command segment %q", segment)
			}
		}
		key := strings.Join(command.Path, " ")
		if _, duplicate := commands[key]; duplicate {
			return fmt.Errorf("duplicate command path %q", key)
		}
		commands[key] = struct{}{}
		if err := validateCommand(command); err != nil {
			return fmt.Errorf("command %q: %w", key, err)
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
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" ||
			parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return errors.New(
				"asset URL must use credential-free HTTPS without query or fragment",
			)
		}
		if err := validateAssetPort(parsed, "asset"); err != nil {
			return err
		}
		expectedURL := fmt.Sprintf(
			"https://github.com/ohtoe02/ohtools-plugins/releases/download/%s-v%s/%s_linux_amd64",
			entry.Name,
			entry.Version.Version,
			entry.Name,
		)
		expectedURLWithExplicitPort := strings.Replace(
			expectedURL,
			"https://github.com/",
			"https://github.com:443/",
			1,
		)
		if asset.URL != expectedURL && asset.URL != expectedURLWithExplicitPort {
			return fmt.Errorf(
				"asset URL must be the immutable first-party release URL %q",
				expectedURL,
			)
		}
	}
	return nil
}

func validateCommand(command Command) error {
	if command.Use != "" {
		if err := validateHelpText("use", command.Use, false); err != nil {
			return err
		}
		fields := strings.Fields(command.Use)
		if len(fields) == 0 || fields[0] != command.Path[len(command.Path)-1] {
			return fmt.Errorf(
				"use must begin with final path segment %q",
				command.Path[len(command.Path)-1],
			)
		}
	}
	if err := validateHelpText("short", command.Short, false); err != nil {
		return err
	}
	if len(command.Arguments) > 64 {
		return errors.New("argument count exceeds 64")
	}
	arguments := map[string]struct{}{}
	optionalSeen := false
	for index, argument := range command.Arguments {
		if !identifier.MatchString(argument.Name) {
			return fmt.Errorf("invalid argument name %q", argument.Name)
		}
		if _, duplicate := arguments[argument.Name]; duplicate {
			return fmt.Errorf("duplicate argument %q", argument.Name)
		}
		arguments[argument.Name] = struct{}{}
		if err := validateHelpText("argument description", argument.Description, true); err != nil {
			return err
		}
		if argument.Required && optionalSeen {
			return fmt.Errorf("required argument %q follows an optional argument", argument.Name)
		}
		if !argument.Required {
			optionalSeen = true
		}
		if argument.Variadic && index != len(command.Arguments)-1 {
			return fmt.Errorf("variadic argument %q must be last", argument.Name)
		}
	}
	if len(command.Flags) > 64 {
		return errors.New("flag count exceeds 64")
	}
	flags := map[string]struct{}{}
	for _, flag := range command.Flags {
		if !identifier.MatchString(flag.Name) {
			return fmt.Errorf("invalid flag name %q", flag.Name)
		}
		if _, reserved := reservedFlags[flag.Name]; reserved {
			return fmt.Errorf("flag %q is reserved by the host", flag.Name)
		}
		if _, duplicate := flags[flag.Name]; duplicate {
			return fmt.Errorf("duplicate flag %q", flag.Name)
		}
		flags[flag.Name] = struct{}{}
		if err := validateHelpText("flag description", flag.Description, true); err != nil {
			return err
		}
		if err := validateFlagDefault(flag); err != nil {
			return err
		}
	}
	return nil
}

func validateHelpText(field, value string, optional bool) error {
	if value == "" && optional {
		return nil
	}
	if !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8", field)
	}
	if len(value) > 512 {
		return fmt.Errorf("%s exceeds 512 bytes", field)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not have surrounding whitespace", field)
	}
	for _, character := range value {
		if unicode.IsControl(character) || character == '\u001b' {
			return fmt.Errorf("%s must be one line without control characters", field)
		}
	}
	return nil
}

var reservedFlags = map[string]struct{}{
	"config": {}, "json": {}, "no-color": {}, "quiet": {}, "verbose": {},
	"debug": {}, "dry-run": {}, "yes": {}, "force": {}, "timeout": {},
	"output": {}, "version": {}, "help": {}, "retry-request-id": {},
}

func validateFlagDefault(flag Flag) error {
	if flag.Default == nil {
		switch flag.Type {
		case "string", "bool", "int", "duration":
			return nil
		default:
			return fmt.Errorf("flag %q has invalid type %q", flag.Name, flag.Type)
		}
	}
	switch flag.Type {
	case "string":
		if _, ok := flag.Default.(string); !ok {
			return fmt.Errorf("flag %q default must be a string", flag.Name)
		}
	case "bool":
		if _, ok := flag.Default.(bool); !ok {
			return fmt.Errorf("flag %q default must be a boolean", flag.Name)
		}
	case "int":
		switch value := flag.Default.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32:
		case uint64:
			if value > uint64(^uint(0)>>1) {
				return fmt.Errorf("flag %q default is outside the host integer range", flag.Name)
			}
		case float64:
			converted := int64(value)
			if float64(converted) != value {
				return fmt.Errorf("flag %q default must be an integer in host range", flag.Name)
			}
		case json.Number:
			if _, err := value.Int64(); err != nil {
				return fmt.Errorf("flag %q default must be an integer in host range", flag.Name)
			}
		default:
			return fmt.Errorf("flag %q default must be an integer", flag.Name)
		}
	case "duration":
		value, ok := flag.Default.(string)
		if !ok {
			return fmt.Errorf("flag %q default must be a duration string", flag.Name)
		}
		if _, err := time.ParseDuration(value); err != nil {
			return fmt.Errorf("flag %q default is not a duration: %w", flag.Name, err)
		}
	default:
		return fmt.Errorf("flag %q has invalid type %q", flag.Name, flag.Type)
	}
	return nil
}

func validateDescription(value string) error {
	if value == "" {
		return nil
	}
	if !utf8.ValidString(value) {
		return errors.New("description must be valid UTF-8")
	}
	if len(value) > 512 {
		return errors.New("description exceeds 512 bytes")
	}
	if strings.TrimSpace(value) != value {
		return errors.New("description must not have surrounding whitespace")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return errors.New("description must be one line without control characters")
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
