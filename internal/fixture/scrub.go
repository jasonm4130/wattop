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
	"os"
	"regexp"
	"strings"
	"unicode/utf8"
)

var homeUserRe = regexp.MustCompile(`/(?:Users|home)/[^/"]+`)

var proseKeys = map[string]bool{
	"text":               true,
	"description":        true,
	"last_agent_message": true,
}

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
	if _, err := dec.Token(); err != nil {
		return err
	}
	buf.WriteByte(']')
	return nil
}

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

func redactPlain(s string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		s = strings.ReplaceAll(s, home, "/home/u")
	}
	s = homeUserRe.ReplaceAllString(s, "/home/u")
	s = strings.ReplaceAll(s, "jasonm4130@gmail.com", "user@example.com")
	s = strings.ReplaceAll(s, "jasonm4130", "user")
	return s
}

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
		panic(fmt.Sprintf("fixture: scrub: marshal string: %v", err))
	}
	buf.Write(enc)
}
