package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func main() {
	if len(os.Args) >= 2 && os.Args[1] == "rm" {
		os.Exit(0)
	}
	required := []string{
		"run",
		"--rm",
		"--pull=never",
		"--platform=linux/amd64",
		"--network=none",
		"--ipc=none",
		"--read-only",
		"--cap-drop=ALL",
		"--security-opt=no-new-privileges",
		"--pids-limit=64",
		"--memory=128m",
		"--cpus=1",
		"--user=65534:65534",
		"--workdir=/",
	}
	for _, value := range required {
		if !slices.Contains(os.Args[1:], value) {
			_, _ = fmt.Fprintf(os.Stderr, "missing sandbox argument %s\n", value)
			os.Exit(2)
		}
	}
	mount := ""
	for _, value := range os.Args[1:] {
		if strings.HasPrefix(value, "--mount=type=bind,source=") {
			mount = value
			break
		}
	}
	const prefix = "--mount=type=bind,source="
	const suffix = ",target=/plugin,readonly"
	if mount == "" || !strings.HasSuffix(mount, suffix) {
		_, _ = fmt.Fprintln(os.Stderr, "missing read-only plugin mount")
		os.Exit(2)
	}
	source := strings.TrimSuffix(strings.TrimPrefix(mount, prefix), suffix)
	content, err := os.ReadFile(filepath.Join(source, "plugin"))
	if err != nil || string(content) != "verified release bytes" {
		if err == nil && string(content) == "timeout release bytes" {
			time.Sleep(30 * time.Second)
		}
		if err == nil && string(content) == "oversized release bytes" {
			_, _ = os.Stdout.WriteString(strings.Repeat("x", (1<<20)+1))
			time.Sleep(30 * time.Second)
		}
		_, _ = fmt.Fprintln(os.Stderr, "sandbox did not receive exact release bytes")
		os.Exit(2)
	}
	manifest := map[string]any{
		"protocol_version": 1,
		"name":             "sample",
		"version":          "1.2.3",
		"description":      "sample plugin",
		"commands": []any{map[string]any{
			"path":                  []string{"sample", "status"},
			"use":                   "status",
			"short":                 "Show sample status",
			"category":              "diagnostic",
			"arguments":             []any{},
			"flags":                 []any{},
			"requires_root":         false,
			"requires_force":        false,
			"supports_dry_run":      false,
			"requires_confirmation": false,
		}},
	}
	if err := json.NewEncoder(os.Stdout).Encode(manifest); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
