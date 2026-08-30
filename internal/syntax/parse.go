package syntax

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/christopherwolf/dore/internal/ast"
	"github.com/christopherwolf/dore/internal/diag"
	"github.com/christopherwolf/dore/internal/source"
)

// Parse builds an AST from a source file.
//
// Parsing never stops at the first error. A spec author fixing five typos
// should see five diagnostics, not run the compiler five times.
func Parse(f *source.File) (*ast.File, *diag.Bag) {
	p := &parser{lines: scanLines(f), file: f, bag: &diag.Bag{}}
	out := &ast.File{Name: f.Name}
	for p.pos < len(p.lines) {
		before := p.pos
		if d := p.parseDecl(out); !d {
			// Skip to the next line at column 0 so one bad declaration does
			// not cascade into every following one.
			p.recover()
		}
		if p.pos == before {
			p.pos++
		}
	}
	return out, p.bag
}

type parser struct {
	lines []logicalLine
	file  *source.File
	pos   int
	bag   *diag.Bag
}

func (p *parser) cur() logicalLine        { return p.lines[p.pos] }
func (p *parser) done() bool              { return p.pos >= len(p.lines) }
func (p *parser) errf(d *diag.Diagnostic) { p.bag.Add(d) }

func (p *parser) recover() {
	p.pos++
	for !p.done() && p.cur().Indent > 0 {
		p.pos++
	}
}

// parseDecl parses one top-level declaration. It reports whether the line was
// recognized as the start of one.
func (p *parser) parseDecl(out *ast.File) bool {
	line := p.cur()
	if line.Indent != 0 {
		p.errf(diag.New("E0001", "unexpected indentation",
			line.span(), "this line is indented but no declaration is open").
			WithHelp("declarations start at column 1; sections inside them are indented"))
		return false
	}

	mode, ok := declMode(line.Text)
	if !ok {
		p.errf(diag.New("E0002", "expected a declaration",
			line.span(), "not a `frozen fn` or `live fn`").
			WithHelp("a Doré file holds function declarations; each starts with `frozen fn` or `live fn`"))
		return false
	}

	fn := p.parseSignature(line, mode)
	p.pos++
	if fn == nil {
		return false
	}
	p.parseSections(fn)
	out.Decls = append(out.Decls, fn)
	return true
}

func declMode(text string) (ast.Mode, bool) {
	switch {
	case strings.HasPrefix(text, "frozen fn "), text == "frozen fn":
		return ast.Frozen, true
	case strings.HasPrefix(text, "live fn "), text == "live fn":
		return ast.Live, true
	}
	return 0, false
}

// parseSignature parses `frozen fn name(a: T, b: U) -> r: V`.
func (p *parser) parseSignature(line logicalLine, mode ast.Mode) *ast.FnDecl {
	toks := tokenize(line)
	// toks[0] = frozen|live, toks[1] = fn
	if len(toks) < 3 {
		p.errf(diag.New("E0003", "incomplete function signature",
			line.span(), "expected a function name after `fn`").
			WithHelp("signatures look like `frozen fn name(input: int) -> output: bool`"))
		return nil
	}

	i := 2
	if toks[i].kind != tIdent {
		p.errf(diag.New("E0004", "expected a function name",
			toks[i].span(line), fmt.Sprintf("found %s", toks[i].describe())).
			WithHelp("function names are identifiers, like `refund_eligible`"))
		return nil
	}
	fn := &ast.FnDecl{
		Mode:     mode,
		Name:     toks[i].text,
		NameSpan: toks[i].span(line),
		Span:     line.span(),
	}
	i++

	if i >= len(toks) || toks[i].kind != tLParen {
		p.errf(diag.New("E0005", "expected `(` after the function name",
			fn.NameSpan, "parameter list starts here").
			WithHelp("a function with no inputs is still written `name() -> out: bool`"))
		return nil
	}
	i++

	// Parameters.
	for i < len(toks) && toks[i].kind != tRParen {
		name := toks[i]
		if name.kind != tIdent {
			p.errf(diag.New("E0006", "expected a parameter name",
				name.span(line), fmt.Sprintf("found %s", name.describe())))
			return nil
		}
		i++
		if i >= len(toks) || toks[i].kind != tColon {
			p.errf(diag.New("E0007", "expected `:` after the parameter name",
				name.span(line), "every parameter needs a declared type").
				WithHelp(fmt.Sprintf("write `%s: int` — Doré has no inferred parameter types", name.text)))
			return nil
		}
		i++
		ty, tyName, tySpan, ok := p.parseType(toks, &i, line)
		if !ok {
			return nil
		}
		fn.Params = append(fn.Params, ast.Param{
			Name: name.text, Type: ty, TypeName: tyName,
			Span: name.span(line), TypeSpan: tySpan,
		})
		if i < len(toks) && toks[i].kind == tComma {
			i++
		}
	}

	if i >= len(toks) || toks[i].kind != tRParen {
		p.errf(diag.New("E0008", "unclosed parameter list",
			line.span(), "expected `)`"))
		return nil
	}
	i++

	if i >= len(toks) || toks[i].kind != tArrow {
		p.errf(diag.New("E0009", "expected `->` and a named result",
			line.span(), "a Doré function must name its output").
			WithHelp("naming the result is what lets a touchstone column refer to it, as in `-> approved: bool`"))
		return nil
	}
	i++

	if i >= len(toks) || toks[i].kind != tIdent {
		p.errf(diag.New("E0010", "expected a result name after `->`",
			line.span(), "results are named, not just typed").
			WithHelp("write `-> approved: bool`, then use `approved` as a touchstone column"))
		return nil
	}
	rname := toks[i]
	i++
	if i >= len(toks) || toks[i].kind != tColon {
		// `-> bool` is the mistake everyone arriving from another language
		// makes. Saying "expected `:` after the result name" treats `bool` as
		// a name and reads as nonsense, so recognize the shape and explain
		// what Doré actually wants.
		if _, isType := lookupType(rname.text); isType {
			p.errf(diag.New("E0011", "the result needs a name, not just a type",
				rname.span(line), fmt.Sprintf("`%s` is a type", rname.text)).
				WithHelp(fmt.Sprintf("write `-> result_name: %s` — touchstone columns refer to the result by name, so it has to have one", rname.text)))
			return nil
		}
		p.errf(diag.New("E0011", "expected `:` after the result name",
			rname.span(line), "the result needs a declared type").
			WithHelp(fmt.Sprintf("write `-> %s: bool`, naming the type the function answers with", rname.text)))
		return nil
	}
	i++
	rty, rtyName, rtySpan, ok := p.parseType(toks, &i, line)
	if !ok {
		return nil
	}
	fn.Result = ast.Result{
		Name: rname.text, Type: rty, TypeName: rtyName,
		Span: rname.span(line), TypeSpan: rtySpan,
	}

	if i < len(toks) {
		p.errf(diag.New("E0012", "unexpected trailing input in signature",
			toks[i].span(line), fmt.Sprintf("found %s after the result type", toks[i].describe())))
	}
	return fn
}

// parseType reads a type name, including the parenthesized form decimal(p,s).
func (p *parser) parseType(toks []token, i *int, line logicalLine) (ty typesType, name string, span source.Span, ok bool) {
	if *i >= len(toks) || toks[*i].kind != tIdent {
		var sp source.Span = line.span()
		if *i < len(toks) {
			sp = toks[*i].span(line)
		}
		p.errf(diag.New("E0013", "expected a type name", sp, "types are written after `:`").
			WithHelp("built-in types: " + strings.Join(typeNames(), ", ")))
		return ty, "", sp, false
	}
	start := toks[*i]
	name = start.text
	span = start.span(line)
	*i++

	// decimal(p,s) is the one type whose name carries arguments.
	if name == "decimal" && *i < len(toks) && toks[*i].kind == tLParen {
		depth, j := 0, *i
		for j < len(toks) {
			if toks[j].kind == tLParen {
				depth++
			} else if toks[j].kind == tRParen {
				depth--
				if depth == 0 {
					break
				}
			}
			j++
		}
		if j >= len(toks) {
			p.errf(diag.New("E0014", "unclosed type arguments", span, "expected `)`").
				WithHelp("write `decimal(10,2)` — precision then scale"))
			return ty, name, span, false
		}
		var parts []string
		for _, t := range toks[*i+1 : j] {
			if t.kind != tComma {
				parts = append(parts, t.text)
			}
		}
		name = "decimal(" + strings.Join(parts, ",") + ")"
		span = joinSpans(span, toks[j].span(line))
		*i = j + 1
	}

	resolved, found := lookupType(name)
	if !found {
		d := diag.New("E0015", fmt.Sprintf("unknown type `%s`", name), span, "not a Doré type")
		if s := suggestType(name); s != "" {
			d.WithHelp(fmt.Sprintf("did you mean `%s`? Doré owns its type semantics, so only built-ins are valid: %s",
				s, strings.Join(typeNames(), ", ")))
		} else {
			d.WithHelp("built-in types: " + strings.Join(typeNames(), ", "))
		}
		p.errf(d)
		// Poison the type and keep parsing. One bad annotation must not
		// suppress every error in the tables below it — the author should
		// see all of them in one run.
		return typesType{}, name, span, true
	}
	return resolved, name, span, true
}

// parseSections consumes every indented block belonging to fn.
func (p *parser) parseSections(fn *ast.FnDecl) {
	for !p.done() && p.cur().Indent > 0 {
		line := p.cur()
		head := line.Text

		switch {
		case head == "intent:" || strings.HasPrefix(head, "intent:"):
			fn.Sections = append(fn.Sections, p.parseIntent(line))
		case head == "examples:":
			p.pos++
			if t := p.parseTable(line, "", source.Span{}); t != nil {
				fn.Sections = append(fn.Sections, t)
			}
		case strings.HasPrefix(head, "scenario"):
			label, lspan, ok := p.parseScenarioHead(line)
			p.pos++
			if ok {
				if t := p.parseTable(line, label, lspan); t != nil {
					fn.Sections = append(fn.Sections, t)
				}
			}
		case strings.HasPrefix(head, "property"):
			if s := p.parseProperty(line); s != nil {
				fn.Sections = append(fn.Sections, s)
			}
		case strings.HasPrefix(head, "|"):
			p.errf(diag.New("E0016", "table row outside a table block",
				line.span(), "this row has no header").
				WithHelp("put rows under an `examples:` or `scenario \"...\":` block"))
			p.pos++
		default:
			kw, _, _ := strings.Cut(head, ":")
			d := diag.New("E0017", fmt.Sprintf("unknown section `%s`", strings.TrimSpace(kw)),
				line.span(), "not a section Doré recognizes")
			d.WithHelp("sections are `intent:`, `examples:`, `scenario \"...\":`, and `property name:`")
			p.errf(d)
			p.pos++
			p.skipIndented(line.Indent)
		}
	}
}

// parseIntent reads the prose block. Its lines are kept verbatim: they inform
// generation and are never checked, so they are never tokenized.
func (p *parser) parseIntent(head logicalLine) *ast.Intent {
	in := &ast.Intent{Span: head.span()}
	if rest := strings.TrimSpace(strings.TrimPrefix(head.Text, "intent:")); rest != "" {
		in.Lines = append(in.Lines, rest)
	}
	p.pos++
	for !p.done() && p.cur().Indent > head.Indent {
		in.Lines = append(in.Lines, strings.TrimSpace(p.cur().Text))
		p.pos++
	}
	if len(in.Lines) == 0 {
		p.errf(&diag.Diagnostic{
			Severity: diag.Warning,
			Code:     "W0001",
			Msg:      "empty intent block",
			Primary:  diag.Label{Span: head.span(), Msg: "no prose here"},
			Help:     "intent describes what the function is for; an empty one gives the generator nothing to work with",
		})
	}
	return in
}

func (p *parser) parseScenarioHead(line logicalLine) (string, source.Span, bool) {
	rest := strings.TrimSpace(strings.TrimPrefix(line.Text, "scenario"))
	rest = strings.TrimSuffix(rest, ":")
	rest = strings.TrimSpace(rest)
	if rest == "" {
		p.errf(diag.New("E0018", "scenario needs a name",
			line.span(), "expected a quoted name").
			WithHelp(`write scenario "high-value orders always escalate": — the name appears in failure output`))
		return "", source.Span{}, false
	}
	label, err := strconv.Unquote(rest)
	if err != nil {
		off := strings.Index(line.Text, rest)
		p.errf(diag.New("E0019", "scenario name must be quoted",
			line.spanAt(len([]rune(line.Text[:off])), len([]rune(rest))), "expected a double-quoted string").
			WithHelp(fmt.Sprintf("write %s", strconv.Quote(rest))))
		return "", source.Span{}, false
	}
	return label, line.span(), true
}

// parseTable reads a header row followed by data rows.
func (p *parser) parseTable(head logicalLine, label string, labelSpan source.Span) *ast.Table {
	t := &ast.Table{Label: label, LabelSpan: labelSpan, Span: head.span()}

	if p.done() || p.cur().Indent <= head.Indent || !strings.HasPrefix(p.cur().Text, "|") {
		p.errf(diag.New("E0020", "table block has no rows",
			head.span(), "expected a header row beneath this").
			WithHelp("a table starts with a header naming each column, then one row per case: | days | approved |"))
		return nil
	}

	hdr := p.cur()
	cells := splitRow(hdr)
	for _, c := range cells {
		t.Columns = append(t.Columns, ast.Column{Name: strings.TrimSpace(c.text), Span: c.span})
	}
	p.pos++

	for !p.done() && p.cur().Indent > head.Indent && strings.HasPrefix(p.cur().Text, "|") {
		line := p.cur()
		rc := splitRow(line)
		row := ast.Row{Span: line.span()}
		for _, c := range rc {
			row.Cells = append(row.Cells, ast.Cell{Raw: strings.TrimSpace(c.text), Span: c.span})
		}
		if len(row.Cells) != len(t.Columns) {
			p.errf(diag.New("E0021", "row does not match the header",
				line.span(), fmt.Sprintf("%d cell(s) here", len(row.Cells))).
				WithLabel(hdr.span(), fmt.Sprintf("header declares %d column(s)", len(t.Columns))).
				WithHelp("every row needs one cell per column, even when a value is repeated"))
		} else {
			t.Rows = append(t.Rows, row)
		}
		p.pos++
	}

	if len(t.Rows) == 0 {
		p.errf(diag.New("E0022", "table has a header but no rows",
			head.span(), "no cases here").
			WithHelp("a header alone gates nothing; add at least one row"))
		return nil
	}
	return t
}

func (p *parser) parseProperty(head logicalLine) *ast.Property {
	rest := strings.TrimSpace(strings.TrimPrefix(head.Text, "property"))
	name, body, _ := strings.Cut(rest, ":")
	name = strings.TrimSpace(name)

	prop := &ast.Property{Name: name, NameSpan: head.span(), Span: head.span()}
	if name == "" {
		p.errf(diag.New("E0023", "property needs a name",
			head.span(), "expected a name before `:`").
			WithHelp("write `property over_limit_never_approved:` — the name appears when the property fails"))
	}
	if b := strings.TrimSpace(body); b != "" {
		prop.Body = append(prop.Body, b)
	}
	p.pos++
	for !p.done() && p.cur().Indent > head.Indent {
		prop.Body = append(prop.Body, strings.TrimSpace(p.cur().Text))
		p.pos++
	}
	if len(prop.Body) == 0 {
		p.errf(diag.New("E0024", fmt.Sprintf("property `%s` has no body", name),
			head.span(), "nothing to check").
			WithHelp("write a condition, like `forall order_total > 500.00 -> approved is false`"))
	}
	return prop
}

func (p *parser) skipIndented(indent int) {
	for !p.done() && p.cur().Indent > indent {
		p.pos++
	}
}

type rowCell struct {
	text string
	span source.Span
}

// splitRow splits `| a | b |` into cells, preserving each cell's exact span so
// a diagnostic can underline one value rather than the whole row.
func splitRow(line logicalLine) []rowCell {
	runes := []rune(line.Text)
	var out []rowCell
	start := -1
	for i, r := range runes {
		if r != '|' {
			continue
		}
		if start >= 0 {
			raw := string(runes[start:i])
			lead := len(raw) - len(strings.TrimLeft(raw, " \t"))
			trimmed := strings.TrimSpace(raw)
			n := len([]rune(trimmed))
			if n == 0 {
				n, lead = 1, 0
			}
			out = append(out, rowCell{text: trimmed, span: line.spanAt(start+lead, n)})
		}
		start = i + 1
	}
	return out
}

func joinSpans(a, b source.Span) source.Span {
	if !a.Valid() {
		return b
	}
	if !b.Valid() || b.End.Offset < a.Start.Offset {
		return a
	}
	return source.Span{File: a.File, Start: a.Start, End: b.End}
}
