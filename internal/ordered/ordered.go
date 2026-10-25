// Package ordered provides a map that keeps keys in insertion order.
// It is used to output mantela.json fields in a fixed order.
package ordered

import (
	"bytes"
	"encoding/json"
)

// Map is a map that keeps keys in insertion order, also when marshaled to JSON.
type Map struct {
	keys   []string
	values map[string]any
}

// New returns an empty Map.
func New() *Map {
	return &Map{values: map[string]any{}}
}

// Set sets the value for key. A new key is appended; an existing key keeps its position.
func (m *Map) Set(key string, value any) {
	if _, ok := m.values[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

// Get returns the value for key.
func (m *Map) Get(key string) (any, bool) {
	v, ok := m.values[key]
	return v, ok
}

// Keys returns the keys in insertion order.
func (m *Map) Keys() []string {
	return m.keys
}

// Len returns the number of keys.
func (m *Map) Len() int {
	return len(m.keys)
}

// MarshalJSON encodes m as a JSON object with keys in insertion order.
// It does not escape HTML characters such as <, > and &.
func (m *Map) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range m.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := encode(&buf, k); err != nil {
			return nil, err
		}
		buf.WriteByte(':')
		if err := encode(&buf, m.values[k]); err != nil {
			return nil, err
		}
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

func encode(buf *bytes.Buffer, v any) error {
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return err
	}
	// drop the trailing newline added by Encode
	buf.Truncate(buf.Len() - 1)
	return nil
}
