// Package source models Doré source files and positions within them.
//
// Every span carries enough information to render a diagnostic without
// re-reading the file, which is what lets the compiler report errors from
// any phase in a uniform way.
package source

import (
	"os"
	"strings"
)

// Pos is a byte offset into a File, plus the 1-indexed line and column it
// resolves to. Columns count runes, not bytes, so carets line up under
// non-ASCII text.
type Pos struct {
	Offset int
	Line   int
	Col    int
}

// Span is a half-open range [Start, End) within a single File.
type Span struct {
	File  *File
	Start Pos
	End   Pos
}

// Valid reports whether s refers to a real range. The zero Span does not.
func (s Span) Valid() bool { return s.File != nil && s.End.Offset >= s.Start.Offset }

// Sub carves a narrower span out of s, offset by rune counts relative to
// s.Start. Used to point at one cell inside a table row, or one word inside
// a signature, without re-scanning the file.
func (s Span) Sub(runeOff, runeLen int) Span {
	if !s.Valid() {
		return s
	}
	line := s.File.LineText(s.Start.Line)
	runes := []rune(line)
	col0 := s.Start.Col - 1
	if col0+runeOff > len(runes) {
		return s
	}
	byteOff := len(string(runes[:col0+runeOff])) - len(string(runes[:col0]))
	byteLen := 0
	if end := col0 + runeOff + runeLen; end <= len(runes) {
		byteLen = len(string(runes[col0+runeOff : end]))
	}
	start := Pos{
		Offset: s.Start.Offset + byteOff,
		Line:   s.Start.Line,
		Col:    s.Start.Col + runeOff,
	}
	return Span{
		File:  s.File,
		Start: start,
		End:   Pos{Offset: start.Offset + byteLen, Line: start.Line, Col: start.Col + runeLen},
	}
}

// File is a loaded source file with a precomputed line index.
type File struct {
	Name  string
	Text  string
	lines []int // byte offset of the start of each line
}

// Load reads a Doré source file from disk.
func Load(name string) (*File, error) {
	b, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	return New(name, string(b)), nil
}

// New builds a File from text already in memory.
func New(name, text string) *File {
	f := &File{Name: name, Text: text, lines: []int{0}}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			f.lines = append(f.lines, i+1)
		}
	}
	return f
}

// LineCount returns the number of lines in the file.
func (f *File) LineCount() int { return len(f.lines) }

// LineText returns line n (1-indexed) without its trailing newline.
func (f *File) LineText(n int) string {
	if n < 1 || n > len(f.lines) {
		return ""
	}
	start := f.lines[n-1]
	end := len(f.Text)
	if n < len(f.lines) {
		end = f.lines[n] - 1
	}
	if end > 0 && end <= len(f.Text) && end > start && f.Text[end-1] == '\r' {
		end--
	}
	if start > end {
		return ""
	}
	return f.Text[start:end]
}

// LineSpan returns a span covering all of line n, excluding the newline.
func (f *File) LineSpan(n int) Span {
	text := f.LineText(n)
	start := f.lines[n-1]
	return Span{
		File:  f,
		Start: Pos{Offset: start, Line: n, Col: 1},
		End:   Pos{Offset: start + len(text), Line: n, Col: len([]rune(text)) + 1},
	}
}

// Indent returns the number of leading spaces on line n, and whether the line
// is blank or comment-only.
func Indent(line string) (n int, blank bool) {
	for i, r := range line {
		if r != ' ' && r != '\t' {
			trimmed := strings.TrimSpace(line[i:])
			return i, trimmed == "" || strings.HasPrefix(trimmed, "#")
		}
	}
	return len(line), true
}
