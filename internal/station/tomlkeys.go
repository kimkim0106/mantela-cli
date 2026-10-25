package station

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/kimkim0106/mantela-cli/internal/ordered"
)

// Section types allowed in [pjsip.*]
var knownSections = []string{"endpoint", "aor", "auth", "registration", "identify"}

// pjsipSections reads [pjsip.*] with keys in the order written.
// Section types not in allowed, and values other than strings, integers and booleans
// (or arrays of them), are errors.
func pjsipSections(md toml.MetaData, path string, raw map[string]map[string]any, allowed []string, what string) (map[string]PjsipSection, []error) {
	var errs []error
	sections := map[string]PjsipSection{}
	for _, kind := range sortedKeys(md, toml.Key{"pjsip"}, raw) {
		if !slices.Contains(knownSections, kind) {
			errs = append(errs, fmt.Errorf("%s: [pjsip.%s] is not a known section type (%s)", path, kind, strings.Join(knownSections, " / ")))
			continue
		}
		if !slices.Contains(allowed, kind) {
			errs = append(errs, fmt.Errorf("%s: [pjsip.%s] is not allowed for %s (%s)", path, kind, what, strings.Join(allowed, " / ")))
			continue
		}
		var sec PjsipSection
		for _, key := range sortedKeys(md, toml.Key{"pjsip", kind}, raw[kind]) {
			values, ok := pjsipValues(raw[kind][key])
			if !ok {
				errs = append(errs, fmt.Errorf("%s: [pjsip.%s] %s must be a string without newlines, an integer, a boolean, or an array of them", path, kind, key))
				continue
			}
			sec = append(sec, PjsipKey{Name: key, Values: values})
		}
		sections[kind] = sec
	}
	return sections, errs
}

// pjsipValues converts a [pjsip.*] value into strings for pjsip.conf, one per array element.
func pjsipValues(v any) ([]string, bool) {
	if arr, ok := v.([]any); ok {
		values := make([]string, 0, len(arr))
		for _, e := range arr {
			s, ok := pjsipScalar(e)
			if !ok {
				return nil, false
			}
			values = append(values, s)
		}
		return values, true
	}
	s, ok := pjsipScalar(v)
	if !ok {
		return nil, false
	}
	return []string{s}, true
}

func pjsipScalar(v any) (string, bool) {
	switch v := v.(type) {
	case string:
		if strings.ContainsAny(v, "\r\n") {
			return "", false
		}
		return v, true
	case int64:
		return strconv.FormatInt(v, 10), true
	case bool:
		return strconv.FormatBool(v), true
	}
	return "", false
}

// orderedValue converts tables in a value read from TOML into ordered.Map, with keys in the
// order written. path is the TOML key of v.
func orderedValue(md toml.MetaData, path toml.Key, v any) any {
	switch v := v.(type) {
	case map[string]any:
		m := ordered.New()
		for _, k := range sortedKeys(md, path, v) {
			m.Set(k, orderedValue(md, slices.Concat(path, toml.Key{k}), v[k]))
		}
		return m
	case []map[string]any: // array of tables ([[...]])
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = orderedValue(md, path, e)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = orderedValue(md, path, e)
		}
		return out
	}
	return v
}

// sortedKeys returns the keys of m in the order written in TOML.
//
// The order comes from MetaData.Keys(), which returns keys in the order they appear.
// Keys missing from Keys() (such as inline tables inside arrays) follow in name order.
func sortedKeys[V any](md toml.MetaData, path toml.Key, m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for _, k := range md.Keys() {
		if len(k) != len(path)+1 || !hasPrefix(k, path) {
			continue
		}
		name := k[len(path)]
		if _, ok := m[name]; ok && !slices.Contains(keys, name) {
			keys = append(keys, name)
		}
	}
	var rest []string
	for k := range m {
		if !slices.Contains(keys, k) {
			rest = append(rest, k)
		}
	}
	slices.Sort(rest)
	return append(keys, rest...)
}

func hasPrefix(k, prefix toml.Key) bool {
	return len(k) >= len(prefix) && slices.Equal(k[:len(prefix)], prefix)
}
