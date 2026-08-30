// Package ast defines Doré's syntax tree.
//
// Nodes are sealed: the marker methods keep the set closed so a type switch
// over them can be checked for exhaustiveness by a linter. Go will not do it
// for us, so the discipline has to be deliberate.
package ast

import (
	"github.com/christopherwolf/dore/internal/source"
	"github.com/christopherwolf/dore/internal/types"
)

// File is one parsed .dore source file.
type File struct {
	Name  string
	Decls []Decl
}

// Decl is a top-level declaration.
type Decl interface{ declNode() }

// Mode distinguishes a compiled-and-frozen function from one that calls a
// model at runtime.
type Mode int

const (
	// Frozen functions are compiled once, verified, cached, and frozen.
	// No model is present at runtime. A frozen function requires examples.
	Frozen Mode = iota
	// Live functions are a schema-constrained model call at runtime. Their
	// examples are an evaluation reported as a pass rate, not a gate.
	Live
)

func (m Mode) String() string {
	if m == Live {
		return "live"
	}
	return "frozen"
}

// Param is one input to a function.
type Param struct {
	Name     string
	Type     types.Type
	TypeName string
	Span     source.Span
	TypeSpan source.Span
}

// Result names the single output of a function. Naming it is what lets a
// touchstone column refer to the output the same way it refers to an input.
type Result struct {
	Name     string
	Type     types.Type
	TypeName string
	Span     source.Span
	TypeSpan source.Span
}

// FnDecl is a `frozen fn` or `live fn` declaration.
type FnDecl struct {
	Mode     Mode
	Name     string
	NameSpan source.Span
	Span     source.Span // the signature header
	Params   []Param
	Result   Result
	Sections []Section
}

func (*FnDecl) declNode() {}

// Intent returns the prose block, or nil when there is none.
func (f *FnDecl) Intent() *Intent {
	for _, s := range f.Sections {
		if i, ok := s.(*Intent); ok {
			return i
		}
	}
	return nil
}

// Tables returns every examples and scenario block in source order. These are
// the oracle: the only sections that gate a build.
func (f *FnDecl) Tables() []*Table {
	var out []*Table
	for _, s := range f.Sections {
		if t, ok := s.(*Table); ok {
			out = append(out, t)
		}
	}
	return out
}

// RowCount totals the rows across every table.
func (f *FnDecl) RowCount() int {
	n := 0
	for _, t := range f.Tables() {
		n += len(t.Rows)
	}
	return n
}

// Properties returns every property block.
func (f *FnDecl) Properties() []*Property {
	var out []*Property
	for _, s := range f.Sections {
		if p, ok := s.(*Property); ok {
			out = append(out, p)
		}
	}
	return out
}

// Section is a block inside a function declaration.
type Section interface{ sectionNode() }

// Intent is the prose block. It informs generation and is never checked.
// Changing it does not invalidate an existing artifact, which is why it is
// excluded from the spec hash.
type Intent struct {
	Lines []string
	Span  source.Span
}

func (*Intent) sectionNode() {}

// Table is an `examples:` or `scenario "...":` block. Both are the same
// structure; a scenario is a named group whose name appears in failure output.
type Table struct {
	Label     string // empty for a bare examples block
	LabelSpan source.Span
	Span      source.Span
	Columns   []Column
	Rows      []Row
}

func (*Table) sectionNode() {}

// IsScenario reports whether this table was written as a named scenario.
func (t *Table) IsScenario() bool { return t.Label != "" }

// Column is one header cell in a table.
type Column struct {
	Name string
	Span source.Span
	// Binding is resolved during checking: which parameter or result this
	// column feeds. Note columns bind to nothing and are ignored.
	Kind ColumnKind
	Type types.Type
}

// ColumnKind records what a table column refers to.
type ColumnKind int

const (
	ColUnresolved ColumnKind = iota
	ColInput
	ColOutput
	ColNote // free text, ignored by the compiler, reprinted on failure
)

// Row is one case in a table.
type Row struct {
	Cells []Cell
	Span  source.Span
}

// Cell is one literal in a row. Value is filled in during checking.
type Cell struct {
	Raw   string
	Span  source.Span
	Value types.Value
}

// Property is a `property name:` block, checked against generated inputs
// rather than a fixed row. Properties cover the regions no row touches.
type Property struct {
	Name     string
	NameSpan source.Span
	Body     []string
	Span     source.Span
}

func (*Property) sectionNode() {}
