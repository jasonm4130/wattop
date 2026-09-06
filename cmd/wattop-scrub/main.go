// Command wattop-scrub redacts a recorded fixture transcript: this
// machine's real home path, username, and human prose, replaced with
// deterministic placeholders via internal/fixture.Scrub, one JSON record at
// a time.
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

// allowedFile is the positive allowlist of file names wattop-scrub will
// ever read. Anything else under -src is skipped, never opened — this is
// the tool that gets pointed at directories under ~/.claude and ~/.codex,
// which also hold files (session lock keys, credential caches) that must
// never be walked, redacted, or copied anywhere.
var allowedFile = regexp.MustCompile(`\.(jsonl|json)$`)

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
		if !allowedFile.MatchString(filepath.Base(*src)) {
			log.Fatalf("wattop-scrub: %s does not match the .json/.jsonl allowlist", *src)
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
		if !allowedFile.MatchString(d.Name()) {
			return nil // not on the allowlist: never opened
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

// scrubFile redacts src and writes the result to dst.
//
// A ".json" file is a single JSON document, possibly formatted across
// several physical lines (mactop's "--count 1" array framing prints the
// opening "[", the object, and the closing "]" as separate lines, for
// instance) — so it is read and scrubbed whole.
//
// A ".jsonl" file is one JSON record per line, scrubbed line by line so a
// line that fails to scrub (most commonly a partial JSON object left by a
// mid-write tail) can be copied through unchanged with a warning instead of
// aborting the whole file: a recorder capturing a live transcript will hit
// exactly this case on its last line.
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
	defer w.Flush()

	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := append(scanner.Bytes(), '\n')

		scrubbed, err := fixture.Scrub(line)
		if err != nil {
			log.Printf("wattop-scrub: %s:%d: %v (copied through unscrubbed)", src, lineNum, err)
			scrubbed = line
		}
		if _, err := w.Write(scrubbed); err != nil {
			return err
		}
	}
	return scanner.Err()
}
