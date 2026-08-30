// Command dore is the Doré compiler.
//
// Two commands exist so far. `check` parses and validates specs; `assay` runs
// a touchstone against a hand-written implementation. Neither involves a model.
// That is deliberate — the verification machinery has to be trustworthy before
// generation is worth building on top of it.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/christopherwolf/dore/internal/assay"
	"github.com/christopherwolf/dore/internal/ast"
	"github.com/christopherwolf/dore/internal/backend"
	"github.com/christopherwolf/dore/internal/backend/python"
	"github.com/christopherwolf/dore/internal/check"
	"github.com/christopherwolf/dore/internal/diag"
	"github.com/christopherwolf/dore/internal/source"
	"github.com/christopherwolf/dore/internal/syntax"
)

const usage = `dore — a specification language that compiles by assay

usage:
  dore check <file.dore>...              parse and validate specs
  dore assay <file.dore> --impl <file>   run the touchstone against an implementation
  dore version

No model is involved in either command.

flags:
  --no-color        disable colored output
  --impl <file>     implementation to assay (assay only)
  --keep-harness    leave the generated harness on disk (assay only)
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
	case "assay":
		return runAssay(args[1:], stdout, stderr)
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

func runAssay(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("assay", flag.ContinueOnError)
	fs.SetOutput(stderr)
	impl := fs.String("impl", "", "implementation file to assay")
	noColor := fs.Bool("no-color", false, "disable colored output")
	keep := fs.Bool("keep-harness", false, "leave the generated harness on disk")

	var flags, paths []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			flags = append(flags, a)
			// --impl takes a value; keep it with its flag.
			if (a == "--impl" || a == "-impl") && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		paths = append(paths, a)
	}
	if err := fs.Parse(flags); err != nil {
		return exitUsage
	}

	if len(paths) != 1 {
		fmt.Fprintln(stderr, "dore assay: expected exactly one .dore file")
		return exitUsage
	}
	if *impl == "" {
		fmt.Fprintln(stderr, "dore assay: --impl is required\n\nusage: dore assay <file.dore> --impl <implementation>")
		return exitUsage
	}
	if _, err := os.Stat(*impl); err != nil {
		fmt.Fprintf(stderr, "dore: %v\n", err)
		return exitUsage
	}

	specPath := paths[0]
	if filepath.Ext(specPath) != ".dore" {
		fmt.Fprintf(stderr, "dore: %s: expected a .dore file\n", specPath)
		return exitUsage
	}
	f, err := source.Load(specPath)
	if err != nil {
		fmt.Fprintf(stderr, "dore: %v\n", err)
		return exitUsage
	}

	// The spec gates the implementation, so an invalid spec is not something to
	// assay around. Running anyway would report failures the spec never meant.
	file, bag := syntax.Parse(f)
	bag.Merge(check.File(file))
	color := !*noColor && os.Getenv("NO_COLOR") == "" && isTerminal(stderr)
	if bag.HasErrors() {
		diag.Style{Color: color}.RenderAll(stderr, bag)
		return exitDiags
	}

	var be backend.Backend
	switch ext := filepath.Ext(*impl); ext {
	case ".py":
		be = python.Backend{}
	default:
		fmt.Fprintf(stderr, "dore: no backend for %s files\n", ext)
		return exitUsage
	}

	rep, err := assay.Run(context.Background(), file, specPath, *impl,
		assay.Options{Backend: be, KeepHarness: *keep})
	if err != nil {
		fmt.Fprintf(stderr, "dore: %v\n", err)
		return exitUsage
	}

	assay.Renderer{Color: color}.Render(stdout, rep)
	if !rep.Passed() {
		return exitDiags
	}
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
