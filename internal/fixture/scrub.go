package fixture

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"
	"unicode/utf8"
)

// proseKeys are JSON object keys whose string value is human prose and must
// be replaced with filler that preserves the value's UTF-8 byte length:
// Claude content-block text ({"type":"text","text":"..."}), Claude subagent
// descriptions, and Codex's last_agent_message / input_text / output_text
// bodies (which also live under a "text" key).
var proseKeys = map[string]bool{
	"text":               true,
	"description":        true,
	"last_agent_message": true,
}

// uuidKeys are JSON object keys carrying an identifier that cross-references
// another record (a Claude parentUuid pointing at another record's uuid, a
// toolUseId pointing at a subagent's meta file, and so on). Each is rewritten
// to a deterministic fixed-format UUID derived from its original value, so
// the same original string always rewrites to the same output and
// cross-record links survive redaction.
var uuidKeys = map[string]bool{
	"uuid":                  true,
	"parentUuid":            true,
	"leafUuid":              true,
	"sessionId":             true,
	"session_id":            true,
	"toolUseId":             true,
	"tool_use_id":           true,
	"hash":                  true,
	"bridgeSessionId":       true,
	"call_id":               true,
	"turn_id":               true,
	"id":                    true,
	"ownerAccountUuid":      true,
	"ownerOrganizationUuid": true,
}

// Scrub rewrites a single JSON record (one line of a .jsonl file, or the
// entire content of a standalone .json file) to remove this machine's real
// paths and usernames and to replace human prose with byte-length-preserving
// filler, while leaving every other field byte-for-byte identical to the
// input.
//
// Every field is preserved exactly except:
//   - any string containing "/Users/jasonmatthew" (rewritten to "/home/u"),
//     "jasonm4130@gmail.com", or "jasonm4130" (rewritten to a placeholder),
//     wherever it appears, in a value or in an object key;
//   - the keys in proseKeys, whose value is replaced with 'x' filler that
//     preserves the original value's UTF-8 byte length exactly, so
//     len(Scrub(line)) == len(line) on the raw line bytes whenever the only
//     redaction present is prose filler (encoding/json's escaping of ", \,
//     <, > and & on re-encode can change the count when the *original*
//     prose itself already contained one of those bytes — keep corpus prose
//     ASCII-safe and free of those characters to keep the invariant exact);
//   - the keys in uuidKeys, whose value is rewritten to a deterministic
//     fixed-format UUID derived from a hash of the original value.
//
// Numbers, booleans, null, and every other string are copied through
// unchanged (via json.Number for numbers, so exact original formatting
// survives) unless they contain one of the redacted substrings above.
func Scrub(line []byte) ([]byte, error) {
	trailingNL := bytes.HasSuffix(line, []byte("\n"))
	body := line
	if trailingNL {
		body = line[:len(line)-1]
	}

	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()

	var buf bytes.Buffer
	if err := scrubValue(dec, &buf, ""); err != nil {
		return nil, fmt.Errorf("fixture: scrub: %w", err)
	}

	out := buf.Bytes()
	if trailingNL {
		out = append(out, '\n')
	}
	return out, nil
}

// ScrubStream scrubs a JSON-lines stream: it reads r one record per line,
// applies Scrub to each, and writes the result to w. name labels the source
// in the warning it logs when a line cannot be scrubbed.
//
// Line terminators are carried through exactly as they were read. That is
// load-bearing rather than tidy: a .jsonl file recorded from a live
// transcript ends mid-write, so its final line is both partial JSON and
// unterminated, and that missing final newline is precisely what makes the
// fixture exercise a byte-offset tailer's buffering path instead of its
// parse-failure path. Splitting the stream with bufio.Scanner would strip
// the terminators before Scrub ever saw them and silently append a newline
// the source did not have, so this reads with bufio.Reader.ReadBytes and
// hands Scrub the raw line, terminator included or not.
//
// A line that fails to scrub — the mid-write partial record above being the
// common case — is copied through byte-for-byte with a warning, so one
// truncated tail never aborts a whole file.
func ScrubStream(r io.Reader, w io.Writer, name string) error {
	br := bufio.NewReader(r)
	lineNum := 0

	for {
		line, readErr := br.ReadBytes('\n')

		// ReadBytes returns the final unterminated chunk together with
		// io.EOF, so the line is processed before the error is checked.
		if len(line) > 0 {
			lineNum++
			scrubbed, err := Scrub(line)
			if err != nil {
				log.Printf("fixture: %s:%d: %v (copied through unscrubbed)", name, lineNum, err)
				scrubbed = line
			}
			if _, err := w.Write(scrubbed); err != nil {
				return err
			}
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

// scrubValue consumes exactly one JSON value from dec and writes its
// scrubbed form to buf. key is the object key this value was found under
// ("" for the document root or for array elements), and drives whether a
// string value is treated as prose, an identifier, or passed through.
func scrubValue(dec *json.Decoder, buf *bytes.Buffer, key string) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}

	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return scrubObject(dec, buf)
		case '[':
			return scrubArray(dec, buf, key)
		default:
			return fmt.Errorf("unexpected delimiter %q", t)
		}
	case string:
		writeJSONString(buf, scrubString(key, t))
		return nil
	case json.Number:
		buf.WriteString(t.String())
		return nil
	case bool:
		if t {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		return nil
	case nil:
		buf.WriteString("null")
		return nil
	default:
		return fmt.Errorf("fixture: scrub: unexpected token type %T", tok)
	}
}

func scrubObject(dec *json.Decoder, buf *bytes.Buffer) error {
	buf.WriteByte('{')
	first := true
	for dec.More() {
		if !first {
			buf.WriteByte(',')
		}
		first = false

		keyTok, err := dec.Token()
		if err != nil {
			return err
		}
		keyStr, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("fixture: scrub: expected object key, got %T", keyTok)
		}
		writeJSONString(buf, redactPlain(keyStr))
		buf.WriteByte(':')

		if err := scrubValue(dec, buf, keyStr); err != nil {
			return err
		}
	}
	// Consume the closing '}'.
	if _, err := dec.Token(); err != nil {
		return err
	}
	buf.WriteByte('}')
	return nil
}

func scrubArray(dec *json.Decoder, buf *bytes.Buffer, key string) error {
	buf.WriteByte('[')
	first := true
	for dec.More() {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		if err := scrubValue(dec, buf, key); err != nil {
			return err
		}
	}
	// Consume the closing ']'.
	if _, err := dec.Token(); err != nil {
		return err
	}
	buf.WriteByte(']')
	return nil
}

// scrubString returns the redacted content for a string found under the
// given object key; the caller is responsible for JSON-encoding it.
func scrubString(key, content string) string {
	switch {
	case uuidKeys[key]:
		return rewriteUUID(content)
	case proseKeys[key]:
		return fillerPreserveByteLen(content)
	default:
		return redactPlain(content)
	}
}

// redactPlain replaces this machine's home path and username wherever they
// appear in s. It is applied to every string and every object key that is
// not otherwise classified, so a leaked path in an unexpected field (an
// argv entry, a cwd, a stray log line) is still caught.
func redactPlain(s string) string {
	s = strings.ReplaceAll(s, "/Users/jasonmatthew", "/home/u")
	s = strings.ReplaceAll(s, "jasonm4130@gmail.com", "user@example.com")
	s = strings.ReplaceAll(s, "jasonm4130", "user")
	return s
}

// fillerPreserveByteLen replaces s with ASCII 'x' characters such that the
// filler's UTF-8 byte length equals s's UTF-8 byte length exactly — not its
// rune count. A multibyte rune (e.g. the 3-byte 日) becomes three 'x's, not
// one, which is what keeps a byte-offset tailer's offsets valid against the
// redacted file.
func fillerPreserveByteLen(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		n := utf8.RuneLen(r)
		if n < 1 {
			n = 1
		}
		for i := 0; i < n; i++ {
			b.WriteByte('x')
		}
	}
	return b.String()
}

// rewriteUUID deterministically maps s to a fixed-format (version-4-shaped)
// UUID string derived from a SHA-256 hash of s. The same input always
// produces the same output, so cross-record references (a parentUuid
// pointing at another record's uuid, a toolUseId matching a subagent's
// meta.json) still line up after independent Scrub calls on each record.
func rewriteUUID(s string) string {
	if s == "" {
		return s
	}
	sum := sha256.Sum256([]byte(s))
	b := make([]byte, 16)
	copy(b, sum[:16])
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func writeJSONString(buf *bytes.Buffer, s string) {
	enc, err := json.Marshal(s)
	if err != nil {
		// json.Marshal of a string only fails for invalid UTF-8, which
		// cannot occur here: s was itself decoded from valid JSON, or is
		// our own filler/uuid/redaction output, all valid UTF-8.
		panic(fmt.Sprintf("fixture: scrub: marshal string: %v", err))
	}
	buf.Write(enc)
}
