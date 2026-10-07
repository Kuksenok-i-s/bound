package bound

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

type capture struct {
	Path       string `json:"output"`
	Exit       int    `json:"exit"`
	TimedOut   bool   `json:"timed_out"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
	Lines      int    `json:"lines"`           // complete lines written to the spill
	Timed      int    `json:"timed_lines"`     // lines that already began with a recognised timestamp
	Stamped    int    `json:"stamped_lines"`   // lines prefixed with a receive timestamp (--stamp)
	Stamp      bool   `json:"stamp,omitempty"` // --stamp was requested
}

// timestampsNote renders the timestamp coverage for an envelope header and,
// when no line carries a timestamp, a hint on how to get one.
func (c capture) timestampsNote() (status, hint string) {
	status = fmt.Sprintf("timestamps=%d/%d", c.Timed, c.Lines)
	if c.Stamp {
		status += fmt.Sprintf(" stamped=true(%d)", c.Stamped)
	}
	if c.Lines > 0 && c.Timed == 0 && !c.Stamp {
		hint = "hint: no leading timestamps; for --since/--profile use the tool's timestamp flag (docker -t, kubectl --timestamps) or --stamp for receive times"
	}
	return status, hint
}

// lineWriter forwards a stream to the spill line by line, counting lines that
// begin with a recognised timestamp and, with stamp, prefixing a receive-time
// RFC3339Nano timestamp to the lines that do not. Lines are never altered
// otherwise; a line longer than maxHold is streamed through unbuffered.
type lineWriter struct {
	mu      *sync.Mutex
	dst     io.Writer
	c       *capture
	stamp   bool
	buf     []byte
	midLine bool // a partial line has already been flushed to dst
}

const maxHold = 1 << 20

func newLineWriter(mu *sync.Mutex, dst io.Writer, c *capture, stamp bool) *lineWriter {
	return &lineWriter{mu: mu, dst: dst, c: c, stamp: stamp}
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			if len(w.buf) >= maxHold {
				if err := w.emit(w.buf, false); err != nil {
					return 0, err
				}
				w.buf = w.buf[:0]
			}
			return len(p), nil
		}
		if err := w.emit(w.buf[:i+1], true); err != nil {
			return 0, err
		}
		w.buf = w.buf[i+1:]
	}
}

// emit writes a chunk; complete marks the end of a line.
func (w *lineWriter) emit(chunk []byte, complete bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if !w.midLine {
		text := strings.TrimSuffix(string(chunk), "\n")
		if _, ok := lineTime(stripANSI(text)); ok {
			w.c.Timed++
		} else if w.stamp {
			if _, err := io.WriteString(w.dst, time.Now().Format(time.RFC3339Nano)+" "); err != nil {
				return err
			}
			w.c.Stamped++
		}
	}
	if _, err := w.dst.Write(chunk); err != nil {
		return err
	}
	if complete {
		w.c.Lines++
	}
	w.midLine = !complete
	return nil
}

// flush writes a trailing line that ended without a newline.
func (w *lineWriter) flush() error {
	if len(w.buf) == 0 {
		return nil
	}
	err := w.emit(w.buf, true)
	w.buf = w.buf[:0]
	return err
}

// captureCommand retains raw merged stdout/stderr and durable termination
// metadata even for a small command. The child status is separate from an
// artifact persistence error, so an I/O failure cannot become a green result.
func captureCommand(argv []string, timeout time.Duration, env []string) (capture, error) {
	return captureCommandStamped(argv, timeout, env, false)
}

// captureCommandStamped is captureCommand with optional receive timestamps.
// stdout and stderr keep their own partial-line buffers and share the spill,
// so lines from the two streams never interleave mid-line.
func captureCommandStamped(argv []string, timeout time.Duration, env []string, stamp bool) (capture, error) {
	spill, err := NewSpill(argv[0])
	if err != nil {
		return capture{}, err
	}
	c := capture{Path: spill.Name(), Stamp: stamp}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = os.Stdin
	var mu sync.Mutex
	out, errw := newLineWriter(&mu, spill, &c, stamp), newLineWriter(&mu, spill, &c, stamp)
	cmd.Stdout, cmd.Stderr = out, errw
	cmd.Env = append(os.Environ(), env...)
	cmd.WaitDelay = 2 * time.Second
	started := time.Now()
	runErr := cmd.Run()
	for _, lw := range []*lineWriter{out, errw} {
		if ferr := lw.flush(); ferr != nil && err == nil {
			err = ferr
		}
	}
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
	if err != nil {
		return c, err
	}
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
