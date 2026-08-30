package types

import "testing"

func mk(t *testing.T, typeName, lit string, expected bool) Value {
	t.Helper()
	ty, ok := Named(typeName)
	if !ok {
		t.Fatalf("unknown type %q", typeName)
	}
	v, err := Parse(lit, ty, expected)
	if err != nil {
		t.Fatalf("Parse(%q, %s): %v", lit, typeName, err)
	}
	return v
}

func TestEqual(t *testing.T) {
	tests := []struct {
		name          string
		typ           string
		want, got     string
		shouldBeEqual bool
	}{
		{"bool match", "bool", "true", "true", true},
		{"bool mismatch", "bool", "true", "false", false},
		{"money exact", "money", "49.99", "49.99", true},
		{"money differs by a cent", "money", "49.99", "50.00", false},
		{"money scale normalizes", "money", "900", "900.00", true},
		{"big int", "int", "170141183460469231731687303715884105728", "170141183460469231731687303715884105728", true},
		{"text match", "text", `"hi"`, `"hi"`, true},
		{"text is case sensitive", "text", `"Hi"`, `"hi"`, false},
		{"date match", "date", "2026-08-27", "2026-08-27", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := mk(t, tc.typ, tc.want, true)
			g := mk(t, tc.typ, tc.got, false)
			if Equal(w, g) != tc.shouldBeEqual {
				t.Errorf("Equal(%s, %s) = %v, want %v", tc.want, tc.got, !tc.shouldBeEqual, tc.shouldBeEqual)
			}
		})
	}
}

// The tolerance is the point of requiring one: 1/3 computed two ways differs in
// the last bits, and the touchstone must not care.
func TestEqualFloatUsesTolerance(t *testing.T) {
	want := mk(t, "float", "0.3333 ~1e-4", true)
	if got := mk(t, "float", "0.33334", false); !Equal(want, got) {
		t.Error("0.33334 should be within 1e-4 of 0.3333")
	}
	if got := mk(t, "float", "0.34", false); Equal(want, got) {
		t.Error("0.34 should be outside 1e-4 of 0.3333")
	}
}

func TestEqualRaises(t *testing.T) {
	want := mk(t, "bool", "!InvalidInput", true)
	same := mk(t, "bool", "!InvalidInput", true)
	other := mk(t, "bool", "!OtherError", true)
	value := mk(t, "bool", "true", false)

	if !Equal(want, same) {
		t.Error("identical raises should match")
	}
	if Equal(want, other) {
		t.Error("different error names should not match")
	}
	if Equal(want, value) {
		t.Error("a returned value should not satisfy an expected raise")
	}
	if Equal(value, want) {
		t.Error("a raise should not satisfy an expected value")
	}
}
