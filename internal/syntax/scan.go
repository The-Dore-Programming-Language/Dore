// Package syntax turns Doré source text into an AST.
//
// The outer structure is scanned by line and indentation rather than by a
// general token stream. That is a deliberate choice, not a shortcut: an
// `intent:` block holds prose that must never be tokenized, an `examples:`
// block is a table whose cells are delimited by pipes, and only signatures and
// property expressions need real tokens. Scanning by line keeps prose intact,
// keeps cell spans exact for diagnostics, and avoids an INDENT/DEDENT lexer
// that would earn its complexity only in a language with nested expressions.
package syntax

import (
	"strings"

	"github.com/christopherwolf/dore/internal/source"
)

// logicalLine is one or more physical lines joined into a single unit.
//
// Lines join while parentheses are unbalanced, so a multi-line signature is
// parsed as one thing. pieces records where each physical line landed inside
// Text, which is what lets a diagnostic about a joined signature still point
// at the right physical line and column.
type logicalLine struct {
	Text   string // joined, with single spaces at join points
	Indent int
	Line   int // first physical line, 1-indexed
	file   *source.File
	pieces []piece
}

type piece struct {
	runeStart int // offset into logicalLine.Text, in runes
	runeLen   int
	line      int // physical line
	col       int // 1-indexed column in the physical line where the piece starts
}

// spanAt maps a rune range within Text back to a span in the original file.
func (l logicalLine) spanAt(runeOff, runeLen int) source.Span {
	for _, p := range l.pieces {
		if runeOff >= p.runeStart && runeOff < p.runeStart+p.runeLen {
			delta := runeOff - p.runeStart
			if delta+runeLen > p.runeLen {
				runeLen = p.runeLen - delta
			}
			if runeLen < 1 {
				runeLen = 1
			}
			return l.file.LineSpan(p.line).Sub(p.col-1+delta, runeLen)
		}
	}
	return l.file.LineSpan(l.Line)
}

// span covers the content of the logical line's first physical line, starting
// at the first non-space rune so carets do not underline indentation.
func (l logicalLine) span() source.Span {
	full := l.file.LineSpan(l.Line)
	n := len([]rune(l.Text))
	if n < 1 {
		n = 1
	}
	return full.Sub(l.Indent, n)
}

// scanLines splits a file into logical lines, dropping blanks and comments.
func scanLines(f *source.File) []logicalLine {
	var out []logicalLine
	var cur *logicalLine
	depth := 0

	for n := 1; n <= f.LineCount(); n++ {
		raw := f.LineText(n)
		indent, blank := source.Indent(raw)

		// A blank or comment line inside an open paren is skipped; outside
		// one it simply separates blocks.
		if blank {
			if depth == 0 {
				cur = nil
			}
			continue
		}

		body := strings.TrimRight(stripComment(raw[indent:]), " \t")
		if body == "" {
			if depth == 0 {
				cur = nil
			}
			continue
		}

		if depth > 0 && cur != nil {
			// Continuation of an unbalanced signature.
			start := len([]rune(cur.Text)) + 1
			cur.Text += " " + body
			cur.pieces = append(cur.pieces, piece{
				runeStart: start,
				runeLen:   len([]rune(body)),
				line:      n,
				col:       indent + 1,
			})
		} else {
			out = append(out, logicalLine{
				Text:   body,
				Indent: indent,
				Line:   n,
				file:   f,
				pieces: []piece{{runeStart: 0, runeLen: len([]rune(body)), line: n, col: indent + 1}},
			})
			cur = &out[len(out)-1]
		}
		depth += parenDelta(body)
		if depth < 0 {
			depth = 0
		}
		if depth == 0 {
			cur = nil
		}
	}
	return out
}

// stripComment removes a trailing `#` comment, respecting double-quoted text
// so a `#` inside a string literal survives.
func stripComment(s string) string {
	inStr, esc := false, false
	for i, r := range s {
		switch {
		case esc:
			esc = false
		case r == '\\' && inStr:
			esc = true
		case r == '"':
			inStr = !inStr
		case r == '#' && !inStr:
			return s[:i]
		}
	}
	return s
}

// parenDelta counts unmatched parens outside of string literals.
func parenDelta(s string) int {
	d, inStr, esc := 0, false, false
	for _, r := range s {
		switch {
		case esc:
			esc = false
		case r == '\\' && inStr:
			esc = true
		case r == '"':
			inStr = !inStr
		case inStr:
		case r == '(':
			d++
		case r == ')':
			d--
		}
	}
	return d
}
