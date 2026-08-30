// Package types defines Doré's type system.
//
// Doré owns its semantics rather than borrowing the host language's. A spec
// that says `int` means arbitrary precision everywhere; a backend that cannot
// honor that is responsible for emitting the guard, not for redefining the
// type. Without this, one spec compiles to two implementations that disagree
// outside the rows the touchstone happens to cover.
package types

import (
	"fmt"
	"strings"
)

// Kind enumerates Doré's built-in types.
type Kind int

const (
	Invalid Kind = iota
	Int          // arbitrary precision integer
	I32          // fixed width; overflow traps, never wraps
	I64          // fixed width; overflow traps, never wraps
	Money        // exact decimal, scale 2, banker's rounding
	Decimal      // exact decimal, declared precision and scale
	Float        // IEEE-754 binary64
	Text         // Unicode scalars, NFC normalized
	Bool
	Date // proleptic Gregorian, no time zone
)

// Type is a Doré type. Decimal carries precision and scale; every other
// built-in is fully described by its Kind.
type Type struct {
	Kind      Kind
	Precision int // Decimal only
	Scale     int // Decimal and Money
}

func (t Type) String() string {
	switch t.Kind {
	case Int:
		return "int"
	case I32:
		return "i32"
	case I64:
		return "i64"
	case Money:
		return "money"
	case Decimal:
		return fmt.Sprintf("decimal(%d,%d)", t.Precision, t.Scale)
	case Float:
		return "float"
	case Text:
		return "text"
	case Bool:
		return "bool"
	case Date:
		return "date"
	}
	return "<invalid>"
}

// IsExact reports whether values of t compare exactly. Float does not, which
// is why it needs a tolerance in an expected-output cell.
func (t Type) IsExact() bool { return t.Kind != Float }

// Named resolves a type name written in source. ok is false for unknown names.
func Named(name string) (Type, bool) {
	switch name {
	case "int":
		return Type{Kind: Int}, true
	case "i32":
		return Type{Kind: I32}, true
	case "i64":
		return Type{Kind: I64}, true
	case "money":
		return Type{Kind: Money, Scale: 2}, true
	case "float":
		return Type{Kind: Float}, true
	case "text":
		return Type{Kind: Text}, true
	case "bool":
		return Type{Kind: Bool}, true
	case "date":
		return Type{Kind: Date}, true
	}
	if p, s, ok := parseDecimalName(name); ok {
		return Type{Kind: Decimal, Precision: p, Scale: s}, true
	}
	return Type{}, false
}

func parseDecimalName(name string) (p, s int, ok bool) {
	if !strings.HasPrefix(name, "decimal(") || !strings.HasSuffix(name, ")") {
		return 0, 0, false
	}
	inner := name[len("decimal(") : len(name)-1]
	parts := strings.Split(inner, ",")
	if len(parts) != 2 {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(parts[0]), "%d", &p); err != nil {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(parts[1]), "%d", &s); err != nil {
		return 0, 0, false
	}
	if p < 1 || s < 0 || s > p {
		return 0, 0, false
	}
	return p, s, true
}

// AllNames lists every spellable built-in, for "did you mean" suggestions and
// for the language server's completion list.
func AllNames() []string {
	return []string{"int", "i32", "i64", "money", "decimal(p,s)", "float", "text", "bool", "date"}
}

// Suggest returns the closest built-in name to what the user typed, or "" when
// nothing is close enough to be worth suggesting.
func Suggest(typed string) string {
	best, bestDist := "", 1<<30
	for _, cand := range []string{"int", "i32", "i64", "money", "decimal", "float", "text", "bool", "date"} {
		if d := editDistance(strings.ToLower(typed), cand); d < bestDist {
			best, bestDist = cand, d
		}
	}
	// Only suggest when the mistake is plausibly a typo. A prefix relation is
	// a strong enough signal to allow a wider distance: someone arriving from
	// another language writes `boolean` or `integer`, which are three edits
	// away but unambiguous about what was meant.
	low := strings.ToLower(typed)
	if best != "" && (strings.HasPrefix(low, best) || strings.HasPrefix(best, low)) && bestDist > 0 {
		return best
	}
	if bestDist > 0 && bestDist <= 2 && bestDist < len(typed) {
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
