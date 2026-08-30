package assay

import (
	"fmt"
	"io"
	"strings"

	"github.com/christopherwolf/dore/internal/ast"
)

// Renderer writes an assay report for a human.
type Renderer struct {
	Color bool
}

const (
	reset = "\033[0m"
	bold  = "\033[1m"
	dim   = "\033[2m"
	red   = "\033[31;1m"
	green = "\033[32;1m"
	amber = "\033[33;1m"
)

func (r Renderer) c(code, s string) string {
	if !r.Color {
		return s
	}
	return code + s + reset
}

// Render writes the whole report.
func (r Renderer) Render(w io.Writer, rep *Report) {
	for _, fn := range rep.Fns {
		r.renderFn(w, fn)
	}

	passed, total := rep.Counts()
	fmt.Fprintln(w)
	switch {
	case rep.Passed() && total > 0:
		fmt.Fprintf(w, "%s  %d/%d rows in %s\n",
			r.c(green, "ok"), passed, total, plural(len(rep.Fns), "function"))
	case total == 0:
		fmt.Fprintf(w, "%s  nothing was assayed\n", r.c(amber, "warning"))
	default:
		fmt.Fprintf(w, "%s  %d of %d rows\n", r.c(red, "failed"), total-passed, total)
	}
}

func (r Renderer) renderFn(w io.Writer, fn *FnResult) {
	name := r.c(bold, fn.Fn.Name)

	if fn.Problem != "" {
		fmt.Fprintf(w, "%s  %s\n", name, r.c(red, "could not be assayed"))
		fmt.Fprintf(w, "    %s\n", fn.Problem)
		return
	}

	passed, total := fn.Counts()
	status := r.c(green, fmt.Sprintf("%d/%d rows", passed, total))
	if passed != total {
		status = r.c(red, fmt.Sprintf("%d/%d rows", passed, total))
	}
	fmt.Fprintf(w, "%s  %s\n", name, status)

	for _, row := range fn.Rows {
		if !row.Passed {
			r.renderRow(w, fn.Fn, row)
		}
	}
}

func (r Renderer) renderRow(w io.Writer, fn *ast.FnDecl, row RowResult) {
	where := "examples"
	if row.Table.IsScenario() {
		where = fmt.Sprintf("scenario %q", row.Table.Label)
	}

	head := fmt.Sprintf("  %s %s, row %d", r.c(red, "✗"), where, row.RowIdx+1)
	if note := noteOf(row); note != "" {
		head += "    " + r.c(dim, note)
	}
	fmt.Fprintln(w, head)

	// Inputs first: a failure the reader cannot reproduce is only half a report.
	width := 0
	for _, p := range fn.Params {
		if len(p.Name) > width {
			width = len(p.Name)
		}
	}
	if width < len("expected") {
		width = len("expected")
	}

	for ci, col := range row.Table.Columns {
		if col.Kind != ast.ColInput || ci >= len(row.Cells()) {
			continue
		}
		fmt.Fprintf(w, "      %-*s  %s\n", width, col.Name, row.Cells()[ci].Raw)
	}

	if row.Problem != "" {
		fmt.Fprintf(w, "      %-*s  %s\n", width, "expected", row.Expected.String())
		fmt.Fprintf(w, "      %s\n", r.c(red, row.Problem))
		return
	}

	fmt.Fprintf(w, "      %-*s  %s\n", width, "expected", r.c(green, row.Expected.String()))
	fmt.Fprintf(w, "      %-*s  %s\n", width, "got", r.c(red, row.Actual.String()))
}

// noteOf returns the row's note cell, which exists so a row can say why it was
// written. Losing it at failure time would make it decoration.
func noteOf(row RowResult) string {
	for ci, col := range row.Table.Columns {
		if col.Kind == ast.ColNote && ci < len(row.Cells()) {
			return row.Cells()[ci].Raw
		}
	}
	return ""
}

func plural(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

var _ = strings.TrimSpace
