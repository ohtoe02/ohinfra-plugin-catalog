package main

import (
	"encoding/json"
	"fmt"
	"os"
)

type manifest struct {
	ProtocolVersion int       `json:"protocol_version"`
	Name            string    `json:"name"`
	Version         string    `json:"version"`
	Description     string    `json:"description"`
	Commands        []command `json:"commands"`
}

type command struct {
	Path                 []string `json:"path"`
	Use                  string   `json:"use"`
	Short                string   `json:"short"`
	Category             string   `json:"category"`
	Arguments            []any    `json:"arguments"`
	Flags                []any    `json:"flags"`
	RequiresRoot         bool     `json:"requires_root"`
	RequiresForce        bool     `json:"requires_force"`
	SupportsDryRun       bool     `json:"supports_dry_run"`
	RequiresConfirmation bool     `json:"requires_confirmation"`
}

func main() {
	if len(os.Args) != 3 || os.Args[1] != "manifest" || os.Args[2] != "--protocol=1" {
		_, _ = fmt.Fprintln(os.Stderr, "usage: releasefixture manifest --protocol=1")
		os.Exit(2)
	}
	value := manifest{
		ProtocolVersion: 1,
		Name:            "sample",
		Version:         "1.2.3",
		Description:     "sample plugin",
		Commands: []command{{
			Path:      []string{"sample", "status"},
			Use:       "status",
			Short:     "Show sample status",
			Category:  "diagnostic",
			Arguments: []any{},
			Flags:     []any{},
		}},
	}
	if err := json.NewEncoder(os.Stdout).Encode(value); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
