package testutil

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/")

// Golden compares got with testdata/<name>.golden. Run `go test -update` to
// (re)write the file, then REVIEW THE DIFF before committing.
func Golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run `go test -update` to create it)", err)
	}
	if got != string(want) {
		t.Errorf("output differs from %s (rerun with -update if the change is intended)\n--- got ---\n%s\n--- want ---\n%s", path, got, want)
	}
}
