package diag

import (
	"fmt"
	"io"
	"strings"

	"github.com/christopherwolf/dore/internal/source"
)

// Style controls colored output. Colors are disabled when writing to a
// non-terminal or when NO_COLOR is set; the caller decides.
type Style struct {
	Color bool
}

const (
	reset  = "\033[0m"
	bold   = "\033[1m"
	dim    = "\033[2m"
	red    = "\033[31;1m"
	yellow = "\033[33;1m"
	blue   = "\033[34;1m"
	cyan   = "\033[36m"
)

func (s Style) wrap(code, text string) string {
	if !s.Color {
		return text
	}
	return code + text + reset
}

func (s Style) severityColor(sev Severity) string {
	switch sev {
	case Warning:
		return yellow
	case Note:
		return blue
	default:
		return red
	}
}

// Render writes one diagnostic in the standard Doré format:
//
//	error[E0104]: touchstone cell does not match declared type
//	  ┌─ examples/refunds.dore:12:29
//	   │
//	12 │     | 5 | 49.99 | maybe |
//	   │                   ^^^^^ not a bool literal
//	   │
//	   = help: bool literals are `true` or `false`
func (s Style) Render(w io.Writer, d *Diagnostic) {
	sev := s.wrap(s.severityColor(d.Severity), d.Severity.String())
	if d.Code != "" {
		sev += s.wrap(s.severityColor(d.Severity), "["+d.Code+"]")
	}
	fmt.Fprintf(w, "%s: %s\n", sev, s.wrap(bold, d.Msg))

	span := d.Primary.Span
	if !span.Valid() {
		if d.Help != "" {
			fmt.Fprintf(w, "  %s %s\n", s.wrap(dim, "="), "help: "+d.Help)
		}
		fmt.Fprintln(w)
		return
	}

	// Gutter is sized to the widest line number we will print.
	maxLine := span.Start.Line
	for _, l := range d.Labels {
		if l.Span.Valid() && l.Span.Start.Line > maxLine {
			maxLine = l.Span.Start.Line
		}
	}
	gw := len(fmt.Sprint(maxLine))
	pad := strings.Repeat(" ", gw)
	bar := s.wrap(dim, "│")

	fmt.Fprintf(w, "%s%s %s:%d:%d\n",
		pad, s.wrap(dim, "┌─"), span.File.Name, span.Start.Line, span.Start.Col)
	fmt.Fprintf(w, "%s %s\n", pad, bar)

	s.renderLabel(w, gw, span, d.Primary.Msg, s.severityColor(d.Severity), '^')
	for _, l := range d.Labels {
		if l.Span.Valid() {
			fmt.Fprintf(w, "%s %s\n", pad, bar)
			s.renderLabel(w, gw, l.Span, l.Msg, cyan, '-')
		}
	}

	fmt.Fprintf(w, "%s %s\n", pad, bar)
	if d.Help != "" {
		fmt.Fprintf(w, "%s %s %s\n", pad, s.wrap(dim, "="),
			s.wrap(bold, "help: ")+d.Help)
	}
	fmt.Fprintln(w)
}

// renderLabel prints one source line with an underline beneath the span.
func (s Style) renderLabel(w io.Writer, gw int, span source.Span, msg, color string, mark rune) {
	line := span.File.LineText(span.Start.Line)
	num := fmt.Sprintf("%*d", gw, span.Start.Line)
	bar := s.wrap(dim, "│")

	fmt.Fprintf(w, "%s %s %s\n", s.wrap(dim, num), bar, line)

	// Underline width in runes, clamped to what is actually visible on this
	// line. A span covering a joined multi-line signature reports an end
	// column past the end of its first physical line, so the clamp is load
	// bearing, not defensive.
	runes := []rune(line)
	width := span.End.Col - span.Start.Col
	if span.End.Line != span.Start.Line {
		width = len(runes) - span.Start.Col + 1
	}
	if avail := len(runes) - span.Start.Col + 1; width > avail {
		width = avail
	}
	if width < 1 {
		width = 1
	}
	// Preserve tabs in the leading run so the caret lands under the right rune.
	var lead strings.Builder
	for i := 0; i < span.Start.Col-1 && i < len(runes); i++ {
		if runes[i] == '\t' {
			lead.WriteRune('\t')
		} else {
			lead.WriteRune(' ')
		}
	}
	underline := strings.Repeat(string(mark), width)
	tail := s.wrap(color, underline)
	if msg != "" {
		tail += " " + s.wrap(color, msg)
	}
	fmt.Fprintf(w, "%s %s %s%s\n", strings.Repeat(" ", gw), bar, lead.String(), tail)
}

// RenderAll writes every diagnostic in the bag followed by a summary line.
func (s Style) RenderAll(w io.Writer, b *Bag) {
	items := b.Items()
	for _, d := range items {
		s.Render(w, d)
	}
	errs := b.Count(Error)
	warns := b.Count(Warning)
	switch {
	case errs > 0 && warns > 0:
		fmt.Fprintf(w, "%s: %d error(s), %d warning(s)\n", s.wrap(red, "failed"), errs, warns)
	case errs > 0:
		fmt.Fprintf(w, "%s: %d error(s)\n", s.wrap(red, "failed"), errs)
	case warns > 0:
		fmt.Fprintf(w, "%s: %d warning(s)\n", s.wrap(yellow, "ok"), warns)
	}
}
