package agents

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

// obj is an order-preserving JSON object whose values stay raw. Rewriting a
// config file therefore never reorders keys or touches unrelated values.
type kv struct {
	K string
	V json.RawMessage
}
type obj []kv

func parseObj(s string) (obj, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return obj{}, nil
	}
	dec := json.NewDecoder(strings.NewReader(s))
	t, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := t.(json.Delim); !ok || d != '{' {
		return nil, errors.New("JSON root is not an object")
	}
	o := obj{}
	for dec.More() {
		kt, err := dec.Token()
		if err != nil {
			return nil, err
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		o = append(o, kv{kt.(string), raw})
	}
	if _, err := dec.Token(); err != nil {
		return nil, err
	}
	return o, nil
}

func (o obj) idx(k string) int {
	for i, e := range o {
		if e.K == k {
			return i
		}
	}
	return -1
}

func (o *obj) set(k string, v json.RawMessage) {
	if i := o.idx(k); i >= 0 {
		(*o)[i].V = v
		return
	}
	*o = append(*o, kv{k, v})
}

func (o *obj) del(k string) {
	if i := o.idx(k); i >= 0 {
		*o = append((*o)[:i], (*o)[i+1:]...)
	}
}

func (o obj) keys() []string {
	ks := make([]string, 0, len(o))
	for _, e := range o {
		ks = append(ks, e.K)
	}
	return ks
}

// MarshalJSON lets obj nest inside other values passed to enc().
func (o obj) MarshalJSON() ([]byte, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, e := range o {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(enc(e.K))
		b.WriteByte(':')
		b.Write(e.V)
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

func (o obj) pretty() string {
	raw, _ := o.MarshalJSON()
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "", "  "); err != nil {
		return string(raw) + "\n"
	}
	return out.String() + "\n"
}

// enc marshals without HTML escaping (keeps & < > readable in URLs).
func enc(v any) json.RawMessage {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	_ = e.Encode(v)
	return json.RawMessage(bytes.TrimRight(b.Bytes(), "\n"))
}

// mk builds an ordered object from key, value, key, value...
func mk(pairs ...any) obj {
	o := obj{}
	for i := 0; i+1 < len(pairs); i += 2 {
		o = append(o, kv{pairs[i].(string), enc(pairs[i+1])})
	}
	return o
}

// stripJSONC removes // and /* */ comments and trailing commas outside strings.
// The bool reports whether any comment was found.
func stripJSONC(s string) (string, bool) {
	var out strings.Builder
	inStr, found := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr:
			out.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				out.WriteByte(s[i])
			} else if c == '"' {
				inStr = false
			}
		case c == '"':
			inStr = true
			out.WriteByte(c)
		case strings.HasPrefix(s[i:], "//"):
			found = true
			for i < len(s) && s[i] != '\n' {
				i++
			}
			i--
		case strings.HasPrefix(s[i:], "/*"):
			found = true
			j := strings.Index(s[i+2:], "*/")
			if j < 0 {
				i = len(s)
			} else {
				i += j + 3
			}
		default:
			out.WriteByte(c)
		}
	}
	return trailingComma.ReplaceAllString(out.String(), "$1"), found
}
