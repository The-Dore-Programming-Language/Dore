// Package check resolves and validates a parsed Doré file.
//
// The compiler's job is to refuse. Everything here exists to reject specs that
// cannot be checked, before a model is ever involved.
package check

import (
	"fmt"
	"sort"
	"strings"

	"github.com/christopherwolf/dore/internal/ast"
	"github.com/christopherwolf/dore/internal/diag"
	"github.com/christopherwolf/dore/internal/types"
)

// File validates every declaration in f, filling in column bindings and cell
// values as it goes.
func File(f *ast.File) *diag.Bag {
	bag := &diag.Bag{}
	seen := map[string]*ast.FnDecl{}

	for _, d := range f.Decls {
		fn, ok := d.(*ast.FnDecl)
		if !ok {
			continue
		}
		if prev, dup := seen[fn.Name]; dup {
			bag.Add(diag.New("E0100", fmt.Sprintf("function `%s` is declared twice", fn.Name),
				fn.NameSpan, "redeclared here").
				WithLabel(prev.NameSpan, "first declared here").
				WithHelp("each function name must be unique within a file; the name is part of the spec hash"))
			continue
		}
		seen[fn.Name] = fn
		checkFn(fn, bag)
	}
	return bag
}

func checkFn(fn *ast.FnDecl, bag *diag.Bag) {
	checkParams(fn, bag)

	tables := fn.Tables()

	// No oracle, no freeze. The central rule: everything else follows from it.
	if fn.Mode == ast.Frozen && len(tables) == 0 {
		bag.Add(diag.New("E0101", fmt.Sprintf("frozen function `%s` has no examples", fn.Name),
			fn.NameSpan, "nothing verifies this function").
			WithHelp("a frozen function is compiled once and verified against its examples; without them there is nothing to gate generation. Add an `examples:` block, or declare it `live fn` if the behavior is genuinely fuzzy"))
	}

	if fn.Mode == ast.Live && len(tables) > 0 {
		bag.Add(&diag.Diagnostic{
			Severity: diag.Note,
			Code:     "N0001",
			Msg:      fmt.Sprintf("examples on live function `%s` are an evaluation, not a gate", fn.Name),
			Primary:  diag.Label{Span: tables[0].Span, Msg: "reported as a pass rate"},
			Help:     "a live function calls a model at runtime, so its behavior cannot be frozen. These rows will be scored, not enforced",
		})
	}

	for _, t := range tables {
		checkTable(fn, t, bag)
	}
	checkProperties(fn, bag)
}

func checkParams(fn *ast.FnDecl, bag *diag.Bag) {
	seen := map[string]ast.Param{}
	for _, p := range fn.Params {
		if prev, dup := seen[p.Name]; dup {
			bag.Add(diag.New("E0102", fmt.Sprintf("duplicate parameter `%s`", p.Name),
				p.Span, "redeclared here").
				WithLabel(prev.Span, "first declared here"))
			continue
		}
		seen[p.Name] = p
		if p.Name == fn.Result.Name {
			bag.Add(diag.New("E0103", fmt.Sprintf("parameter `%s` shadows the result name", p.Name),
				p.Span, "same name as the result").
				WithLabel(fn.Result.Span, "result declared here").
				WithHelp("touchstone columns refer to inputs and the result by name, so the names must be distinct"))
		}
	}
}

func checkTable(fn *ast.FnDecl, t *ast.Table, bag *diag.Bag) {
	// Resolve every column to a parameter, the result, or a note.
	byName := map[string]types.Type{}
	for _, p := range fn.Params {
		byName[p.Name] = p.Type
	}

	seen := map[string]int{}
	var covered []string

	for i := range t.Columns {
		col := &t.Columns[i]
		if prev, dup := seen[col.Name]; dup {
			bag.Add(diag.New("E0104", fmt.Sprintf("duplicate column `%s`", col.Name),
				col.Span, "appears twice in this header").
				WithLabel(t.Columns[prev].Span, "first appearance"))
			continue
		}
		seen[col.Name] = i

		switch {
		case col.Name == "note":
			col.Kind = ast.ColNote
		case col.Name == fn.Result.Name:
			col.Kind, col.Type = ast.ColOutput, fn.Result.Type
			covered = append(covered, col.Name)
		default:
			if ty, ok := byName[col.Name]; ok {
				col.Kind, col.Type = ast.ColInput, ty
				covered = append(covered, col.Name)
			} else {
				d := diag.New("E0105", fmt.Sprintf("column `%s` matches no input or result", col.Name),
					col.Span, "unknown column")
				if s := suggest(col.Name, knownNames(fn)); s != "" {
					d.WithHelp(fmt.Sprintf("did you mean `%s`? Columns bind by name to the signature, and `note` is reserved for free text", s))
				} else {
					d.WithHelp("columns must be named after an input or the result. Signature declares: " +
						strings.Join(knownNames(fn), ", ") + ". A column named `note` holds free text and is ignored")
				}
				bag.Add(d)
			}
		}
	}

	// Every input and the result must appear. A table that omits a column is
	// not an incomplete oracle, it is an unusable one.
	var missing []string
	for _, p := range fn.Params {
		if _, ok := seen[p.Name]; !ok {
			missing = append(missing, p.Name)
		}
	}
	if _, ok := seen[fn.Result.Name]; !ok {
		missing = append(missing, fn.Result.Name)
	}
	if len(missing) > 0 {
		label := t.Span
		if len(t.Columns) > 0 {
			label = t.Columns[0].Span
		}
		bag.Add(diag.New("E0106", fmt.Sprintf("table is missing %s", plural(missing)),
			label, fmt.Sprintf("no column for %s", strings.Join(quoteAll(missing), ", "))).
			WithLabel(fn.Span, "signature declares them here").
			WithHelp("every input and the result need a column; a row that leaves one unspecified cannot be run"))
		// Keep going. Cells in the columns that did resolve are still worth
		// checking, and reporting them now saves the author another run.
	}
	_ = covered

	// Typecheck every cell against its column.
	for ri := range t.Rows {
		row := &t.Rows[ri]
		for ci := range row.Cells {
			if ci >= len(t.Columns) {
				break
			}
			col := t.Columns[ci]
			cell := &row.Cells[ci]

			if col.Kind == ast.ColNote {
				cell.Value = types.Value{Text: cell.Raw, Literal: cell.Raw}
				continue
			}
			if col.Kind == ast.ColUnresolved {
				continue // already reported on the header
			}
			if col.Type.Kind == types.Invalid {
				continue // the type annotation was already reported
			}

			v, perr := types.Parse(cell.Raw, col.Type, col.Kind == ast.ColOutput)
			if perr != nil {
				d := diag.New("E0107",
					fmt.Sprintf("cell does not match declared type `%s`", col.Type),
					cell.Span, perr.Msg).
					WithLabel(col.Span, fmt.Sprintf("column `%s` is %s", col.Name, col.Type))
				if perr.Help != "" {
					d.WithHelp(perr.Help)
				}
				bag.Add(d)
				continue
			}
			cell.Value = v
		}
	}
}

func checkProperties(fn *ast.FnDecl, bag *diag.Bag) {
	seen := map[string]*ast.Property{}
	for _, p := range fn.Properties() {
		if p.Name == "" {
			continue
		}
		if prev, dup := seen[p.Name]; dup {
			bag.Add(diag.New("E0108", fmt.Sprintf("duplicate property `%s`", p.Name),
				p.NameSpan, "redeclared here").
				WithLabel(prev.NameSpan, "first declared here"))
			continue
		}
		seen[p.Name] = p
	}
}

func knownNames(fn *ast.FnDecl) []string {
	out := make([]string, 0, len(fn.Params)+1)
	for _, p := range fn.Params {
		out = append(out, p.Name)
	}
	out = append(out, fn.Result.Name)
	sort.Strings(out)
	return out
}

func quoteAll(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "`" + n + "`"
	}
	return out
}

func plural(missing []string) string {
	if len(missing) == 1 {
		return "a column"
	}
	return fmt.Sprintf("%d columns", len(missing))
}

// suggest returns the closest candidate to typed, or "" when none is close.
func suggest(typed string, candidates []string) string {
	best, bestDist := "", 1<<30
	for _, c := range candidates {
		if d := editDistance(strings.ToLower(typed), strings.ToLower(c)); d < bestDist {
			best, bestDist = c, d
		}
	}
	if bestDist > 0 && bestDist <= 3 && bestDist < len(typed) {
		return best
	}
	return ""
}

func editDistance(a, b string) int {
	ar, br := []rune(a), []rune(b)
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, min(cur[j-1]+1, prev[j-1]+cost))
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}
