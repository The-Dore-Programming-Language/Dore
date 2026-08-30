# Doré — working notes

A specification language that compiles by assay. See `README.md` for the
language's intent, and the design revision at
https://claude.ai/code/artifact/d394d317-2dc6-4225-a751-0c7146c688de for the
decisions this implementation follows.

## Layout

    cmd/dore/          CLI. Only `check` exists so far — no model involved.
    internal/source/   Files, positions, spans.
    internal/diag/     Diagnostic type and renderer.
    internal/syntax/   Line scanner, tokenizer, parser.
    internal/ast/      Syntax tree.
    internal/types/    Doré's type system and literal parsing.
    internal/check/    Resolution and validation.

## Invariants

These are load-bearing. Breaking one breaks the language's central claim.

**The oracle is model-free.** Nothing between "generated code ran" and
"pass or fail" may involve a model. `intent:` is prose that informs generation
and is never checked; `examples:` holds typed literals and always is. The
split is syntactic so it cannot erode.

**The model never writes to the oracle.** Tables live in human-authored source.
When proposal generation lands, proposed rows go to a separate file the assay
never reads, and `dore review` requires per-row confirmation. There is no
`--accept-all`, ever — it would turn Doré into the self-grading agent loop it
exists to replace.

**No oracle, no freeze.** A `frozen fn` without examples is a compile error
(E0101), not a warning.

**Doré owns its types.** `int` means arbitrary precision everywhere. A backend
that cannot honor a type emits a guard; it does not redefine the type. Without
this, one spec compiles to two implementations that disagree outside the rows.

**Two determinisms, kept separate.** The gate produces conformance. The cache
produces determinism. Neither substitutes for the other, and "the tests make it
deterministic" is false.

## Conventions

Every phase returns `*diag.Bag`, never `error`. Diagnostic codes are stable
(E0101); prose may be reworded, codes may not.

Parsing and checking never stop at the first error. A bad type annotation
poisons the type and continues, so tables below it still get checked.

Every diagnostic has a golden snapshot in `internal/check/testdata`. Changing a
message shows up as a reviewed diff:

    go test ./internal/check -update

Measure coverage with `make cover`, never a bare `go test -cover`. Two flags
are load bearing: `-coverpkg=./...` credits the golden tests for the parser and
renderer they drive, and `-count=1` defeats the test cache. A cached run does
not re-emit coverage data but its statements are still counted, so it reports
roughly fifteen points low.

AST nodes are sealed with marker methods. Go will not check type-switch
exhaustiveness, so the discipline is manual — when adding a node, grep for
existing switches.
