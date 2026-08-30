# Doré

**A specification language that compiles by assay.**

*Doré* — pronounced `/dɔːˈreɪ/`, "dor-AY" — is the bar of unrefined bullion that
comes off a mine floor. It is ninety-odd percent precious metal already: all the
value is there, none of it is certified. You write Doré, and the compiler refines
it into verified, frozen, ordinary code.

> **Status: design phase.** Nothing here is implemented yet. This document is the
> intended direction; where the code and this document disagree, this document is
> the plan and the code is behind.

---

## What Doré is

Doré is a source language in which you describe *what a function must do* — its
signature, its governing rules, and a table of known-correct cases — and the
compiler produces a plain implementation in a host language, verified against
that table before it is allowed to exist.

```
frozen fn refund_eligible(days_since_purchase: int, order_total: float) -> approved: bool
  rule: approve refunds within 30 days of purchase
  rule: orders over 500.00 require manager sign-off and are never auto-approved

  touchstone:
    | days_since_purchase | order_total | approved |
    | 5                   | 49.99       | true     |
    | 30                  | 49.99       | true     |
    | 31                  | 49.99       | false    |
    | 5                   | 900.00      | false    |
```

`dore assay refunds.dore` emits a readable Python (or TypeScript) function, checked
against every row, hallmarked with the hash of the spec that produced it. The model
that wrote it is not present at runtime. The generated code is yours: commit it,
read it, step through it in a debugger, hand-edit it if you must.

## Why

Large language models write plausible code. Plausible is not the same as correct,
and the gap between them is where every current tool leaves you standing.

The usual responses are to review harder, prompt better, or add tests afterward.
Doré takes a different position: **the specification and its acceptance criteria are
the same artifact, and code that has not passed them does not compile.** Not
"compiles with a warning." Does not compile.

That single rule is what turns "an LLM wrote some code" into "an implementation
exists that provably satisfies the cases I wrote down." It is also what makes this
a language rather than a prompt — there is a build, and it is either green or it
has failed.

## How it works

```
  .dore source
       │
       ├─ parse ───────────── grammar rejects specs that cannot be checked
       ├─ typecheck ───────── every touchstone cell must be a valid literal
       │
       ├─ cache lookup ─────► hit: emit the hallmarked artifact, byte for byte. done.
       │                       (no model call, no network, no variance)
       │  miss
       ▼
     generate ─────────────── constrained decoding against a fixed output schema
       │
       ├─ cupel ───────────── static gate: no imports outside the allowlist, no
       │                       eval/exec/open, no dunder access. Fails closed.
       │
       ├─ assay ───────────── run every touchstone row. Green or nothing.
       │                       └─ red: feed failures back, bounded retries, then fail
       ▼
    hallmark ──────────────── cache keyed on hash(spec + model + effort + prompt
                              version + compiler version) and emit
```

Determinism comes from four layers, in descending order of how much work they do:

1. **The cache.** Same spec, same model, same compiler → the same bytes, forever.
   Reproducibility lives here. Everything else governs what happens on a miss.
2. **The touchstone.** Generated code that fails a single row never reaches the
   cache. Behavior is pinned to the spec even when the model's internals wobble.
3. **The cupel.** A static gate on generated code, so generation can run unattended.
4. **Pinning** — model version, effort, prompt template. Necessary hygiene, weak
   alone; it is an *input* to the cache key rather than a mechanism of its own.

`dore build --frozen` fails on any cache miss. That is how CI stays reproducible and
how a model upgrade never silently rewrites your business logic during a deploy.

## The frozen / live gradient

Determinism and a model's usefulness are in tension. Every function you freeze is a
function that can no longer adapt to input you did not anticipate. Every function
you leave live buys that adaptability and pays for it in variance.

Doré makes the choice explicit and visible at the definition site:

- **`frozen fn`** — compiled once, verified, cached, frozen. No model at runtime.
  Requires a touchstone; a frozen function with no oracle is a compile error.
- **`live fn`** — a schema-constrained model call at runtime. For the genuinely
  fuzzy: classify this message, extract the intent, judge this tone. A touchstone
  here is an *evaluation*, reported as a pass rate, not a gate.

**A `frozen` function may not call a `live` one.** The dependency arrow points from
nondeterministic to deterministic and never back, or "frozen" is a lie and the
verification means nothing. Call the live function at the boundary and pass its
result in as an input.

## Design principles

**No oracle, no freeze.** The central rule. Everything else follows from it.

**The compiler's job is to refuse.** Codegen is the easy half. The value is in the
programs that do not make it through — and in a spec language, in interrogating the
spec until it is tight enough to generate from at all. A compiler that accepts
whatever you typed is a worse deal than writing the function yourself.

**Generated code is a first-class artifact.** It is committed, diffed, and reviewed,
because unlike a transpiler's output it is not predictable from the input. Any design
that hides it is wrong.

**Compile to a host language.** No VM, no bytecode, no runtime of our own. Doré
inherits the host's debugger, package manager, profiler, and ecosystem, and its
output looks like code a person would have written.

**The escape hatch must exist and must be greppable.** Inline host code, and `live`.
Easy to reach for, trivial to audit, bannable in CI by teams that want it banned.

**Strictness is a ratchet, not a default.** Ship permissive. Hold-out rows, required
properties, and `--frozen` builds are opt-in and per-file.

## Non-goals

- General-purpose programming. Doré describes functions; the host language does the
  rest.
- Replacing human review of generated code.
- Maximal expressiveness. Every type must have an unambiguous literal form in a
  touchstone cell, and features that cannot be checked do not get in.
- Being a prompt framework. If the model is in the loop at runtime for most of your
  program, you want a different tool.

## What "verified" means here — and what it does not

A touchstone is an *incomplete* oracle. It checks specific inputs; it does not prove
a property over all of them. A model handed three rows can satisfy them with an
if-chain that hardcodes exactly those three, pass, and be wrong on the fourth.

This is the hardest problem in the design and most of the interesting work lives
here. The planned answers, in order of expected value:

- **Hold-out rows** — compile against a subset, verify against the whole table.
- **Adversarial case generation** — after a function goes green, a separate pass
  proposes boundary cases (exactly 30 days, exactly 500.00, negatives) for you to
  label. Confirmed answers become permanent rows, so the touchstone grows toward
  completeness instead of sitting at three rows forever.
- **Properties** — `property:` clauses checkable against generated inputs, covering
  regions no row touches.
- **Fineness** — a reported coverage measure, so a weak touchstone is visible rather
  than merely unlucky. If the generated code can be perturbed and still pass every
  row, the spec is under-determined and Doré should say so.

Doré gives you a machine-checked test suite that gates compilation. It does not give
you a proof. Anyone who tells you an LLM-backed compiler gives you a proof is selling
something.

## Vocabulary

The assay metaphor is used where it is more precise than the generic term, and
nowhere else. Tool verbs and artifacts are themed; language keywords stay plain.

| Term | Meaning |
|---|---|
| **doré** | A source file. Valuable, unrefined, uncertified. |
| **assay** | Compile and verify. Determine what something is actually made of by testing it. |
| **touchstone** | The table of known-correct cases. Historically, the stone gold is rubbed against to test its purity. |
| **hallmark** | The stamp on a verified artifact: spec hash, model, compiler version. |
| **cupel** | The static gate generated code passes through before it runs. |
| **fineness** | How completely the touchstone pins down the input space. |

## Roadmap

1. Parser, type checker, and diagnostics — **no model involved**. Error message
   quality is most of what people mean when they call a language finished.
2. The assay harness, exercised against hand-written implementations. Prove the
   verification machinery before adding generation.
3. Codegen with constrained decoding and the bounded repair loop.
4. Cache and `--frozen` builds.
5. `live fn` and runtime schema constraints.
6. Hold-out rows, properties, adversarial case generation, fineness reporting.
7. Language server: staleness, failing rows inline, jump-to-generated-code.

## License and contribution

Doré will be released under a permissive open-source license. A language nobody can
fork, audit, or keep alive without its author is a language nobody should build on.

Design decisions are made by the maintainers and are not open to contribution while
the language is pre-1.0 — design coherence is the scarcest resource an early language
has. Bug reports, failing touchstones, host-language backends, editor integrations,
and documentation are all welcome.

**Code that Doré generates belongs to you, unencumbered.** The compiler's license does
not reach its output.
