package assay_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/christopherwolf/dore/internal/assay"
)

func render(t *testing.T, rep *assay.Report) string {
	t.Helper()
	var buf bytes.Buffer
	assay.Renderer{Color: false}.Render(&buf, rep)
	return buf.String()
}

// A green assay says so plainly and reports what it covered.
func TestGreenReportStatesTheCoverage(t *testing.T) {
	rep := setup(t, refundSpec, `from decimal import Decimal

def refund_eligible(days_since_purchase, order_total):
    if order_total > Decimal("500.00"):
        return False
    return days_since_purchase <= 30
`)
	out := render(t, rep)
	for _, want := range []string{"refund_eligible", "4/4", "ok"} {
		if !strings.Contains(out, want) {
			t.Errorf("green report missing %q:\n%s", want, out)
		}
	}
}

// A failing row must show the inputs that produced it, the expected value, and
// the actual one. Without the inputs the reader cannot reproduce the failure.
func TestFailingRowShowsInputsExpectedAndActual(t *testing.T) {
	rep := setup(t, refundSpec, `from decimal import Decimal

def refund_eligible(days_since_purchase, order_total):
    if order_total > Decimal("500.00"):
        return False
    return days_since_purchase < 30
`)
	out := render(t, rep)
	for _, want := range []string{
		"days_since_purchase", "30",
		"order_total", "49.99",
		"expected", "true",
		"got", "false",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("failure report missing %q:\n%s", want, out)
		}
	}
}

// The note column exists so a row can say why it was written. It has to survive
// into the failure output or it is decoration.
func TestNoteIsReprintedOnFailure(t *testing.T) {
	rep := setup(t, refundSpec, `from decimal import Decimal

def refund_eligible(days_since_purchase, order_total):
    if order_total > Decimal("500.00"):
        return False
    return days_since_purchase < 30
`)
	out := render(t, rep)
	if !strings.Contains(out, "boundary, inclusive") {
		t.Errorf("the failing row's note should appear:\n%s", out)
	}
}

// A named scenario locates the failure by name rather than by table index.
func TestScenarioNameLocatesTheFailure(t *testing.T) {
	spec := `frozen fn f(a: int) -> out: bool
  examples:
    | a | out  |
    | 1 | true |
  scenario "large values are rejected":
    | a    | out   |
    | 9999 | false |
`
	rep := setup(t, spec, `def f(a):
    return True
`)
	out := render(t, rep)
	if !strings.Contains(out, "large values are rejected") {
		t.Errorf("scenario name should locate the failure:\n%s", out)
	}
}

// A run that could not happen is not a table of failing rows. Saying "0/4 rows"
// would blame the spec for the implementation being absent.
func TestProblemRendersDistinctlyFromRowFailures(t *testing.T) {
	rep := setup(t, refundSpec, `def something_else():
    return None
`)
	out := render(t, rep)
	if strings.Contains(out, "0/4") {
		t.Errorf("a missing function should not be reported as failing rows:\n%s", out)
	}
	if !strings.Contains(out, "refund_eligible") {
		t.Errorf("report should name the function:\n%s", out)
	}
}
