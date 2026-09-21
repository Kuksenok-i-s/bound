package bound

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSimpleArgv(t *testing.T) {
	cases := []struct {
		in   string
		want []string
		ok   bool
	}{
		{`go test ./...`, []string{"go", "test", "./..."}, true},
		{`echo "a b" 'c d'`, []string{"echo", "a b", "c d"}, true},
		{`go test ./... | tail -5`, nil, false},
		{`cat f > out`, nil, false},
		{`echo $(date)`, nil, false},
		{`ls *.go`, nil, false},
		{`a && b`, nil, false},
		{``, nil, false},
	}
	for _, c := range cases {
		got, ok := SimpleArgv(c.in)
		if ok != c.ok {
			t.Fatalf("%q: ok=%v want %v", c.in, ok, c.ok)
		}
		if ok && strings.Join(got, "\x00") != strings.Join(c.want, "\x00") {
			t.Fatalf("%q: got %q want %q", c.in, got, c.want)
		}
	}
}

func TestRewriteShell(t *testing.T) {
	l := DefaultLimits()
	dir := t.TempDir()
	big := filepath.Join(dir, "big.txt")
	_ = os.WriteFile(big, []byte("x\n"), 0o644)

	cases := []struct {
		cmd     string
		action  string
		contain string
	}{
		{"go test -v ./...", "rewrite", "run -- go test ./..."},
		{"go test -v ./pkg", "rewrite", "run -- go test -v ./pkg"},
		{"go test ./... | tail -20", "allow", ""},
		{"kubectl logs pod-1", "rewrite", "--tail=300"},
		{"kubectl logs pod-1 --tail=50", "rewrite", "run -- kubectl logs pod-1 --tail=50"},
		{"journalctl -u nginx", "rewrite", "-n 300"},
		{"docker logs web --since 10m", "rewrite", "run -- docker logs"},
		{"git diff", "rewrite", " diff"},
		{"git diff --cached src/a.go", "rewrite", "diff --cached src/a.go"},
		{"git log", "rewrite", "-n 20"},
		{"git log -n 5", "allow", ""},
		{"git status", "rewrite", "git status --short --branch"},
		{"git status -s", "allow", ""},
		{"cat " + big, "rewrite", " read " + big},
		{"cat /nonexistent/file", "allow", ""},
		{"find .", "rewrite", " tree ."},
		{"find src -name '*.go' -maxdepth 2", "rewrite", "run -- find"},
		{"npm test", "rewrite", "run -- npm test"},
		{"pytest tests/unit -q", "rewrite", "run -- pytest tests/unit -q"},
		{"ls -la", "allow", ""},
		{"echo hi", "allow", ""},
		{"/usr/local/bin/bound run -- go test ./...", "allow", ""},
	}
	for _, c := range cases {
		d := RewriteShell(c.cmd, dir, l)
		if d.Action != c.action {
			t.Errorf("%q: action %s want %s (%s)", c.cmd, d.Action, c.action, d.Reason)
			continue
		}
		if c.contain != "" && !strings.Contains(d.Command, c.contain) {
			t.Errorf("%q: command %q lacks %q", c.cmd, d.Command, c.contain)
		}
	}
}

func TestParsers(t *testing.T) {
	goOut := `--- FAIL: TestRefresh (0.01s)
    refresh_test.go:88: expected 200 got 401
--- FAIL: TestRevoke (0.00s)
    revoke_test.go:41: nil pointer
FAIL
FAIL	example.com/auth	0.123s
ok  	example.com/util	0.010s
`
	got := parseGo(strings.NewReader(goOut))
	if len(got) < 3 || !strings.Contains(got[0], "ok=1 fail=1") || !strings.Contains(got[1], "TestRefresh") || !strings.Contains(got[1], "refresh_test.go:88") {
		t.Fatalf("go parser: %q", got)
	}

	pyOut := `FAILED tests/test_a.py::test_x - AssertionError: 1 != 2
E       assert 1 == 2
=========== 1 failed, 42 passed in 3.21s ===========
`
	got = parsePytest(strings.NewReader(pyOut))
	if len(got) != 3 || !strings.Contains(got[0], "1 failed, 42 passed") || !strings.HasPrefix(got[1], "FAILED tests/test_a.py::test_x") {
		t.Fatalf("pytest parser: %q", got)
	}

	jestOut := `  ● Login › rejects bad password
Tests:       1 failed, 10 passed, 11 total
Test Suites: 1 failed, 2 passed, 3 total
`
	got = parseJest(strings.NewReader(jestOut))
	if len(got) != 3 || !strings.Contains(strings.Join(got, "\n"), "rejects bad password") {
		t.Fatalf("jest parser: %q", got)
	}

	cargoOut := `test auth::tests::refresh ... FAILED
thread 'auth::tests::refresh' panicked at src/auth.rs:10:5:
test result: FAILED. 9 passed; 1 failed; 0 ignored
`
	got = parseCargo(strings.NewReader(cargoOut))
	if len(got) != 3 || !strings.Contains(got[0], "9 passed; 1 failed") {
		t.Fatalf("cargo parser: %q", got)
	}
}

func TestOutlineAndRange(t *testing.T) {
	dir := t.TempDir()
	src := "package x\n\nimport \"fmt\"\n\ntype Foo struct{}\n\nfunc NewFoo() *Foo { return nil }\n\nfunc (f *Foo) Start() {\n\tfmt.Println()\n}\n"
	p := filepath.Join(dir, "foo.go")
	_ = os.WriteFile(p, []byte(src), 0o644)
	// force regex path regardless of ctags presence
	ext := filepath.Ext(p)
	rx := outlineRules[ext]
	n := 0
	for _, ln := range strings.Split(src, "\n") {
		if rx.MatchString(ln) {
			n++
		}
	}
	if n != 3 {
		t.Fatalf("go outline regex matched %d, want 3", n)
	}
	a, b, err := parseRange("5:9", 11)
	if err != nil || a != 5 || b != 9 {
		t.Fatalf("range: %d %d %v", a, b, err)
	}
	if _, _, err := parseRange("9:5", 11); err == nil {
		t.Fatal("expected error for reversed range")
	}
	var buf bytes.Buffer
	if code := Read([]string{p, "5:7"}, &buf); code != 0 {
		t.Fatal("read failed")
	}
	if !strings.Contains(buf.String(), "     5| type Foo struct{}") || strings.Contains(buf.String(), "     9|") {
		t.Fatalf("range output: %s", buf.String())
	}
}

func TestHTMLOutline(t *testing.T) {
	dir := t.TempDir()
	page := "<!doctype html>\n<html>\n<head>\n<link rel=\"stylesheet\" href=\"ds.css\">\n</head>\n<body>\n<nav class=\"top\">\n<a href=\"#\">x</a>\n</nav>\n<main>\n<h1>Orders</h1>\n<p>text</p>\n<p>more</p>\n<table id=\"orders\">\n<tr><td>1</td></tr>\n</table>\n<div id=\"empty-state\">none</div>\n</main>\n<template id=\"row\"><tr></tr></template>\n</body>\n</html>\n"
	p := filepath.Join(dir, "orders.html")
	_ = os.WriteFile(p, []byte(page), 0o644)
	got := strings.Join(outline(p, 100), "\n")
	for _, want := range []string{"<link", "<nav", "<main", "<h1>Orders", `id="orders"`, `id="empty-state"`, "<template"} {
		if !strings.Contains(got, want) {
			t.Errorf("outline missing %q:\n%s", want, got)
		}
	}
	for _, no := range []string{"<p>text", "<td>1", "<a href"} {
		if strings.Contains(got, no) {
			t.Errorf("outline should not include %q", no)
		}
	}
}

func TestHookClaudeShellRewrite(t *testing.T) {
	in := `{"tool_name":"Bash","tool_input":{"command":"go test -v ./...","description":"run"},"cwd":"/tmp"}`
	var out bytes.Buffer
	if code := Hook([]string{"claude"}, strings.NewReader(in), &out); code != 0 {
		t.Fatal("hook exit", code)
	}
	var doc map[string]map[string]interface{}
	if err := json.Unmarshal(out.Bytes(), &doc); err != nil {
		t.Fatalf("json: %v: %s", err, out.String())
	}
	hso := doc["hookSpecificOutput"]
	if hso["permissionDecision"] != "allow" {
		t.Fatalf("decision: %v", hso)
	}
	ui := hso["updatedInput"].(map[string]interface{})
	if !strings.Contains(ui["command"].(string), "run -- go test ./...") || ui["description"] != "run" {
		t.Fatalf("updatedInput: %v", ui)
	}
}

func TestHookCursorGrepAndRead(t *testing.T) {
	in := `{"tool_name":"Grep","tool_input":{"pattern":"foo","path":"."},"cwd":"/tmp"}`
	var out bytes.Buffer
	Hook([]string{"cursor"}, strings.NewReader(in), &out)
	var doc map[string]interface{}
	_ = json.Unmarshal(out.Bytes(), &doc)
	ui, _ := doc["updated_input"].(map[string]interface{})
	if doc["permission"] != "allow" || ui == nil || ui["head_limit"].(float64) != 100 {
		t.Fatalf("grep hook: %s", out.String())
	}

	dir := t.TempDir()
	big := filepath.Join(dir, "big.go")
	var sb strings.Builder
	sb.WriteString("package big\n")
	for i := 0; i < 2500; i++ {
		sb.WriteString("func F")
		sb.WriteString(strings.Repeat("x", i%7))
		sb.WriteString("() {}\n")
	}
	_ = os.WriteFile(big, []byte(sb.String()), 0o644)
	in = `{"tool_name":"Read","tool_input":{"path":"` + big + `"},"cwd":"` + dir + `"}`
	out.Reset()
	Hook([]string{"cursor"}, strings.NewReader(in), &out)
	_ = json.Unmarshal(out.Bytes(), &doc)
	if doc["permission"] != "deny" || !strings.Contains(doc["agent_message"].(string), "offset/limit") {
		t.Fatalf("read hook: %s", out.String())
	}
	in = `{"tool_name":"Read","tool_input":{"path":"` + big + `","offset":10,"limit":50},"cwd":"` + dir + `"}`
	out.Reset()
	Hook([]string{"cursor"}, strings.NewReader(in), &out)
	if strings.TrimSpace(out.String()) != `{"permission":"allow"}` {
		t.Fatalf("ranged read should pass: %s", out.String())
	}
}

func TestHookNoopIsSilentForClaude(t *testing.T) {
	in := `{"tool_name":"Bash","tool_input":{"command":"echo hi"},"cwd":"/tmp"}`
	var out bytes.Buffer
	Hook([]string{"claude"}, strings.NewReader(in), &out)
	if out.Len() != 0 {
		t.Fatalf("expected empty output, got %s", out.String())
	}
}

func TestEnvelopeCap(t *testing.T) {
	e := newEnvelope(100)
	for i := 0; i < 50; i++ {
		e.line("0123456789")
	}
	if len(e.String()) > 100 {
		t.Fatalf("envelope exceeded cap: %d", len(e.String()))
	}
}
