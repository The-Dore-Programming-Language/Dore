package assay_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/christopherwolf/dore/internal/assay"
	"github.com/christopherwolf/dore/internal/backend/python"
	"github.com/christopherwolf/dore/internal/check"
	"github.com/christopherwolf/dore/internal/source"
	"github.com/christopherwolf/dore/internal/syntax"
)

const refundSpec = `frozen fn refund_eligible(days_since_purchase: int, order_total: money) -> approved: bool
  examples:
    | days_since_purchase | order_total | approved | note                |
    | 5                   | 49.99       | true     | ordinary            |
    | 30                  | 49.99       | true     | boundary, inclusive |
    | 31                  | 49.99       | false    | boundary, exclusive |
    | 5                   | 900.00      | false    | limit beats recency |
`

// setup writes a spec and an implementation, then assays them.
func setup(t *testing.T, spec, impl string) *assay.Report {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not on PATH")
	}

	dir := t.TempDir()
	specPath := filepath.Join(dir, "spec.dore")
	implPath := filepath.Join(dir, "impl.py")
	mustWrite(t, specPath, spec)
	mustWrite(t, implPath, impl)

	f, err := source.Load(specPath)
	if err != nil {
		t.Fatal(err)
	}
	file, bag := syntax.Parse(f)
	bag.Merge(check.File(file))
	if bag.HasErrors() {
		t.Fatalf("fixture spec does not check: %v", bag.Items()[0].Msg)
	}

	rep, err := assay.Run(context.Background(), file, specPath, implPath,
		assay.Options{Backend: python.Backend{}})
	if err != nil {
		t.Fatalf("assay.Run: %v", err)
	}
	return rep
}

// A correct implementation turns every row green.
func TestCorrectImplementationPassesEveryRow(t *testing.T) {
	rep := setup(t, refundSpec, `from decimal import Decimal

def refund_eligible(days_since_purchase, order_total):
    if order_total > Decimal("500.00"):
        return False
    return days_since_purchase <= 30
`)
	if !rep.Passed() {
		t.Errorf("expected a green assay, got: %s", firstProblem(rep))
	}
	if passed, total := rep.Counts(); passed != 4 || total != 4 {
		t.Errorf("counts = %d/%d, want 4/4", passed, total)
	}
}

// An off-by-one fails exactly the row that catches it, and names the value it
// produced. The other rows still report, so one run shows the whole picture.
func TestOffByOneFailsOnlyTheBoundaryRow(t *testing.T) {
	rep := setup(t, refundSpec, `from decimal import Decimal

def refund_eligible(days_since_purchase, order_total):
    if order_total > Decimal("500.00"):
        return False
    return days_since_purchase < 30
`)
	if rep.Passed() {
		t.Fatal("expected the assay to fail")
	}
	passed, total := rep.Counts()
	if passed != 3 || total != 4 {
		t.Fatalf("counts = %d/%d, want 3/4", passed, total)
	}

	fn := rep.Fns[0]
	failed := []int{}
	for _, row := range fn.Rows {
		if !row.Passed {
			failed = append(failed, row.RowIdx)
		}
	}
	if len(failed) != 1 || failed[0] != 1 {
		t.Fatalf("failed rows = %v, want only row index 1 (30 days)", failed)
	}
	if got := fn.Rows[1].Actual.String(); got != "false" {
		t.Errorf("actual = %q, want false", got)
	}
	if !fn.Rows[1].HasActual {
		t.Error("a row that ran should carry the value it produced")
	}
}

// Money must not round-trip through float: 0.1 + 0.2 is a classic near-miss
// that an exact comparison has to catch.
func TestMoneyIsComparedExactly(t *testing.T) {
	spec := `frozen fn total(a: money, b: money) -> sum: money
  examples:
    | a    | b    | sum  |
    | 0.10 | 0.20 | 0.30 |
`
	rep := setup(t, spec, `from decimal import Decimal

def total(a, b):
    return Decimal(str(float(a) + float(b)))
`)
	if rep.Passed() {
		t.Error("float arithmetic on money should not satisfy an exact decimal row")
	}
}

// A raised error satisfies a row that expected one, and only that one.
func TestExpectedRaiseIsSatisfiedByTheNamedError(t *testing.T) {
	spec := `frozen fn half(n: int) -> out: int
  examples:
    | n  | out            |
    | 4  | 2              |
    | -1 | !ValueError    |
`
	rep := setup(t, spec, `def half(n):
    if n < 0:
        raise ValueError("negative")
    return n // 2
`)
	if !rep.Passed() {
		t.Errorf("expected green, got: %s", firstProblem(rep))
	}
}

// A missing function is a problem with the run, not a table of failing rows.
// Reporting four failures would blame the spec for the implementation's absence.
func TestMissingFunctionIsReportedAsAProblem(t *testing.T) {
	rep := setup(t, refundSpec, `def something_else():
    return None
`)
	if rep.Passed() {
		t.Fatal("expected failure")
	}
	fn := rep.Fns[0]
	if fn.Problem == "" {
		t.Fatal("expected a problem on the function, got none")
	}
	if !strings.Contains(fn.Problem, "refund_eligible") {
		t.Errorf("problem should name the missing function, got %q", fn.Problem)
	}
	if len(fn.Rows) != 0 {
		t.Errorf("a missing function should produce no row verdicts, got %d", len(fn.Rows))
	}
}

func firstProblem(rep *assay.Report) string {
	for _, fn := range rep.Fns {
		if fn.Problem != "" {
			return fn.Problem
		}
		for _, r := range fn.Rows {
			if r.Problem != "" {
				return r.Problem
			}
			if !r.Passed {
				return "row " + r.Expected.String() + " != " + r.Actual.String()
			}
		}
	}
	return "(none)"
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
