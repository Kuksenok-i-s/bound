package bound

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"time"
)

type capture struct {
	Path       string `json:"output"`
	Exit       int    `json:"exit"`
	TimedOut   bool   `json:"timed_out"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// captureCommand retains raw merged stdout/stderr and durable termination
// metadata even for a small command. The child status is separate from an
// artifact persistence error, so an I/O failure cannot become a green result.
func captureCommand(argv []string, timeout time.Duration, env []string) (capture, error) {
	spill, err := NewSpill(argv[0])
	if err != nil {
		return capture{}, err
	}
	c := capture{Path: spill.Name()}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout, cmd.Stderr = spill, spill
	cmd.Env = append(os.Environ(), env...)
	cmd.WaitDelay = 2 * time.Second
	started := time.Now()
	runErr := cmd.Run()
	c.DurationMS = time.Since(started).Milliseconds()
	c.TimedOut = runErr != nil && errors.Is(ctx.Err(), context.DeadlineExceeded)
	if runErr != nil {
		c.Error = runErr.Error()
		var ee *exec.ExitError
		switch {
		case c.TimedOut:
			c.Exit = 124
		case errors.As(runErr, &ee):
			c.Exit = childExitStatus(ee)
		default:
			c.Exit = 127
		}
	}
	closeErr := spill.Close()
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return c, err
	}
	if err := os.WriteFile(c.Path+".meta.json", append(data, '\n'), 0600); err != nil {
		return c, err
	}
	return c, closeErr
}

func commandTimeout(flags map[string]string, fallback time.Duration) (time.Duration, error) {
	if v, ok := flags["timeout"]; ok {
		d, err := time.ParseDuration(v)
		if err != nil {
			return 0, err
		}
		if d <= 0 {
			return 0, errors.New("timeout must be positive")
		}
		return d, nil
	}
	return fallback, nil
}
