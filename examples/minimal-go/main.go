package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const protocolVersion = 1

var (
	version     = "1.0.0"
	defaultRoot = "/tmp/ohtools-example"
)

type Manifest struct {
	ProtocolVersion int       `json:"protocol_version"`
	Name            string    `json:"name"`
	Version         string    `json:"version"`
	Description     string    `json:"description,omitempty"`
	Commands        []Command `json:"commands"`
}

type Command struct {
	Path                 []string   `json:"path"`
	Use                  string     `json:"use"`
	Short                string     `json:"short"`
	Category             string     `json:"category"`
	Arguments            []Argument `json:"arguments"`
	Flags                []Flag     `json:"flags"`
	RequiresRoot         bool       `json:"requires_root"`
	RequiresForce        bool       `json:"requires_force"`
	SupportsDryRun       bool       `json:"supports_dry_run"`
	RequiresConfirmation bool       `json:"requires_confirmation"`
}

type Argument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required"`
	Variadic    bool   `json:"variadic"`
}

type Flag struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Default     any    `json:"default,omitempty"`
}

type Invocation struct {
	ProtocolVersion int            `json:"protocol_version"`
	RequestID       string         `json:"request_id"`
	CommandPath     []string       `json:"command_path"`
	Arguments       []string       `json:"arguments"`
	Options         map[string]any `json:"options"`
	Deadline        string         `json:"deadline,omitempty"`
	PlanDigest      string         `json:"plan_digest,omitempty"`
}

type Plan struct {
	CommandID            string   `json:"command_id"`
	Summary              string   `json:"summary"`
	Checks               []Check  `json:"checks"`
	Changes              []Change `json:"changes"`
	Risks                []string `json:"risks"`
	RequiresRoot         bool     `json:"requires_root"`
	RequiresForce        bool     `json:"requires_force"`
	RequiresConfirmation bool     `json:"requires_confirmation"`
}

type Result struct {
	SchemaVersion string            `json:"schema_version"`
	Command       string            `json:"command"`
	Status        string            `json:"status"`
	Timestamp     string            `json:"timestamp"`
	DurationMS    int64             `json:"duration_ms"`
	Host          string            `json:"host"`
	Tool          Tool              `json:"tool"`
	Checks        []Check           `json:"checks"`
	Data          map[string]any    `json:"data"`
	Changes       []Change          `json:"changes"`
	Errors        []StructuredError `json:"errors"`
}

type Tool struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Commit       string `json:"commit"`
	BuildDate    string `json:"build_date"`
	GoVersion    string `json:"go_version"`
	Architecture string `json:"architecture"`
}

type Check struct {
	ID      string         `json:"id"`
	Status  string         `json:"status"`
	Summary string         `json:"summary"`
	Details map[string]any `json:"details,omitempty"`
}

type Change struct {
	Object  string         `json:"object"`
	Action  string         `json:"action"`
	Status  string         `json:"status"`
	Details map[string]any `json:"details,omitempty"`
}

type StructuredError struct {
	Kind       string         `json:"kind"`
	Code       string         `json:"code,omitempty"`
	Message    string         `json:"message"`
	Dependency string         `json:"dependency,omitempty"`
	Retryable  bool           `json:"retryable"`
	Details    map[string]any `json:"details,omitempty"`
}

func main() {
	os.Exit(run(os.Args, os.Stdin, os.Stdout, os.Stderr, defaultRoot, time.Now))
}

func run(
	args []string,
	stdin io.Reader,
	stdout io.Writer,
	stderr io.Writer,
	root string,
	now func() time.Time,
) int {
	if len(args) != 3 || args[2] != "--protocol=1" {
		_, _ = fmt.Fprintln(stderr, "usage: example-plugin <manifest|plan|execute> --protocol=1")
		return 2
	}

	encoder := json.NewEncoder(stdout)
	switch args[1] {
	case "manifest":
		if err := encoder.Encode(pluginManifest()); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
	case "plan":
		invocation, err := readInvocation(stdin)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 2
		}
		plan, err := buildPlan(invocation, root)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 2
		}
		if err := encoder.Encode(plan); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
	case "execute":
		invocation, err := readInvocation(stdin)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 2
		}
		result, err := execute(invocation, root, now)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 2
		}
		if err := encoder.Encode(result); err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 1
		}
	default:
		_, _ = fmt.Fprintln(stderr, "unsupported protocol verb")
		return 2
	}
	return 0
}

func pluginManifest() Manifest {
	return Manifest{
		ProtocolVersion: protocolVersion,
		Name:            "example-plugin",
		Version:         version,
		Description:     "Minimal reference implementation for ohtools plugin protocol v1",
		Commands: []Command{
			{
				Path:     []string{"example", "echo"},
				Use:      "echo <text>",
				Short:    "Echo text through plugin protocol v1",
				Category: "diagnostic",
				Arguments: []Argument{{
					Name: "text", Description: "Text to echo", Required: true,
				}},
				Flags: []Flag{{
					Name: "uppercase", Type: "bool",
					Description: "Convert the echoed text to uppercase", Default: false,
				}},
			},
			{
				Path:     []string{"example", "write"},
				Use:      "write <text>",
				Short:    "Write a message under /tmp/ohtools-example",
				Category: "operational",
				Arguments: []Argument{{
					Name: "text", Description: "Message to write", Required: true,
				}},
				Flags:                []Flag{},
				SupportsDryRun:       true,
				RequiresConfirmation: true,
			},
		},
	}
}

func readInvocation(input io.Reader) (Invocation, error) {
	decoder := json.NewDecoder(io.LimitReader(input, 1<<20))
	decoder.DisallowUnknownFields()
	var invocation Invocation
	if err := decoder.Decode(&invocation); err != nil {
		return Invocation{}, fmt.Errorf("decode invocation: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Invocation{}, errors.New("decode invocation: trailing JSON document")
		}
		return Invocation{}, fmt.Errorf("decode invocation: %w", err)
	}
	if invocation.ProtocolVersion != protocolVersion {
		return Invocation{}, fmt.Errorf("unsupported protocol version %d", invocation.ProtocolVersion)
	}
	if invocation.CommandPath == nil {
		invocation.CommandPath = []string{}
	}
	if invocation.Arguments == nil {
		invocation.Arguments = []string{}
	}
	if invocation.Options == nil {
		invocation.Options = map[string]any{}
	}
	return invocation, nil
}

func buildPlan(invocation Invocation, root string) (Plan, error) {
	if strings.Join(invocation.CommandPath, " ") != "example write" {
		return Plan{}, errors.New("plan is supported only for example write")
	}
	if len(invocation.Arguments) != 1 {
		return Plan{}, errors.New("example write requires exactly one text argument")
	}
	target := filepath.Join(root, "message.txt")
	desired := []byte(invocation.Arguments[0] + "\n")
	if current, err := os.ReadFile(target); err == nil {
		if bytes.Equal(current, desired) {
			return Plan{
				CommandID:            "example.write",
				Summary:              "Example message already has the desired content",
				Checks:               []Check{},
				Changes:              []Change{},
				Risks:                []string{},
				RequiresConfirmation: true,
			}, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Plan{}, fmt.Errorf("inspect example message: %w", err)
	}
	return Plan{
		CommandID: "example.write",
		Summary:   "Write the example message file",
		Checks:    []Check{},
		Changes: []Change{{
			Object: target,
			Action: "write",
			Status: "planned",
			Details: map[string]any{
				"mode": "0600",
			},
		}},
		Risks:                []string{"Replaces the previous example message"},
		RequiresConfirmation: true,
	}, nil
}

func planDigest(plan Plan) (string, error) {
	encoded, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("encode operation plan: %w", err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

func execute(invocation Invocation, root string, now func() time.Time) (Result, error) {
	switch strings.Join(invocation.CommandPath, " ") {
	case "example echo":
		if len(invocation.Arguments) != 1 {
			return Result{}, errors.New("example echo requires exactly one text argument")
		}
		text := invocation.Arguments[0]
		if uppercase, ok := invocation.Options["uppercase"]; ok {
			value, valid := uppercase.(bool)
			if !valid {
				return Result{}, errors.New("uppercase must be a boolean")
			}
			if value {
				text = strings.ToUpper(text)
			}
		}
		return canonicalResult("example echo", now(), map[string]any{"echo": text}), nil
	case "example write":
		plan, err := buildPlan(invocation, root)
		if err != nil {
			return Result{}, err
		}
		expected, err := planDigest(plan)
		if err != nil {
			return Result{}, err
		}
		if invocation.PlanDigest == "" || invocation.PlanDigest != expected {
			return Result{}, errors.New("plan digest does not match the approved plan")
		}
		target := filepath.Join(root, "message.txt")
		if len(plan.Changes) == 0 {
			return canonicalResult("example write", now(), map[string]any{
				"changed": false,
				"reason":  "already_desired",
				"path":    target,
			}), nil
		}
		if err := atomicWrite(target, []byte(invocation.Arguments[0]+"\n")); err != nil {
			return Result{}, err
		}
		result := canonicalResult("example write", now(), map[string]any{
			"changed": true,
			"reason":  "desired_state_applied",
			"path":    target,
		})
		result.Changes = []Change{{
			Object: target,
			Action: "write",
			Status: "applied",
			Details: map[string]any{
				"mode": "0600",
			},
		}}
		return result, nil
	default:
		return Result{}, errors.New("unsupported command path")
	}
}

func canonicalResult(command string, timestamp time.Time, data map[string]any) Result {
	return Result{
		SchemaVersion: "1.0",
		Command:       command,
		Status:        "pass",
		Timestamp:     timestamp.UTC().Format(time.RFC3339Nano),
		DurationMS:    0,
		Host:          "",
		Tool: Tool{
			Name: "example-plugin", Version: version,
		},
		Checks:  []Check{},
		Data:    data,
		Changes: []Change{},
		Errors:  []StructuredError{},
	}
}

func atomicWrite(target string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return fmt.Errorf("create example directory: %w", err)
	}
	file, err := os.CreateTemp(filepath.Dir(target), ".message-*")
	if err != nil {
		return fmt.Errorf("create staged message: %w", err)
	}
	staged := file.Name()
	defer func() {
		_ = os.Remove(staged)
	}()
	defer func() { _ = file.Close() }()
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("set staged message mode: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		return fmt.Errorf("write staged message: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync staged message: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close staged message: %w", err)
	}
	if err := os.Rename(staged, target); err != nil {
		return fmt.Errorf("activate staged message: %w", err)
	}
	return nil
}
