package main

import (
	"bufio"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"

	"github.com/jasonm4130/wattop/internal/fixture"
)

// ~/.claude and ~/.codex also hold session lock keys, credential caches
// (auth.json) and settings that must never be walked, redacted or copied, so
// only the shapes the fixture corpus is made of are allowed.
var (
	transcriptFile = regexp.MustCompile(`\.jsonl$`)
	subagentMeta   = regexp.MustCompile(`\.meta\.json$`)
	sessionFile    = regexp.MustCompile(`^[0-9]+\.json$`)
	mactopSnapshot = regexp.MustCompile(`^mactop-[\w-]+\.json$`)
)

func allowedFile(path string) bool {
	name := filepath.Base(path)
	switch {
	case transcriptFile.MatchString(name), subagentMeta.MatchString(name), mactopSnapshot.MatchString(name):
		return true
	case sessionFile.MatchString(name):
		return filepath.Base(filepath.Dir(path)) == "sessions"
	}
	return false
}

func main() {
	src := flag.String("src", "", "source file or directory to scrub")
	dst := flag.String("dst", "", "destination file or directory to write scrubbed output to")
	flag.Parse()

	if *src == "" || *dst == "" {
		fmt.Fprintln(os.Stderr, "usage: wattop-scrub -src <path> -dst <path>")
		os.Exit(2)
	}

	info, err := os.Stat(*src)
	if err != nil {
		log.Fatalf("wattop-scrub: %v", err)
	}

	if !info.IsDir() {
		if !allowedFile(*src) {
			log.Fatalf("wattop-scrub: %s is not a transcript, subagent meta, session or mactop file", *src)
		}
		if err := scrubFile(*src, *dst); err != nil {
			log.Fatalf("wattop-scrub: %v", err)
		}
		return
	}

	err = filepath.WalkDir(*src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !allowedFile(path) {
			return nil
		}

		rel, err := filepath.Rel(*src, path)
		if err != nil {
			return err
		}
		return scrubFile(path, filepath.Join(*dst, rel))
	})
	if err != nil {
		log.Fatalf("wattop-scrub: %v", err)
	}
}

func scrubFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	if filepath.Ext(src) == ".json" {
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		scrubbed, err := fixture.Scrub(data)
		if err != nil {
			log.Printf("wattop-scrub: %s: %v (copied through unscrubbed)", src, err)
			scrubbed = data
		}
		return os.WriteFile(dst, scrubbed, 0o644)
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	w := bufio.NewWriter(out)
	if err := fixture.ScrubStream(in, w, src); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	return out.Close()
}
