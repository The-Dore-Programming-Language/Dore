package syntax

import (
	"testing"

	"github.com/christopherwolf/dore/internal/ast"
	"github.com/christopherwolf/dore/internal/source"
)

func parseOK(t *testing.T, src string) *ast.FnDecl {
	t.Helper()
	f, bag := Parse(source.New("test.dore", src))
	if bag.HasErrors() {
		for _, d := range bag.Items() {
			t.Errorf("unexpected diagnostic: %s: %s", d.Code, d.Msg)
		}
		t.FailNow()
	}
	if len(f.Decls) != 1 {
		t.Fatalf("got %d decls, want 1", len(f.Decls))
	}
	return f.Decls[0].(*ast.FnDecl)
}

// A signature may wrap across lines. The scanner joins on unbalanced parens,
// and spans must still resolve back to the right physical line.
func TestMultiLineSignature(t *testing.T) {
	fn := parseOK(t, `frozen fn f(
  a: int,
  b: money,
) -> out: bool
  examples:
    | a | b    | out  |
    | 1 | 2.00 | true |
`)
	if len(fn.Params) != 2 {
		t.Fatalf("got %d params, want 2", len(fn.Params))
	}
	if fn.Params[1].Name != "b" || fn.Params[1].Type.String() != "money" {
		t.Errorf("param 1 = %+v", fn.Params[1])
	}
	if fn.Result.Name != "out" {
		t.Errorf("result = %q, want out", fn.Result.Name)
	}
	// The span for `b` must point at line 3, not the joined line 1.
	if got := fn.Params[1].Span.Start.Line; got != 3 {
		t.Errorf("param b span on line %d, want 3", got)
	}
}

// Intent is prose. It must survive verbatim and never be tokenized, since it
// informs generation but is never checked.
func TestIntentIsVerbatim(t *testing.T) {
	fn := parseOK(t, `frozen fn f(a: int) -> out: bool
  intent:
    Approve when a > 30: that is the rule.
    Costs over $500.00 (any currency) escalate.
  examples:
    | a | out  |
    | 1 | true |
`)
	in := fn.Intent()
	if in == nil || len(in.Lines) != 2 {
		t.Fatalf("intent = %+v", in)
	}
	if in.Lines[0] != "Approve when a > 30: that is the rule." {
		t.Errorf("line 0 = %q", in.Lines[0])
	}
	if in.Lines[1] != "Costs over $500.00 (any currency) escalate." {
		t.Errorf("line 1 = %q", in.Lines[1])
	}
}

func TestScenarioGrouping(t *testing.T) {
	fn := parseOK(t, `frozen fn f(a: int) -> out: bool
  examples:
    | a | out  |
    | 1 | true |
  scenario "large values are rejected":
    | a    | out   |
    | 9999 | false |
`)
	tables := fn.Tables()
	if len(tables) != 2 {
		t.Fatalf("got %d tables, want 2", len(tables))
	}
	if tables[0].IsScenario() {
		t.Error("bare examples block should not be a scenario")
	}
	if tables[1].Label != "large values are rejected" {
		t.Errorf("label = %q", tables[1].Label)
	}
	if fn.RowCount() != 2 {
		t.Errorf("RowCount = %d, want 2", fn.RowCount())
	}
}

// A `#` inside a quoted cell is data, not a comment.
func TestCommentStrippingRespectsStrings(t *testing.T) {
	fn := parseOK(t, `frozen fn f(tag: text) -> out: bool  # trailing comment
  examples:
    | tag      | out  |
    | "#hash"  | true |
`)
	cell := fn.Tables()[0].Rows[0].Cells[0]
	if cell.Raw != `"#hash"` {
		t.Errorf("cell = %q, want %q", cell.Raw, `"#hash"`)
	}
}

func TestLiveMode(t *testing.T) {
	fn := parseOK(t, `live fn f(m: text) -> out: text
`)
	if fn.Mode != ast.Live {
		t.Errorf("mode = %v, want live", fn.Mode)
	}
}

// Parsing must not stop at the first error: an author fixing five typos should
// see five diagnostics, not run the compiler five times.
func TestErrorsDoNotCascade(t *testing.T) {
	_, bag := Parse(source.New("test.dore", `frozen fn f(a: nope) -> out: bool
  examples:
    | a | out |
    | 1 | 2   |

frozen fn g(a: int) -> out: alsonope
  examples:
    | a | out |
    | 1 | 2   |
`))
	codes := map[string]bool{}
	for _, d := range bag.Items() {
		codes[d.Code] = true
	}
	if !codes["E0015"] {
		t.Error("expected unknown-type diagnostics")
	}
	if n := len(bag.Items()); n < 2 {
		t.Errorf("got %d diagnostics, want both declarations reported", n)
	}
}
