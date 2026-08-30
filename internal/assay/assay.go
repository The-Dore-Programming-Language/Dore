// Package assay runs a touchstone against an implementation.
//
// This is the gate. Nothing here consults a model, and nothing a model wrote
// can influence the verdict: the expected values come from the human-authored
// spec, the actual values come from running the code, and Doré compares them.
package assay

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/christopherwolf/dore/internal/ast"
	"github.com/christopherwolf/dore/internal/backend"
	"github.com/christopherwolf/dore/internal/crucible"
	"github.com/christopherwolf/dore/internal/types"
)

// Options configure a run.
type Options struct {
	Backend backend.Backend
	Limits  crucible.Limits
	// KeepHarness leaves the generated harness on disk for debugging.
	KeepHarness bool
}

// RowResult is one touchstone row's verdict.
type RowResult struct {
	Table    *ast.Table
	TableIdx int
	RowIdx   int
	Row      ast.Row

	Passed bool
	// Expected is the output cell from the spec.
	Expected types.Value
	// Actual is what the implementation produced, when it produced anything.
	Actual    types.Value
	HasActual bool

	// Problem is set when the row could not be judged at all — the harness
	// failed, the value would not parse, the process died. Distinct from a
	// row that ran and disagreed.
	Problem string
}

// Cells returns the row's cells, so a report can show the inputs that produced
// a failure without reaching back into the AST.
func (r RowResult) Cells() []ast.Cell { return r.Row.Cells }

// FnResult collects every row for one function.
type FnResult struct {
	Fn   *ast.FnDecl
	Rows []RowResult
	// Problem is set when the whole function could not be assayed.
	Problem string
}

// Passed reports whether every row passed and nothing went wrong.
func (r *FnResult) Passed() bool {
	if r.Problem != "" {
		return false
	}
	for _, row := range r.Rows {
		if !row.Passed {
			return false
		}
	}
	return true
}

// Counts returns passing and total row counts.
func (r *FnResult) Counts() (passed, total int) {
	for _, row := range r.Rows {
		total++
		if row.Passed {
			passed++
		}
	}
	return passed, total
}

// Report is the outcome of assaying a whole file.
type Report struct {
	SpecPath string
	ImplPath string
	Backend  string
	Fns      []*FnResult
}

// Passed reports whether the whole assay is green.
func (r *Report) Passed() bool {
	for _, fn := range r.Fns {
		if !fn.Passed() {
			return false
		}
	}
	return true
}

// Counts totals rows across every function.
func (r *Report) Counts() (passed, total int) {
	for _, fn := range r.Fns {
		p, t := fn.Counts()
		passed += p
		total += t
	}
	return passed, total
}

// Run assays every frozen function in file against implPath.
//
// Live functions are skipped: their behavior comes from a model at runtime, so
// their rows are an evaluation to be scored, not a gate to be enforced. Scoring
// them is not built yet.
func Run(ctx context.Context, file *ast.File, specPath, implPath string, opts Options) (*Report, error) {
	rep := &Report{
		SpecPath: specPath,
		ImplPath: implPath,
		Backend:  opts.Backend.Name(),
	}

	workDir := filepath.Dir(implPath)
	if workDir == "" {
		workDir = "."
	}

	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FnDecl)
		if !ok || fn.Mode == ast.Live {
			continue
		}
		res := runFn(ctx, fn, workDir, filepath.Base(implPath), opts)
		rep.Fns = append(rep.Fns, res)
	}
	return rep, nil
}

func runFn(ctx context.Context, fn *ast.FnDecl, workDir, implFile string, opts Options) *FnResult {
	res := &FnResult{Fn: fn}

	// Build the row list first so a result can be matched back to its cell
	// even when the harness reports out of order or stops early.
	type slot struct {
		table    *ast.Table
		tableIdx int
		rowIdx   int
		row      ast.Row
		expected types.Value
	}
	var slots []slot
	for ti, table := range fn.Tables() {
		outCol := -1
		for ci, col := range table.Columns {
			if col.Kind == ast.ColOutput {
				outCol = ci
			}
		}
		if outCol < 0 {
			res.Problem = "table has no output column"
			return res
		}
		for ri, row := range table.Rows {
			slots = append(slots, slot{table, ti, ri, row, row.Cells[outCol].Value})
		}
	}
	if len(slots) == 0 {
		res.Problem = "no rows to assay"
		return res
	}

	src, err := opts.Backend.Harness(fn, implFile)
	if err != nil {
		res.Problem = err.Error()
		return res
	}

	harnessPath := filepath.Join(workDir, fmt.Sprintf(".dore-assay-%s%s", fn.Name, opts.Backend.Extension()))
	if err := os.WriteFile(harnessPath, []byte(src), 0o600); err != nil {
		res.Problem = fmt.Sprintf("writing harness: %v", err)
		return res
	}
	if !opts.KeepHarness {
		defer os.Remove(harnessPath)
	}

	prog, args := opts.Backend.Command(filepath.Base(harnessPath))
	run, err := crucible.Run(ctx, prog, args, workDir, opts.Limits)
	if err != nil {
		res.Problem = err.Error()
		return res
	}

	byKey := map[[2]int]backend.Result{}
	var harnessProblem string
	for _, line := range strings.Split(run.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var r backend.Result
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue
		}
		if r.Table < 0 {
			harnessProblem = r.Message
			continue
		}
		byKey[[2]int{r.Table, r.Row}] = r
	}

	if harnessProblem != "" {
		res.Problem = harnessProblem
		return res
	}
	if len(byKey) == 0 {
		res.Problem = harnessFailure(run)
		return res
	}

	for _, s := range slots {
		rr := RowResult{
			Table: s.table, TableIdx: s.tableIdx, RowIdx: s.rowIdx,
			Row: s.row, Expected: s.expected,
		}
		got, ok := byKey[[2]int{s.tableIdx, s.rowIdx}]
		switch {
		case !ok:
			rr.Problem = "the implementation never reported this row"
			if run.TimedOut {
				rr.Problem = "timed out before this row ran"
			}
		case got.Outcome == backend.Failed:
			rr.Problem = got.Message
		case got.Outcome == backend.Raised:
			rr.Actual = types.Value{Type: s.expected.Type, Raises: got.Error, Literal: "!" + got.Error}
			rr.HasActual = true
			rr.Passed = types.Equal(s.expected, rr.Actual)
		default:
			actual, perr := types.Parse(got.Repr, s.expected.Type, false)
			if perr != nil {
				rr.Problem = fmt.Sprintf("returned %q, which is not a valid %s: %s",
					got.Repr, s.expected.Type, perr.Msg)
				break
			}
			rr.Actual = actual
			rr.HasActual = true
			rr.Passed = types.Equal(s.expected, actual)
		}
		res.Rows = append(res.Rows, rr)
	}
	return res
}

// harnessFailure explains why nothing came back at all.
func harnessFailure(run *crucible.Result) string {
	if run.TimedOut {
		return fmt.Sprintf("timed out after %s without reporting a row", run.Duration.Round(1e6))
	}
	if s := strings.TrimSpace(run.Stderr); s != "" {
		lines := strings.Split(s, "\n")
		return "the harness failed: " + lines[len(lines)-1]
	}
	return fmt.Sprintf("the harness produced no output (exit %d)", run.ExitCode)
}
