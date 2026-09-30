//go:build !unix

package bound

import "os/exec"

func childExitStatus(err *exec.ExitError) int {
	code := err.ExitCode()
	if code < 0 {
		return 1
	}
	return code
}
