// Command dore is the Doré compiler.
//
// Only `dore check` exists so far: it parses and validates specs without
// involving a model. That is deliberate — the verification machinery has to be
// trustworthy before generation is worth building on top of it.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/christopherwolf/dore/internal/ast"
	"github.com/christopherwolf/dore/internal/check"
	"github.com/christopherwolf/dore/internal/diag"
	"github.com/christopherwolf/dore/internal/source"
	"github.com/christopherwolf/dore/internal/syntax"
)

const usage = `dore — a specification language that compiles by assay

usage:
  dore check <file.dore>...   parse and validate specs (no model involved)
  dore version

flags:
  --no-color   disable colored diagnostics
`

// Exit codes are the CLI's contract with CI, so they are named rather than
// scattered as literals.
const (
	exitOK    = 0 // no diagnostics that fail a build
	exitDiags = 1 // the specs were read, and they have errors
	exitUsage = 2 // bad invocation, or a file that could not be read
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is main's body, parameterized over its streams so it can be tested
// without spawning a process.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}

	switch args[0] {
	case "check":
		return runCheck(args[1:], stdout, stderr)
	case "version":
		fmt.Fprintln(stdout, "dore 0.0.1-dev")
		return exitOK
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "dore: unknown command %q\n\n%s", args[0], usage)
		return exitUsage
	}
}

func runCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	noColor := fs.Bool("no-color", false, "disable colored diagnostics")

	// Accept flags before or after paths; `dore check f.dore --no-color` is
	// the order people actually type.
	var flags, paths []string
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
		} else {
			paths = append(paths, a)
		}
	}
	if err := fs.Parse(flags); err != nil {
		return exitUsage
	}

	if len(paths) == 0 {
		fmt.Fprintln(stderr, "dore check: no input files")
		return exitUsage
	}

	style := diag.Style{Color: !*noColor && os.Getenv("NO_COLOR") == "" && isTerminal(stderr)}
	bag := &diag.Bag{}
	var frozen, live, rows int

	for _, path := range paths {
		if ext := filepath.Ext(path); ext != ".dore" {
			fmt.Fprintf(stderr, "dore: %s: expected a .dore file\n", path)
			return exitUsage
		}
		f, err := source.Load(path)
		if err != nil {
			fmt.Fprintf(stderr, "dore: %v\n", err)
			return exitUsage
		}

		file, pbag := syntax.Parse(f)
		bag.Merge(pbag)
		bag.Merge(check.File(file))

		for _, d := range file.Decls {
			if fn, ok := d.(*ast.FnDecl); ok {
				if fn.Mode == ast.Live {
					live++
				} else {
					frozen++
				}
				rows += fn.RowCount()
			}
		}
	}

	style.RenderAll(stderr, bag)

	if bag.HasErrors() {
		return exitDiags
	}
	fmt.Fprintf(stdout, "ok  %s\n", summary(frozen, live, rows, len(paths)))
	return exitOK
}

func summary(frozen, live, rows, files int) string {
	parts := []string{fmt.Sprintf("%d file(s)", files)}
	if frozen > 0 {
		parts = append(parts, fmt.Sprintf("%d frozen", frozen))
	}
	if live > 0 {
		parts = append(parts, fmt.Sprintf("%d live", live))
	}
	parts = append(parts, fmt.Sprintf("%d row(s)", rows))
	return strings.Join(parts, ", ")
}

// isTerminal reports whether w is a character device, so colors are only
// emitted when a human is likely reading them.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
