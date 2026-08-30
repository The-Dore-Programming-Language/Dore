package types

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
)

// Value is a literal from a touchstone cell, already validated against the
// declared type of its column.
//
// Exact types keep a big.Int (scaled by Scale for decimals) so that money
// never round-trips through float. Float is the one inexact type, and an
// expected-output cell of that type must carry a Tol.
type Value struct {
	Type    Type
	Int     *big.Int // Int, I32, I64, Money, Decimal (scaled)
	Float   float64  // Float
	Text    string   // Text
	Bool    bool     // Bool
	Date    time.Time
	Tol     float64 // Float only; tolerance for comparison
	HasTol  bool
	Raises  string // non-empty when the cell asserts a raised error
	Literal string // the source text, preserved for round-tripping and hashing
}

// IsRaise reports whether this cell asserts an error rather than a value.
func (v Value) IsRaise() bool { return v.Raises != "" }

// String renders the value back to its canonical literal form. This is what
// the spec hash sees, so it must not vary with formatting in the source.
func (v Value) String() string {
	if v.IsRaise() {
		return "!" + v.Raises
	}
	switch v.Type.Kind {
	case Bool:
		return strconv.FormatBool(v.Bool)
	case Text:
		return strconv.Quote(v.Text)
	case Float:
		s := strconv.FormatFloat(v.Float, 'g', -1, 64)
		if v.HasTol {
			s += " ~" + strconv.FormatFloat(v.Tol, 'g', -1, 64)
		}
		return s
	case Date:
		return v.Date.Format("2006-01-02")
	case Money, Decimal:
		return scaledString(v.Int, v.Type.Scale)
	default:
		if v.Int == nil {
			return "0"
		}
		return v.Int.String()
	}
}

func scaledString(n *big.Int, scale int) string {
	if n == nil {
		return "0"
	}
	neg := n.Sign() < 0
	abs := new(big.Int).Abs(n).String()
	if scale == 0 {
		if neg {
			return "-" + abs
		}
		return abs
	}
	for len(abs) <= scale {
		abs = "0" + abs
	}
	out := abs[:len(abs)-scale] + "." + abs[len(abs)-scale:]
	if neg {
		return "-" + out
	}
	return out
}

// ParseError describes why a literal did not fit its declared type. It carries
// a rune offset within the cell so the caller can point a caret at the exact
// character rather than the whole cell.
type ParseError struct {
	Msg  string
	Help string
}

func (e *ParseError) Error() string { return e.Msg }

func perr(msg, help string) *ParseError { return &ParseError{Msg: msg, Help: help} }

// Parse validates raw against t and returns the resulting Value.
//
// expected marks a cell in an output column. Float is rejected there without a
// tolerance: exact float equality is the most common source of flaky test
// suites in existence, and a spec language that permits it by default inherits
// that flakiness as a language feature.
func Parse(raw string, t Type, expected bool) (Value, *ParseError) {
	raw = strings.TrimSpace(raw)
	v := Value{Type: t, Literal: raw}

	if raw == "" {
		return v, perr("empty cell", "every cell needs a literal; there is no implicit default")
	}

	if strings.HasPrefix(raw, "!") {
		name := strings.TrimSpace(raw[1:])
		if name == "" {
			return v, perr("expected an error name after `!`", "write `!InvalidInput` to assert that the call raises")
		}
		if !expected {
			return v, perr("`!` is only valid in an output column",
				"inputs are values; move this assertion to the output column")
		}
		v.Raises = name
		return v, nil
	}

	switch t.Kind {
	case Bool:
		switch raw {
		case "true":
			v.Bool = true
		case "false":
			v.Bool = false
		default:
			return v, perr(fmt.Sprintf("%q is not a bool literal", raw), "bool literals are `true` or `false`")
		}

	case Text:
		s, err := strconv.Unquote(raw)
		if err != nil {
			return v, perr("text literals must be double-quoted",
				fmt.Sprintf("write %s instead of %s", strconv.Quote(raw), raw))
		}
		v.Text = s

	case Int, I32, I64:
		n, ok := new(big.Int).SetString(raw, 10)
		if !ok {
			return v, perr(fmt.Sprintf("%q is not an integer literal", raw),
				"integer literals are digits with an optional leading `-`")
		}
		if err := checkWidth(n, t); err != nil {
			return v, err
		}
		v.Int = n

	case Money, Decimal:
		n, err := parseScaled(raw, t.Scale)
		if err != nil {
			return v, err
		}
		if t.Kind == Decimal {
			digits := len(strings.TrimLeft(new(big.Int).Abs(n).String(), "0"))
			if digits > t.Precision {
				return v, perr(fmt.Sprintf("%s needs %d significant digits, but the type allows %d", raw, digits, t.Precision),
					fmt.Sprintf("widen the type to decimal(%d,%d) or use a smaller value", digits, t.Scale))
			}
		}
		v.Int = n

	case Float:
		lit, tol, hasTol, err := splitTolerance(raw)
		if err != nil {
			return v, err
		}
		f, ferr := strconv.ParseFloat(lit, 64)
		if ferr != nil {
			return v, perr(fmt.Sprintf("%q is not a float literal", lit), "float literals look like `1.5`, `-0.25`, or `1e-9`")
		}
		if math.IsInf(f, 0) {
			return v, perr("float literal overflows binary64", "use a smaller magnitude")
		}
		if expected && !hasTol {
			return v, perr("float output cell needs a tolerance",
				fmt.Sprintf("write `%s ~1e-9` — exact float equality makes touchstones flaky. For money amounts, use the `money` type instead", lit))
		}
		v.Float, v.Tol, v.HasTol = f, tol, hasTol

	case Date:
		d, derr := time.Parse("2006-01-02", raw)
		if derr != nil {
			return v, perr(fmt.Sprintf("%q is not a date literal", raw), "dates are written `YYYY-MM-DD`")
		}
		v.Date = d

	default:
		return v, perr("unsupported type in a touchstone cell", "")
	}
	return v, nil
}

func checkWidth(n *big.Int, t Type) *ParseError {
	var bits int
	switch t.Kind {
	case I32:
		bits = 32
	case I64:
		bits = 64
	default:
		return nil
	}
	lo := new(big.Int).Lsh(big.NewInt(-1), uint(bits-1))
	hi := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(bits-1)), big.NewInt(1))
	if n.Cmp(lo) < 0 || n.Cmp(hi) > 0 {
		return perr(fmt.Sprintf("%s does not fit in %s", n, t),
			fmt.Sprintf("%s holds %s to %s; use `int` for arbitrary precision", t, lo, hi))
	}
	return nil
}

// parseScaled converts a decimal literal to an integer scaled by scale.
// It rejects excess fractional digits rather than rounding, because silently
// rounding a spec's own literal would make the touchstone lie.
func parseScaled(raw string, scale int) (*big.Int, *ParseError) {
	neg := strings.HasPrefix(raw, "-")
	body := strings.TrimPrefix(strings.TrimPrefix(raw, "-"), "+")

	intPart, fracPart, hasDot := strings.Cut(body, ".")
	if intPart == "" && !hasDot {
		return nil, perr(fmt.Sprintf("%q is not a decimal literal", raw), "decimal literals look like `49.99` or `-100`")
	}
	if !allDigits(intPart) || (hasDot && !allDigits(fracPart)) {
		return nil, perr(fmt.Sprintf("%q is not a decimal literal", raw), "decimal literals look like `49.99` or `-100`")
	}
	if len(fracPart) > scale {
		return nil, perr(
			fmt.Sprintf("%s has %d decimal places, but the type allows %d", raw, len(fracPart), scale),
			"Doré will not round a literal you wrote; use a value the type can hold exactly")
	}
	for len(fracPart) < scale {
		fracPart += "0"
	}
	n, ok := new(big.Int).SetString(intPart+fracPart, 10)
	if !ok {
		return nil, perr(fmt.Sprintf("%q is not a decimal literal", raw), "")
	}
	if neg {
		n.Neg(n)
	}
	return n, nil
}

func allDigits(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// splitTolerance separates `0.333 ~1e-4` into its literal and tolerance.
func splitTolerance(raw string) (lit string, tol float64, has bool, err *ParseError) {
	before, after, found := strings.Cut(raw, "~")
	if !found {
		return strings.TrimSpace(raw), 0, false, nil
	}
	t, ferr := strconv.ParseFloat(strings.TrimSpace(after), 64)
	if ferr != nil || t <= 0 {
		return "", 0, false, perr(fmt.Sprintf("%q is not a valid tolerance", strings.TrimSpace(after)),
			"a tolerance is a positive float, like `~1e-9`")
	}
	return strings.TrimSpace(before), t, true, nil
}
