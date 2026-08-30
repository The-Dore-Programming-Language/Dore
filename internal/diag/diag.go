// Package diag defines Doré's diagnostic type and its renderer.
//
// Every phase of the compiler returns Diagnostics rather than error values.
// Error message quality is most of what people mean when they call a language
// finished, so the diagnostic type is a first-class artifact: it carries a
// stable code, a primary span, secondary labels, and optional help.
package diag

import (
	"sort"

	"github.com/christopherwolf/dore/internal/source"
)

// Severity ranks a diagnostic. Only Error fails a build.
type Severity int

const (
	Error Severity = iota
	Warning
	Note
)

func (s Severity) String() string {
	switch s {
	case Warning:
		return "warning"
	case Note:
		return "note"
	default:
		return "error"
	}
}

// Label annotates a span with a short message rendered beneath the caret.
type Label struct {
	Span source.Span
	Msg  string
}

// Diagnostic is one compiler message.
//
// Code is a stable identifier (E0104) so messages can be documented,
// suppressed, and searched for without depending on prose that may be
// reworded later.
type Diagnostic struct {
	Severity Severity
	Code     string
	Msg      string
	Primary  Label
	Labels   []Label
	Help     string
}

// New builds an error diagnostic pointing at span.
func New(code, msg string, span source.Span, label string) *Diagnostic {
	return &Diagnostic{
		Severity: Error,
		Code:     code,
		Msg:      msg,
		Primary:  Label{Span: span, Msg: label},
	}
}

// WithHelp attaches a help line suggesting a fix. Help text should say what to
// do, not restate what went wrong.
func (d *Diagnostic) WithHelp(help string) *Diagnostic {
	d.Help = help
	return d
}

// WithLabel attaches a secondary annotation, used to point at the declaration
// a violation contradicts.
func (d *Diagnostic) WithLabel(span source.Span, msg string) *Diagnostic {
	d.Labels = append(d.Labels, Label{Span: span, Msg: msg})
	return d
}

// Bag accumulates diagnostics across phases so a single run can report every
// independent problem rather than stopping at the first.
type Bag struct {
	items []*Diagnostic
}

// Add appends a diagnostic. A nil diagnostic is ignored so callers can add
// unconditionally.
func (b *Bag) Add(d *Diagnostic) {
	if d != nil {
		b.items = append(b.items, d)
	}
}

// Merge appends every diagnostic from other.
func (b *Bag) Merge(other *Bag) {
	if other != nil {
		b.items = append(b.items, other.items...)
	}
}

// Items returns the diagnostics in source order.
func (b *Bag) Items() []*Diagnostic {
	sort.SliceStable(b.items, func(i, j int) bool {
		a, c := b.items[i].Primary.Span, b.items[j].Primary.Span
		if a.File == nil || c.File == nil {
			return false
		}
		if a.File.Name != c.File.Name {
			return a.File.Name < c.File.Name
		}
		return a.Start.Offset < c.Start.Offset
	})
	return b.items
}

// HasErrors reports whether the bag holds anything that should fail a build.
func (b *Bag) HasErrors() bool {
	for _, d := range b.items {
		if d.Severity == Error {
			return true
		}
	}
	return false
}

// Len returns the number of diagnostics held.
func (b *Bag) Len() int { return len(b.items) }

// Count returns how many diagnostics of the given severity the bag holds.
func (b *Bag) Count(s Severity) int {
	n := 0
	for _, d := range b.items {
		if d.Severity == s {
			n++
		}
	}
	return n
}
