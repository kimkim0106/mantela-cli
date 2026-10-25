// Package build generates mantela.json and Asterisk config from a loaded station directory.
package build

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"

	"github.com/kimkim0106/mantela-cli/internal/ordered"
	"github.com/kimkim0106/mantela-cli/internal/station"
)

// Output order of aboutMe fields (the order in the Mantela 0.1.x schema).
// Fields not in the schema follow in the order written.
var aboutMeOrder = []string{
	"name", "preferredPrefix", "identifier",
	"sipUsername", "sipPassword", "sipServer", "sipPort",
	"sipUri", "geolocationCoordinates", "unavailable", "image",
}

// SchemaURL returns the URL of the official schema for the Mantela version.
func SchemaURL(version string) string {
	if strings.HasPrefix(version, "0.1.") {
		return "https://tkytel.github.io/mantela/0.1.x/mantela.schema.json"
	}
	return ""
}

// MantelaJSON renders mantela.json.
//
// Fields of each element follow the order in the Mantela schema.
// extensions are ordered by extension number as a string and providers by slug
// (the order in which they were loaded).
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
		// In stage 0, unavailable is output only when enabled = false
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

// reorder sorts the keys of m by order and appends keys not in order in their original order.
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
