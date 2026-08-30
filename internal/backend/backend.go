// Package backend defines what a Doré host-language target must provide.
//
// A backend is responsible for two things and no more: turning Doré values
// into that language's literals, and producing a harness that calls the
// implementation and reports what came back. It never decides whether a row
// passed. That judgement is Doré's, in package types, so one spec cannot mean
// two different things in two languages.
package backend

import (
	"fmt"

	"github.com/christopherwolf/dore/internal/ast"
)

// Backend targets one host language.
type Backend interface {
	// Name identifies the backend in reports and in the hallmark.
	Name() string

	// Command returns the program and arguments that run harnessPath.
	Command(harnessPath string) (string, []string)

	// Harness renders a self-contained program that imports implPath, calls
	// fn once per row, and writes one Result line per row to stdout.
	Harness(fn *ast.FnDecl, implPath string) (string, error)

	// Extension is the harness file's suffix, including the dot.
	Extension() string
}

// Outcome is what happened when the harness called the implementation.
type Outcome string

const (
	// Returned means the call produced a value; Repr holds it.
	Returned Outcome = "returned"
	// Raised means the call raised an error the spec may have expected.
	Raised Outcome = "raised"
	// Failed means the harness itself could not run the row — a missing
	// function, a bad signature, an unmarshallable value. Never a spec failure.
	Failed Outcome = "failed"
)

// Result is one row's outcome, decoded from the harness's output.
//
// Repr is a canonical string rather than a typed value: JSON numbers cannot
// carry an arbitrary-precision int or an exact decimal without loss, and the
// whole point of Doré's type system is that those survive the round trip.
type Result struct {
	Table   int     `json:"table"`
	Row     int     `json:"row"`
	Outcome Outcome `json:"outcome"`
	Repr    string  `json:"repr,omitempty"`
	Error   string  `json:"error,omitempty"`
	Message string  `json:"message,omitempty"`
}

// ErrUnsupported reports a type a backend cannot marshal.
type ErrUnsupported struct {
	Backend string
	Type    string
}

func (e *ErrUnsupported) Error() string {
	return fmt.Sprintf("the %s backend cannot marshal %s", e.Backend, e.Type)
}
