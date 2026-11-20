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

	sc := &scrubber{dec: dec, in: body}
	if err := sc.value(""); err != nil {
		return nil, fmt.Errorf("fixture: scrub: %w", err)
	}

	out := sc.out.Bytes()
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

type scrubber struct {
	dec *json.Decoder
	in  []byte
	out bytes.Buffer
}

func (sc *scrubber) token() (tok json.Token, raw []byte, err error) {
	start := sc.dec.InputOffset()
	tok, err = sc.dec.Token()
	if err != nil {
		return nil, nil, err
	}
	raw = sc.in[start:sc.dec.InputOffset()]
	if i := bytes.IndexByte(raw, '"'); i >= 0 {
		raw = raw[i:]
	}
	return tok, raw, nil
}

func (sc *scrubber) writeString(original string, raw []byte, scrubbed string) {
	if scrubbed == original {
		sc.out.Write(raw)
		return
	}
	writeJSONString(&sc.out, scrubbed)
}

func (sc *scrubber) value(key string) error {
	tok, raw, err := sc.token()
	if err != nil {
		return err
	}
	buf := &sc.out

	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			return sc.object()
		case '[':
			return sc.array(key)
		default:
			return fmt.Errorf("unexpected delimiter %q", t)
		}
	case string:
		sc.writeString(t, raw, scrubString(key, t))
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

func (sc *scrubber) object() error {
	sc.out.WriteByte('{')
	first := true
	for sc.dec.More() {
		if !first {
			sc.out.WriteByte(',')
		}
		first = false

		keyTok, raw, err := sc.token()
		if err != nil {
			return err
		}
		keyStr, ok := keyTok.(string)
		if !ok {
			return fmt.Errorf("fixture: scrub: expected object key, got %T", keyTok)
		}
		sc.writeString(keyStr, raw, redactPlain(keyStr))
		sc.out.WriteByte(':')

		if err := sc.value(keyStr); err != nil {
			return err
		}
	}
	if _, err := sc.dec.Token(); err != nil {
		return err
	}
	sc.out.WriteByte('}')
	return nil
}

func (sc *scrubber) array(key string) error {
	sc.out.WriteByte('[')
	first := true
	for sc.dec.More() {
		if !first {
			sc.out.WriteByte(',')
		}
		first = false
		if err := sc.value(key); err != nil {
			return err
		}
	}
	if _, err := sc.dec.Token(); err != nil {
		return err
	}
	sc.out.WriteByte(']')
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
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		panic(fmt.Sprintf("fixture: scrub: encode string: %v", err))
	}
	buf.Truncate(buf.Len() - len("\n"))
}
