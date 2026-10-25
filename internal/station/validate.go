package station

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// Validate は、build の前に、重複と Mantela の必須項目を確かめる。
//
// 段階0では、これ以外の検査（type の列挙、identifier の文字種など）は行わない。
func (st *Station) Validate() error {
	var errs []error
	errs = append(errs, st.validateMantela()...)
	errs = append(errs, st.validateDuplicates()...)
	return errors.Join(errs...)
}

func (st *Station) validateMantela() []error {
	path := filepath.Join(st.Dir, "station.toml")
	var errs []error

	switch {
	case st.MantelaVersion == "":
		errs = append(errs, fmt.Errorf("%s: [mantela] version is missing", path))
	case !strings.HasPrefix(st.MantelaVersion, "0.1."):
		errs = append(errs, fmt.Errorf("%s: [mantela] version = %q is not supported (only 0.1.x)", path, st.MantelaVersion))
	}

	if st.AboutMe == nil {
		errs = append(errs, fmt.Errorf("%s: [mantela.aboutMe] is missing", path))
	} else {
		for _, key := range []string{"identifier", "name"} {
			if v, _ := st.AboutMe.Get(key); !nonEmptyString(v) {
				errs = append(errs, fmt.Errorf("%s: [mantela.aboutMe] %s is required (non-empty string)", path, key))
			}
		}
		if v, _ := st.AboutMe.Get("preferredPrefix"); !stringOrStrings(v) {
			errs = append(errs, fmt.Errorf("%s: [mantela.aboutMe] preferredPrefix is required (string or array of strings)", path))
		}
	}

	for _, e := range st.Extensions {
		if e.Publish && e.Name == "" {
			errs = append(errs, fmt.Errorf("%s: name is required for extensions published in Mantela (or set publish = false)", e.File))
		}
	}
	for _, p := range st.Providers {
		if p.Publish {
			for _, f := range []struct{ key, value string }{
				{"name", p.Name}, {"prefix", p.Prefix}, {"identifier", p.Identifier}, {"url", p.URL},
			} {
				if f.value == "" {
					errs = append(errs, fmt.Errorf("%s: %s is required for providers published in Mantela (or set publish = false)", p.File, f.key))
				}
			}
		} else if p.Enabled && p.Trunk != nil && p.Prefix == "" {
			// 載せない provider でも、発信ルートを作るには prefix が要る
			errs = append(errs, fmt.Errorf("%s: prefix is required to generate the outbound route", p.File))
		}
	}
	return errs
}

func (st *Station) validateDuplicates() []error {
	var errs []error
	dup := func(kind, value, file string, seen map[string]string) {
		if value == "" {
			return
		}
		if prev, ok := seen[value]; ok {
			errs = append(errs, fmt.Errorf("%s: %s %q is also used in %s", file, kind, value, prev))
			return
		}
		seen[value] = file
	}

	extIDs := map[string]string{}
	for _, e := range st.Extensions {
		dup("identifier", e.Identifier, e.File, extIDs)
	}
	provIDs := map[string]string{}
	prefixes := map[string]string{}
	for _, p := range st.Providers {
		dup("identifier", p.Identifier, p.File, provIDs)
		dup("prefix", p.Prefix, p.File, prefixes)
	}

	// pjsip.conf では内線番号と slug がどちらもセクション名になるので、同じ名前は使えない
	names := map[string]string{}
	for _, e := range st.Extensions {
		names[e.Number] = e.File
	}
	for _, p := range st.Providers {
		if p.Trunk == nil || !p.Enabled {
			continue
		}
		if prev, ok := names[p.Slug]; ok {
			errs = append(errs, fmt.Errorf("%s: slug %q is the same as an extension number (%s), so pjsip.conf section names would collide", p.File, p.Slug, prev))
		}
	}
	return errs
}

func nonEmptyString(v any) bool {
	s, ok := v.(string)
	return ok && s != ""
}

func stringOrStrings(v any) bool {
	switch v := v.(type) {
	case string:
		return v != ""
	case []any:
		for _, e := range v {
			if _, ok := e.(string); !ok {
				return false
			}
		}
		return true
	}
	return false
}
