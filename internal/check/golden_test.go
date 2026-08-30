package check_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/christopherwolf/dore/internal/check"
	"github.com/christopherwolf/dore/internal/diag"
	"github.com/christopherwolf/dore/internal/source"
	"github.com/christopherwolf/dore/internal/syntax"
)

var update = flag.Bool("update", false, "rewrite golden files")

// TestGolden snapshots the exact rendered output for every fixture.
//
// Error message quality is most of what people mean when they call a language
// finished, so messages are treated as a versioned artifact: any change to one
// shows up as a diff in review rather than degrading unnoticed.
func TestGolden(t *testing.T) {
	files, err := filepath.Glob("testdata/*.dore")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures found: %v", err)
	}

	for _, path := range files {
		t.Run(strings.TrimSuffix(filepath.Base(path), ".dore"), func(t *testing.T) {
			f, err := source.Load(path)
			if err != nil {
				t.Fatal(err)
			}
			file, bag := syntax.Parse(f)
			bag.Merge(check.File(file))

			var buf bytes.Buffer
			diag.Style{Color: false}.RenderAll(&buf, bag)
			if bag.Len() == 0 {
				buf.WriteString("ok: no diagnostics\n")
			}
			got := buf.String()

			golden := strings.TrimSuffix(path, ".dore") + ".golden"
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("missing golden file; run: go test ./internal/check -update\n%v", err)
			}
			if got != string(want) {
				t.Errorf("output changed.\n--- want ---\n%s\n--- got ---\n%s", want, got)
			}
		})
	}
}
