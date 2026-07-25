//go:build linux

package catalogtool

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"
)

func configureSandboxCommand(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	command.Cancel = func() error {
		if command.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	command.WaitDelay = time.Second
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func validateSandboxRuntimeOwnership(info os.FileInfo) error {
	status, valid := info.Sys().(*syscall.Stat_t)
	if !valid || status.Uid != 0 || info.Mode().Perm()&0o022 != 0 {
		return errors.New("sandbox runtime must be root-owned and not group/other writable")
	}
	return nil
}
