package catalogtool

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	releaseManifestTimeout     = 10 * time.Second
	releaseManifestOutputLimit = 1 << 20
	releaseManifestErrorLimit  = 64 << 10
	dockerSandboxImage         = "debian:13-slim"
)

type ManifestSandbox interface {
	Manifest(context.Context, []byte) ([]byte, error)
}

type DockerManifestSandbox struct {
	RuntimePath string
	Image       string

	allowUntrustedRuntime bool
}

func DefaultDockerManifestSandbox() DockerManifestSandbox {
	runtimePath := "/usr/bin/docker"
	if runtime.GOOS == "windows" {
		runtimePath = `C:\Program Files\Docker\Docker\resources\bin\docker.exe`
	}
	return DockerManifestSandbox{RuntimePath: runtimePath, Image: dockerSandboxImage}
}

func (sandbox DockerManifestSandbox) Manifest(
	ctx context.Context,
	binary []byte,
) ([]byte, error) {
	if err := validateSandboxRuntime(
		sandbox.RuntimePath,
		sandbox.allowUntrustedRuntime,
	); err != nil {
		return nil, err
	}
	if sandbox.Image != dockerSandboxImage {
		return nil, fmt.Errorf("sandbox image must be %q", dockerSandboxImage)
	}
	directory, err := os.MkdirTemp("", "ohtools-catalog-sandbox-*")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(directory) }()
	// The bind-mounted directory must be traversable by the sandbox's
	// unprivileged uid. It contains only the already-public release bytes.
	if err := os.Chmod(directory, 0o755); err != nil {
		return nil, err
	}
	executable := filepath.Join(directory, "plugin")
	if err := writeSandboxBinary(executable, binary); err != nil {
		return nil, err
	}
	containerName := "ohtools-catalog-" + filepath.Base(directory)
	arguments := []string{
		"run",
		"--rm",
		"--pull=never",
		"--name=" + containerName,
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
		"--mount=type=bind,source=" + directory + ",target=/plugin,readonly",
		sandbox.Image,
		"/plugin/plugin",
		"manifest",
		"--protocol=1",
	}
	runContext, stop := context.WithCancel(ctx)
	defer stop()
	command := exec.CommandContext(runContext, sandbox.RuntimePath, arguments...) // #nosec G204 -- absolute validated runtime and fixed argv.
	command.Dir = directory
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
	command.Stdin = strings.NewReader("")
	stdout := &boundedBuffer{limit: releaseManifestOutputLimit, onLimit: stop}
	stderr := &boundedBuffer{limit: releaseManifestErrorLimit, onLimit: stop}
	command.Stdout = stdout
	command.Stderr = stderr
	configureSandboxCommand(command)

	runErr := command.Run()
	if stdout.exceeded {
		return nil, errors.Join(
			fmt.Errorf("sandbox manifest stdout exceeds %d bytes", releaseManifestOutputLimit),
			sandbox.cleanupAfterFailure(runErr, containerName, directory),
		)
	}
	if stderr.exceeded {
		return nil, errors.Join(
			fmt.Errorf("sandbox manifest stderr exceeds %d bytes", releaseManifestErrorLimit),
			sandbox.cleanupAfterFailure(runErr, containerName, directory),
		)
	}
	if runErr != nil {
		cleanupErr := sandbox.removeContainer(containerName, directory)
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			runErr = errors.Join(errors.New("sandbox manifest invocation timed out"), cleanupErr)
		} else {
			runErr = errors.Join(fmt.Errorf("sandbox manifest invocation failed: %w", runErr), cleanupErr)
		}
		return nil, runErr
	}
	return append([]byte(nil), stdout.bytes()...), nil
}

func (sandbox DockerManifestSandbox) cleanupAfterFailure(runErr error, name, directory string) error {
	if runErr == nil {
		return nil
	}
	return sandbox.removeContainer(name, directory)
}

func (sandbox DockerManifestSandbox) removeContainer(name, directory string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, sandbox.RuntimePath, "rm", "--force", name) // #nosec G204 -- validated runtime and generated name.
	command.Dir = directory
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"}
	command.Stdin = strings.NewReader("")
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	configureSandboxCommand(command)
	if err := command.Run(); err != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return fmt.Errorf("clean up sandbox container: %w", err)
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return errors.New("sandbox container cleanup timed out")
	}
	return nil
}

func validateSandboxRuntime(path string, allowUntrusted bool) error {
	if !filepath.IsAbs(path) {
		return errors.New("sandbox runtime path must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("sandbox runtime is unavailable: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New("sandbox runtime must be a regular non-symlink file")
	}
	if !allowUntrusted {
		if err := validateSandboxRuntimeOwnership(info); err != nil {
			return err
		}
	}
	return nil
}

func writeSandboxBinary(path string, binary []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o555)
	if err != nil {
		return err
	}
	if _, err := file.Write(binary); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

type boundedBuffer struct {
	content  bytes.Buffer
	limit    int
	exceeded bool
	onLimit  func()
	once     sync.Once
}

func (buffer *boundedBuffer) Write(input []byte) (int, error) {
	remaining := buffer.limit - buffer.content.Len()
	if remaining > 0 {
		count := len(input)
		if count > remaining {
			count = remaining
		}
		_, _ = buffer.content.Write(input[:count])
	}
	if len(input) > remaining {
		buffer.exceeded = true
		if buffer.onLimit != nil {
			buffer.once.Do(buffer.onLimit)
		}
	}
	return len(input), nil
}

func (buffer *boundedBuffer) bytes() []byte {
	return buffer.content.Bytes()
}
