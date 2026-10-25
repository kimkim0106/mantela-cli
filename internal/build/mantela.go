// Package build は、読み込んだ局ディレクトリから mantela.json と Asterisk 用の設定を作る。
package build

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"

	"github.com/kimkim0106/mantela-cli/internal/ordered"
	"github.com/kimkim0106/mantela-cli/internal/station"
)

// aboutMe の項目を出力する順（Mantela 0.1.x のスキーマに書かれた順）。
// スキーマに無い項目は、この後ろに書いた順で足す。
var aboutMeOrder = []string{
	"name", "preferredPrefix", "identifier",
	"sipUsername", "sipPassword", "sipServer", "sipPort",
	"sipUri", "geolocationCoordinates", "unavailable", "image",
}

// SchemaURL は、Mantela の版に対応する公式スキーマの URL を返す。
func SchemaURL(version string) string {
	if strings.HasPrefix(version, "0.1.") {
		return "https://tkytel.github.io/mantela/0.1.x/mantela.schema.json"
	}
	return ""
}

// MantelaJSON は mantela.json の中身を作る。
//
// 各要素の中は Mantela のスキーマに書かれた項目の順に出す。
// extensions は内線番号の文字列順、providers は slug の文字列順（読み込んだ順のまま）。
func MantelaJSON(st *station.Station) ([]byte, error) {
	top := ordered.New()
	top.Set("$schema", SchemaURL(st.MantelaVersion))
	top.Set("version", st.MantelaVersion)
	top.Set("aboutMe", reorder(st.AboutMe, aboutMeOrder))

	extensions := []any{}
	for _, e := range st.Extensions {
		if !e.Publish {
			continue
		}
		m := ordered.New()
		m.Set("name", e.Name)
		m.Set("extension", e.Number)
		if e.Identifier != "" {
			m.Set("identifier", e.Identifier)
		}
		if e.Model != "" {
			m.Set("model", e.Model)
		}
		m.Set("type", e.Type)
		extensions = append(extensions, m)
	}
	top.Set("extensions", extensions)

	providers := []any{}
	for _, p := range st.Providers {
		if !p.Publish {
			continue
		}
		m := ordered.New()
		m.Set("name", p.Name)
		m.Set("prefix", p.Prefix)
		m.Set("identifier", p.Identifier)
		m.Set("mantela", p.URL)
		// unavailable は、段階0では enabled = false のときだけ出力する
		if !p.Enabled {
			m.Set("unavailable", true)
		}
		providers = append(providers, m)
	}
	top.Set("providers", providers)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(top); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// reorder は、m のキーを order の順に並べ、order に無いキーを元の順で後ろに足す。
func reorder(m *ordered.Map, order []string) *ordered.Map {
	out := ordered.New()
	for _, k := range order {
		if v, ok := m.Get(k); ok {
			out.Set(k, v)
		}
	}
	for _, k := range m.Keys() {
		if !slices.Contains(order, k) {
			v, _ := m.Get(k)
			out.Set(k, v)
		}
	}
	return out
}
