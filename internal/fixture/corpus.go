// Package fixture provides the redaction tool used to record wattop's test
// corpus, and the corpus-path helper every consumer of that corpus uses.
package fixture

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// CorpusDir returns the absolute path to the module's testdata directory
// (<module root>/testdata), regardless of the caller's package directory.
//
// go test sets the working directory to the package under test, so a
// relative literal like "testdata/soc/mactop-ndjson.jsonl" resolves
// differently from every package. CorpusDir walks up from this file's own
// location (via runtime.Caller, which is fixed at compile time and immune to
// the working directory) until it finds the directory containing go.mod,
// and returns "<that directory>/testdata".
//
// It panics if no go.mod is found, since every caller depends on this
// resolving correctly and a silent empty path would fail far from its cause.
func CorpusDir() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("fixture.CorpusDir: runtime.Caller failed to report this file's path")
	}

	dir := filepath.Dir(thisFile)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "testdata")
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			panic(fmt.Sprintf("fixture.CorpusDir: no go.mod found walking up from %s", thisFile))
		}
		dir = parent
	}
}
