package types

import "testing"

func mustType(t *testing.T, name string) Type {
	t.Helper()
	ty, ok := Named(name)
	if !ok {
		t.Fatalf("Named(%q) failed", name)
	}
	return ty
}

func TestParseExactTypes(t *testing.T) {
	tests := []struct {
		typ, in, want string
	}{
		{"int", "42", "42"},
		{"int", "-7", "-7"},
		{"int", "170141183460469231731687303715884105728", "170141183460469231731687303715884105728"},
		{"money", "49.99", "49.99"},
		{"money", "900", "900.00"},
		{"money", "-0.01", "-0.01"},
		{"bool", "true", "true"},
		{"text", `"hi"`, `"hi"`},
		{"date", "2026-08-27", "2026-08-27"},
		{"decimal(10,4)", "1.5", "1.5000"},
	}
	for _, tc := range tests {
		v, err := Parse(tc.in, mustType(t, tc.typ), true)
		if err != nil {
			t.Errorf("Parse(%q, %s): %v", tc.in, tc.typ, err)
			continue
		}
		if got := v.String(); got != tc.want {
			t.Errorf("Parse(%q, %s) = %s, want %s", tc.in, tc.typ, got, tc.want)
		}
	}
}

// Doré must never round a literal the author wrote. Silently accepting 49.999
// as money would make the touchstone assert something the source does not say.
func TestMoneyRejectsExcessPrecision(t *testing.T) {
	_, err := Parse("49.999", mustType(t, "money"), true)
	if err == nil {
		t.Fatal("expected 49.999 to be rejected for money")
	}
	if want := "3 decimal places"; !contains(err.Msg, want) {
		t.Errorf("message %q should mention %q", err.Msg, want)
	}
}

// Exact float equality is the most common source of flaky suites; the language
// refuses it in an output cell rather than inheriting the flakiness.
func TestFloatOutputRequiresTolerance(t *testing.T) {
	if _, err := Parse("0.333", mustType(t, "float"), true); err == nil {
		t.Fatal("expected a bare float output cell to be rejected")
	}
	v, err := Parse("0.333 ~1e-4", mustType(t, "float"), true)
	if err != nil {
		t.Fatalf("tolerance form rejected: %v", err)
	}
	if !v.HasTol || v.Tol != 1e-4 {
		t.Errorf("tolerance not parsed: %+v", v)
	}
	// An input cell has no such requirement: nothing is compared against it.
	if _, err := Parse("0.333", mustType(t, "float"), false); err != nil {
		t.Errorf("float input cell should not need a tolerance: %v", err)
	}
}

func TestFixedWidthOverflowTraps(t *testing.T) {
	if _, err := Parse("2147483648", mustType(t, "i32"), false); err == nil {
		t.Fatal("expected i32 overflow to be rejected")
	}
	if _, err := Parse("2147483647", mustType(t, "i32"), false); err != nil {
		t.Fatalf("i32 max should fit: %v", err)
	}
}

func TestRaiseCellsOnlyInOutputColumns(t *testing.T) {
	v, err := Parse("!InvalidInput", mustType(t, "bool"), true)
	if err != nil {
		t.Fatalf("raise cell rejected in output column: %v", err)
	}
	if !v.IsRaise() || v.Raises != "InvalidInput" {
		t.Errorf("raise not parsed: %+v", v)
	}
	if _, err := Parse("!InvalidInput", mustType(t, "bool"), false); err == nil {
		t.Fatal("expected `!` to be rejected in an input column")
	}
}

func TestSuggestTypo(t *testing.T) {
	if got := Suggest("flaot"); got != "float" {
		t.Errorf("Suggest(flaot) = %q, want float", got)
	}
	// Arrivals from other languages write these; they are three edits away
	// but unambiguous, and a prefix relation is what makes them safe to suggest.
	if got := Suggest("boolean"); got != "bool" {
		t.Errorf("Suggest(boolean) = %q, want bool", got)
	}
	if got := Suggest("integer"); got != "int" {
		t.Errorf("Suggest(integer) = %q, want int", got)
	}
	// Nothing close enough should suggest nothing rather than guess.
	if got := Suggest("varchar"); got != "" {
		t.Errorf("Suggest(varchar) = %q, want no suggestion", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
