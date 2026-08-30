# Contributing to Doré

Thanks for looking. A note on scope before you spend time.

## What is open, and what is not

Doré is pre-1.0, and **design decisions are made by the maintainers**. Design
coherence is the scarcest resource an early language has, and a language that
accepts every good idea becomes a language with no point of view.

That means proposals to change the syntax, the type system, or the invariants
below will usually be declined, however reasonable — not because they are
wrong, but because the design is not open yet.

**Very welcome:**

- Bug reports, especially a `.dore` file that behaves wrongly
- Failing touchstones — a spec that should check and does not, or vice versa
- Host-language backends
- Editor integrations
- Documentation, error message wording, and diagnostics that read badly

## Invariants

These are load bearing. A change that weakens one will be declined regardless
of what else it does. `CLAUDE.md` explains each in full.

1. **The oracle is model-free.** Nothing between "code ran" and "pass or fail"
   may consult a model.
2. **The model never writes to the oracle.** No `--accept-all`, ever.
3. **No oracle, no freeze.** A frozen function without examples is an error.
4. **Doré owns its types.** Backends honor Doré semantics; they do not
   redefine them.
5. **The gate produces conformance; the cache produces determinism.** Neither
   substitutes for the other.

## How to work

**Test first.** Write the failing test, watch it fail for the right reason,
then make it pass. Tests written after the fact only confirm what the code
already does. Pull requests without a test that would have failed before the
change will be sent back.

**Diagnostics are a versioned artifact.** Every message has a golden snapshot
in `internal/check/testdata`. If you change one, update it deliberately:

    go test ./internal/check -update

and include the diff in your pull request so the wording gets reviewed.

**Error codes are stable.** Prose may be reworded; `E0101` may not be reused
for something else.

## Before you open a pull request

    make check     # gofmt, vet, and the full test suite

Coverage uses `make cover`, never a bare `go test -cover` — a cached run reports
roughly fifteen points low, because cached results do not re-emit coverage data
while their statements are still counted.

## Getting started

    go build -o dore ./cmd/dore
    ./dore check examples/refunds.dore
    ./dore assay examples/refunds.dore --impl examples/refunds.py

`dore assay` executes the implementation you point it at. It runs in a separate
process with a scrubbed environment and a timeout, but it does **not** yet block
network or filesystem access — treat it as no safer than running that file
yourself.
