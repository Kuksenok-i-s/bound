package bound

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// isWindows switches argv parsing, quoting and the -c shell. Kept as a
// variable so tests can exercise both code paths on any host.
var isWindows = runtime.GOOS == "windows"

// shellArgv wraps a command line for the platform shell.
func shellArgv(line string) []string {
	if isWindows {
		if ps, err := exec.LookPath("pwsh"); err == nil {
			return []string{ps, "-NoProfile", "-NonInteractive", "-Command", line}
		}
		if ps, err := exec.LookPath("powershell"); err == nil {
			return []string{ps, "-NoProfile", "-NonInteractive", "-Command", line}
		}
		return []string{os.Getenv("COMSPEC"), "/C", line}
	}
	return []string{"/bin/sh", "-c", line}
}

// quoteForHook renders the binary path for a hook command string. Double
// quotes are understood by sh, bash, PowerShell and cmd alike.
func quoteForHook(path string) string {
	if strings.ContainsAny(path, " \t") {
		return `"` + path + `"`
	}
	return path
}

// quoteWord quotes one argv word for the platform shell. POSIX: single quotes.
// Windows (PowerShell/cmd): double quotes; backslashes are path separators and
// are left alone.
func quoteWord(a string) string {
	if a == "" {
		if isWindows {
			return `""`
		}
		return "''"
	}
	if isWindows {
		if !strings.ContainsAny(a, " \t\"&|<>^%$(){};,`'") {
			return a
		}
		return `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
	}
	if !strings.ContainsAny(a, " \t\n'\"\\$`!*?[]{}()<>|&;#~") {
		return a
	}
	return "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
}

// SimpleArgv splits a command line into words. It returns ok=false when the
// line contains shell metacharacters (pipes, redirects, substitutions, globs,
// compound commands) so callers leave such commands untouched.
func SimpleArgv(cmd string) ([]string, bool) { return simpleArgv(cmd, isWindows) }

func simpleArgv(cmd string, windows bool) (argv []string, ok bool) {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return nil, false
	}
	meta := "|&;<>`$()\n*?[]{}~#"
	if windows {
		meta = "|&<>`$()\n*?{}^%\n"
	}
	var cur strings.Builder
	inWord := false
	quote := byte(0)
	flush := func() {
		if inWord {
			argv = append(argv, cur.String())
			cur.Reset()
			inWord = false
		}
	}
	for i := 0; i < len(cmd); i++ {
		c := cmd[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			} else if !windows && quote == '"' && c == '\\' && i+1 < len(cmd) {
				i++
				cur.WriteByte(cmd[i])
			} else if quote == '"' && (c == '$' || c == '`') {
				return nil, false
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote = c
			inWord = true
		case !windows && c == '\\' && i+1 < len(cmd):
			i++
			cur.WriteByte(cmd[i])
			inWord = true
		case c == ' ' || c == '\t':
			flush()
		case strings.IndexByte(meta, c) >= 0:
			return nil, false
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if quote != 0 {
		return nil, false
	}
	flush()
	return argv, len(argv) > 0
}
