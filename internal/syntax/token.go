package syntax

import (
	"fmt"
	"strings"

	"github.com/christopherwolf/dore/internal/source"
	"github.com/christopherwolf/dore/internal/types"
)

// Tokens exist only for the fragments that need them: function signatures and,
// later, property expressions. Prose and table cells are never tokenized.
type tokenKind int

const (
	tIdent tokenKind = iota
	tLParen
	tRParen
	tColon
	tComma
	tArrow
	tNumber
	tUnknown
)

type token struct {
	kind    tokenKind
	text    string
	runeOff int
	runeLen int
}

func (t token) span(l logicalLine) source.Span { return l.spanAt(t.runeOff, t.runeLen) }

func (t token) describe() string {
	switch t.kind {
	case tIdent:
		return fmt.Sprintf("`%s`", t.text)
	case tLParen:
		return "`(`"
	case tRParen:
		return "`)`"
	case tColon:
		return "`:`"
	case tComma:
		return "`,`"
	case tArrow:
		return "`->`"
	case tNumber:
		return fmt.Sprintf("number `%s`", t.text)
	}
	return fmt.Sprintf("`%s`", t.text)
}

func tokenize(l logicalLine) []token {
	runes := []rune(l.Text)
	var out []token
	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case r == ' ' || r == '\t':
			i++
		case r == '(':
			out = append(out, token{tLParen, "(", i, 1})
			i++
		case r == ')':
			out = append(out, token{tRParen, ")", i, 1})
			i++
		case r == ':':
			out = append(out, token{tColon, ":", i, 1})
			i++
		case r == ',':
			out = append(out, token{tComma, ",", i, 1})
			i++
		case r == '-' && i+1 < len(runes) && runes[i+1] == '>':
			out = append(out, token{tArrow, "->", i, 2})
			i += 2
		case isIdentStart(r):
			j := i
			for j < len(runes) && isIdentPart(runes[j]) {
				j++
			}
			out = append(out, token{tIdent, string(runes[i:j]), i, j - i})
			i = j
		case r >= '0' && r <= '9':
			j := i
			for j < len(runes) && (runes[j] >= '0' && runes[j] <= '9') {
				j++
			}
			out = append(out, token{tNumber, string(runes[i:j]), i, j - i})
			i = j
		default:
			out = append(out, token{tUnknown, string(r), i, 1})
			i++
		}
	}
	return out
}

func isIdentStart(r rune) bool {
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isIdentPart(r rune) bool { return isIdentStart(r) || (r >= '0' && r <= '9') }

// The parser refers to the type system through these thin aliases so its
// signature-parsing code stays readable.
type typesType = types.Type

func lookupType(name string) (types.Type, bool) { return types.Named(name) }
func suggestType(name string) string            { return types.Suggest(name) }
func typeNames() []string                       { return types.AllNames() }

var _ = strings.TrimSpace
