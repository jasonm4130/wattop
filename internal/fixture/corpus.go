package fixture

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

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
