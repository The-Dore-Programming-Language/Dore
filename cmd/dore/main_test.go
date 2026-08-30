package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Exit codes are the CLI's contract with CI: 0 clean, 1 diagnostics, 2 bad
// invocation. A build script cannot tell a failed spec from a typo'd path
// without them, so they are pinned here.
func TestExitCodes(t *testing.T) {
	dir := t.TempDir()
	clean := filepath.Join(dir, "clean.dore")
	broken := filepath.Join(dir, "broken.dore")

	write(t, clean, `frozen fn f(a: int) -> out: bool
  examples:
    | a | out  |
    | 1 | true |
`)
	write(t, broken, `frozen fn f(a: int) -> out: bool
  examples:
    | a | out   |
    | 1 | maybe |
`)

	tests := []struct {
		name string
		args []string
		want int
	}{
		{"clean file", []string{"check", clean}, exitOK},
		{"diagnostics", []string{"check", broken}, exitDiags},
		{"no arguments", nil, exitUsage},
		{"no input files", []string{"check"}, exitUsage},
		{"unknown command", []string{"assay", clean}, exitUsage},
		{"missing file", []string{"check", filepath.Join(dir, "nope.dore")}, exitUsage},
		{"wrong extension", []string{"check", "README.md"}, exitUsage},
		{"version", []string{"version"}, exitOK},
		{"help", []string{"--help"}, exitOK},
		{"flag after path", []string{"check", clean, "--no-color"}, exitOK},
		{"flag before path", []string{"check", "--no-color", clean}, exitOK},
		{"mixed clean and broken", []string{"check", clean, broken}, exitDiags},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			if got := run(tc.args, &out, &errOut); got != tc.want {
				t.Errorf("run(%v) = %d, want %d\nstdout: %s\nstderr: %s",
					tc.args, got, tc.want, out.String(), errOut.String())
			}
		})
	}
}

// A failing check must not print an "ok" summary to stdout, and a passing one
// must not print diagnostics to stderr. Scripts read these streams separately.
func TestStreamSeparation(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.dore")
	write(t, broken, `frozen fn f(a: int) -> out: bool
`)

	var out, errOut bytes.Buffer
	if code := run([]string{"check", broken, "--no-color"}, &out, &errOut); code != exitDiags {
		t.Fatalf("exit = %d, want %d", code, exitDiags)
	}
	if out.Len() != 0 {
		t.Errorf("stdout should be empty on failure, got %q", out.String())
	}
	if !strings.Contains(errOut.String(), "E0101") {
		t.Errorf("stderr should carry the diagnostic, got %q", errOut.String())
	}
}

func TestSummaryCountsAcrossFiles(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.dore")
	b := filepath.Join(dir, "b.dore")
	write(t, a, `frozen fn f(a: int) -> out: bool
  examples:
    | a | out  |
    | 1 | true |
    | 2 | true |
`)
	write(t, b, `live fn g(m: text) -> out: text
`)

	var out, errOut bytes.Buffer
	if code := run([]string{"check", a, b, "--no-color"}, &out, &errOut); code != exitOK {
		t.Fatalf("exit = %d, want %d\n%s", code, exitOK, errOut.String())
	}
	got := out.String()
	for _, want := range []string{"2 file(s)", "1 frozen", "1 live", "2 row(s)"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary %q missing %q", strings.TrimSpace(got), want)
		}
	}
}

// Colors are suppressed when stderr is not a terminal, so redirected output
// stays diffable.
func TestNoColorWhenNotATerminal(t *testing.T) {
	dir := t.TempDir()
	broken := filepath.Join(dir, "broken.dore")
	write(t, broken, `frozen fn f(a: int) -> out: bool
`)

	var out, errOut bytes.Buffer
	run([]string{"check", broken}, &out, &errOut)
	if strings.Contains(errOut.String(), "\033[") {
		t.Errorf("expected no ANSI codes when stderr is a buffer, got %q", errOut.String())
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
