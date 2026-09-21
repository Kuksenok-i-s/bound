// Command bound keeps tool output inside a fixed budget so that coding agents
// (Cursor, Claude Code, Codex, ...) never ingest unbounded shell output, search
// results, files, diffs or logs. Every subcommand prints a bounded envelope and
// writes the full artifact to disk for targeted follow-up reads.
package main

import (
	"fmt"
	"os"

	"github.com/Kuksenok-i-s/bound/internal/bound"
)

var version = "dev"

const usage = `bound %s - bounded tool output for coding agents

usage: bound <command> [flags] [args]

  run   [--timeout D] [--lines N] [-c] -- <cmd...>   run a command; envelope + spill file
  grep  [-n N] [-i] [-t ext] [-g glob] <pattern> [paths]   bounded search (rg/grep)
  read  <file> [A:B] [--outline] [--full] [--grep RE -C N]   bounded file read
  diff  [git-diff args] [-- paths]                     --stat first, per-file on request
  log   <file> | -- <cmd...>  [--tail N] [--grep RE] [--since TS] [-C N]
  tree  [dir] [--depth N] [--max N]                    bounded directory listing
  hook  <cursor|claude|codex>                          pre-tool hook adapter (stdin JSON)
  init  [--agent all|cursor|claude|codex] [--project] [--no-skills] [--agents-md] [--dry-run]
  stats [--clean]                                      raw vs delivered bytes, spill files
  version

env: BOUND_DIR (spill dir, default $TMPDIR/bound), BOUND_RUN_LINES, BOUND_GREP_MAX,
     BOUND_READ_SOFT, BOUND_READ_HARD, BOUND_DIFF_SOFT, BOUND_LOG_TAIL, BOUND_TREE_MAX
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, usage, version)
		os.Exit(2)
	}
	args := os.Args[2:]
	var code int
	switch os.Args[1] {
	case "run":
		code = bound.Run(args, os.Stdout)
	case "grep":
		code = bound.Grep(args, os.Stdout)
	case "read":
		code = bound.Read(args, os.Stdout)
	case "diff":
		code = bound.Diff(args, os.Stdout)
	case "log":
		code = bound.Log(args, os.Stdout)
	case "tree":
		code = bound.Tree(args, os.Stdout)
	case "hook":
		code = bound.Hook(args, os.Stdin, os.Stdout)
	case "init":
		code = bound.Init(args, os.Stdout)
	case "stats":
		code = bound.Stats(args, os.Stdout)
	case "version", "--version", "-v":
		fmt.Println("bound", version)
	case "help", "-h", "--help":
		fmt.Printf(usage, version)
	default:
		fmt.Fprintf(os.Stderr, "bound: unknown command %q\n\n", os.Args[1])
		fmt.Fprintf(os.Stderr, usage, version)
		code = 2
	}
	os.Exit(code)
}
