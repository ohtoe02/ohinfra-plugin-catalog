package catalogtool

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type operationPlan struct {
	CommandID            string       `json:"command_id"`
	Summary              string       `json:"summary"`
	Checks               []planCheck  `json:"checks"`
	Changes              []planChange `json:"changes"`
	Risks                []string     `json:"risks"`
	RequiresRoot         bool         `json:"requires_root"`
	RequiresForce        bool         `json:"requires_force"`
	RequiresConfirmation bool         `json:"requires_confirmation"`
}

type planCheck struct {
	ID      string         `json:"id"`
	Status  string         `json:"status"`
	Summary string         `json:"summary"`
	Details map[string]any `json:"details,omitempty"`
}

type planChange struct {
	Object  string         `json:"object"`
	Action  string         `json:"action"`
	Status  string         `json:"status"`
	Details map[string]any `json:"details,omitempty"`
}

type invocationContract struct {
	ProtocolVersion int            `json:"protocol_version"`
	RequestID       string         `json:"request_id"`
	CommandPath     []string       `json:"command_path"`
	Arguments       []string       `json:"arguments"`
	Options         map[string]any `json:"options"`
	Deadline        string         `json:"deadline,omitempty"`
	PlanDigest      string         `json:"plan_digest,omitempty"`
}

var (
	commandIDPattern  = regexp.MustCompile(`^[a-z][a-z0-9-]*(\.[a-z][a-z0-9-]*)+$`)
	planDigestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func CanonicalPlanJSON(encoded []byte) ([]byte, error) {
	if !utf8.Valid(encoded) {
		return nil, errors.New("plan must be valid UTF-8 JSON")
	}
	if err := rejectDuplicateJSONFields(encoded); err != nil {
		return nil, err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &root); err != nil {
		return nil, err
	}
	if err := requireJSONFields(
		root,
		"plan",
		"command_id",
		"summary",
		"checks",
		"changes",
		"risks",
		"requires_root",
		"requires_force",
		"requires_confirmation",
	); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var plan operationPlan
	if err := decoder.Decode(&plan); err != nil {
		return nil, err
	}
	if err := requireDecoderEOF(decoder); err != nil {
		return nil, err
	}
	if !commandIDPattern.MatchString(plan.CommandID) {
		return nil, errors.New("plan command_id is invalid")
	}
	if plan.Checks == nil || plan.Changes == nil || plan.Risks == nil {
		return nil, errors.New("plan canonical arrays must not be null")
	}
	if err := validateNestedPresence(
		root["checks"],
		"plan.checks",
		"id",
		"status",
		"summary",
	); err != nil {
		return nil, err
	}
	if err := validateNestedPresence(
		root["changes"],
		"plan.changes",
		"object",
		"action",
		"status",
	); err != nil {
		return nil, err
	}
	for _, check := range plan.Checks {
		if check.ID == "" || !validPlanStatus(check.Status) {
			return nil, errors.New("plan check is invalid")
		}
	}
	return json.Marshal(plan)
}

func ValidateInvocationJSON(command Command, phase string, encoded []byte) error {
	if phase != "plan" && phase != "execute" {
		return fmt.Errorf("invalid invocation phase %q", phase)
	}
	if !utf8.Valid(encoded) {
		return errors.New("invocation must be valid UTF-8 JSON")
	}
	if err := rejectDuplicateJSONFields(encoded); err != nil {
		return err
	}
	var root map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &root); err != nil {
		return err
	}
	if err := requireJSONFields(
		root,
		"invocation",
		"protocol_version",
		"request_id",
		"command_path",
		"arguments",
		"options",
	); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var invocation invocationContract
	if err := decoder.Decode(&invocation); err != nil {
		return err
	}
	if err := requireDecoderEOF(decoder); err != nil {
		return err
	}
	if invocation.ProtocolVersion != 1 {
		return errors.New("invocation protocol_version must be 1")
	}
	if invocation.RequestID == "" || len(invocation.RequestID) > 128 ||
		strings.TrimSpace(invocation.RequestID) != invocation.RequestID {
		return errors.New("invocation request_id is invalid")
	}
	if invocation.CommandPath == nil || !slices.Equal(invocation.CommandPath, command.Path) {
		return errors.New("invocation command_path does not match the command")
	}
	if invocation.Arguments == nil || invocation.Options == nil {
		return errors.New("invocation arguments and options must not be null")
	}
	if err := validateInvocationArgumentCount(command, invocation.Arguments); err != nil {
		return err
	}
	if err := validateInvocationOptions(command, invocation.Options); err != nil {
		return err
	}
	if invocation.Deadline != "" {
		if _, err := time.Parse(time.RFC3339Nano, invocation.Deadline); err != nil {
			return fmt.Errorf("invocation deadline is invalid: %w", err)
		}
	}
	mutation := command.Category == "operational" || command.Category == "runbook"
	switch {
	case phase == "plan" && invocation.PlanDigest != "":
		return errors.New("plan invocation must omit plan_digest")
	case phase == "execute" && mutation && !planDigestPattern.MatchString(invocation.PlanDigest):
		return errors.New("mutation execute invocation requires a lowercase plan_digest")
	case phase == "execute" && !mutation && invocation.PlanDigest != "":
		return errors.New("diagnostic execute invocation must omit plan_digest")
	}
	return nil
}

func validateInvocationArgumentCount(command Command, arguments []string) error {
	minimum := 0
	maximum := len(command.Arguments)
	variadic := false
	for _, argument := range command.Arguments {
		if argument.Required {
			minimum++
		}
		if argument.Variadic {
			variadic = true
		}
	}
	if len(arguments) < minimum || (!variadic && len(arguments) > maximum) {
		return errors.New("invocation argument count does not match the command")
	}
	return nil
}

func validateInvocationOptions(command Command, options map[string]any) error {
	flags := make(map[string]Flag, len(command.Flags))
	for _, flag := range command.Flags {
		flags[flag.Name] = flag
	}
	for name, value := range options {
		flag, found := flags[name]
		if !found {
			return fmt.Errorf("unknown option %q", name)
		}
		if !validInvocationOption(flag.Type, value) {
			return fmt.Errorf("option %q has the wrong type", name)
		}
	}
	return nil
}

func validInvocationOption(flagType string, value any) bool {
	switch flagType {
	case "string":
		_, valid := value.(string)
		return valid
	case "duration":
		text, valid := value.(string)
		if !valid {
			return false
		}
		_, err := time.ParseDuration(text)
		return err == nil
	case "bool":
		_, valid := value.(bool)
		return valid
	case "int":
		number, valid := value.(float64)
		return valid && !math.IsNaN(number) && !math.IsInf(number, 0) &&
			number == math.Trunc(number) && number >= math.MinInt && number <= math.MaxInt
	default:
		return false
	}
}

func requireDecoderEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("multiple JSON documents are not allowed")
		}
		return err
	}
	return nil
}

func validPlanStatus(status string) bool {
	switch status {
	case "pass", "info", "warning", "critical", "skipped", "partial", "error", "cancelled":
		return true
	default:
		return false
	}
}

func referenceResultExitCode(status, kind string) int {
	switch status {
	case "critical":
		return 6
	case "partial":
		return 9
	case "warning":
		return 5
	case "cancelled":
		return 7
	case "error", "skipped":
		return referenceErrorKindExitCode(kind)
	default:
		return 0
	}
}

func referenceErrorKindExitCode(kind string) int {
	switch kind {
	case "configuration":
		return 10
	case "timeout":
		return 8
	case "cancelled":
		return 7
	case "missing_dependency":
		return 4
	case "insufficient_privileges":
		return 3
	case "invalid_arguments":
		return 2
	default:
		return 1
	}
}

func referenceProcessFailureKind(exitCode int) string {
	switch exitCode {
	case 2:
		return "invalid_arguments"
	case 3:
		return "insufficient_privileges"
	case 4:
		return "missing_dependency"
	case 7:
		return "cancelled"
	case 8:
		return "timeout"
	case 10:
		return "configuration"
	default:
		return "general"
	}
}
