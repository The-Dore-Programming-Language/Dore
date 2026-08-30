package types

import "math"

// Equal reports whether an actual result matches an expected touchstone cell.
//
// Comparison semantics belong to Doré, not to a backend. If each backend
// decided what "equal" meant, one spec would pass in Python and fail in
// TypeScript for reasons the touchstone never described — the same divergence
// the type system exists to prevent. Backends report a raw value; this decides.
func Equal(expected, actual Value) bool {
	if expected.IsRaise() || actual.IsRaise() {
		return expected.Raises == actual.Raises
	}
	switch expected.Type.Kind {
	case Bool:
		return expected.Bool == actual.Bool
	case Text:
		return expected.Text == actual.Text
	case Date:
		return expected.Date.Equal(actual.Date)
	case Float:
		return floatEqual(expected, actual)
	case Int, I32, I64, Money, Decimal:
		if expected.Int == nil || actual.Int == nil {
			return expected.Int == actual.Int
		}
		return expected.Int.Cmp(actual.Int) == 0
	}
	return false
}

// floatEqual compares within the tolerance the cell declared. A float output
// cell cannot be written without one, so the absence of a tolerance here means
// the value came from somewhere that skipped validation.
func floatEqual(expected, actual Value) bool {
	if math.IsNaN(expected.Float) || math.IsNaN(actual.Float) {
		return false
	}
	tol := expected.Tol
	if !expected.HasTol {
		return expected.Float == actual.Float
	}
	return math.Abs(expected.Float-actual.Float) <= tol
}
