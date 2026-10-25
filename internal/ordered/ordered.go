// Package ordered は、キーを入れた順に保つ map を提供する。
// mantela.json の項目を決まった順で出力するために使う。
package ordered

import (
	"bytes"
	"encoding/json"
)

// Map は、キーを入れた順に保つ map。JSON にするときもその順で出力する。
type Map struct {
	keys   []string
	values map[string]any
}

// New は空の Map を作る。
func New() *Map {
	return &Map{values: map[string]any{}}
}

// Set はキーに値を入れる。新しいキーは末尾に足し、既にあるキーは位置を変えずに値だけ替える。
func (m *Map) Set(key string, value any) {
	if _, ok := m.values[key]; !ok {
		m.keys = append(m.keys, key)
	}
	m.values[key] = value
}

// Get はキーの値を返す。
func (m *Map) Get(key string) (any, bool) {
	v, ok := m.values[key]
	return v, ok
}

// Keys はキーを入れた順で返す。
func (m *Map) Keys() []string {
	return m.keys
}

// Len はキーの数を返す。
func (m *Map) Len() int {
	return len(m.keys)
}

// MarshalJSON はキーを入れた順で JSON のオブジェクトにする。
// HTML 向けのエスケープ（< を < にするなど）はしない。
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
	// Encode が末尾に足す改行を取る
	buf.Truncate(buf.Len() - 1)
	return nil
}
