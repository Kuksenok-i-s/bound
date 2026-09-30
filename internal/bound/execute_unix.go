//go:build unix

package bound

import (
	"os/exec"
	"syscall"
)

func childExitStatus(err *exec.ExitError) int {
	if status, ok := err.ProcessState.Sys().(syscall.WaitStatus); ok && status.Signaled() {
		return 128 + int(status.Signal())
	}
	return err.ExitCode()
}
