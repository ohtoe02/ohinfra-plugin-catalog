//go:build !linux

package catalogtool

import (
	"os"
	"os/exec"
	"time"
)

func configureSandboxCommand(command *exec.Cmd) {
	command.WaitDelay = time.Second
}

func syncDirectory(string) error {
	return nil
}

func validateSandboxRuntimeOwnership(os.FileInfo) error {
	return nil
}
