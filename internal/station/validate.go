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
		errs = append(errs, fmt.Errorf("%s: [mantela] version がありません", path))
	case !strings.HasPrefix(st.MantelaVersion, "0.1."):
		errs = append(errs, fmt.Errorf("%s: [mantela] version = %q には対応していません（0.1.x だけ）", path, st.MantelaVersion))
	}

	if st.AboutMe == nil {
		errs = append(errs, fmt.Errorf("%s: [mantela.aboutMe] がありません", path))
	} else {
		for _, key := range []string{"identifier", "name"} {
			if v, _ := st.AboutMe.Get(key); !nonEmptyString(v) {
				errs = append(errs, fmt.Errorf("%s: [mantela.aboutMe] %s は必須です（空でない文字列）", path, key))
			}
		}
		if v, _ := st.AboutMe.Get("preferredPrefix"); !stringOrStrings(v) {
			errs = append(errs, fmt.Errorf("%s: [mantela.aboutMe] preferredPrefix は必須です（文字列か、文字列の配列）", path))
		}
	}

	for _, e := range st.Extensions {
		if e.Publish && e.Name == "" {
			errs = append(errs, fmt.Errorf("%s: Mantela に載せる内線には name が必要です（載せないなら publish = false）", e.File))
		}
	}
	for _, p := range st.Providers {
		if p.Publish {
			for _, f := range []struct{ key, value string }{
				{"name", p.Name}, {"prefix", p.Prefix}, {"identifier", p.Identifier}, {"url", p.URL},
			} {
				if f.value == "" {
					errs = append(errs, fmt.Errorf("%s: Mantela に載せる provider には %s が必要です（載せないなら publish = false）", p.File, f.key))
				}
			}
		} else if p.Enabled && p.Trunk != nil && p.Prefix == "" {
			// 載せない provider でも、発信ルートを作るには prefix が要る
			errs = append(errs, fmt.Errorf("%s: 発信ルートを作るには prefix が必要です", p.File))
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
			errs = append(errs, fmt.Errorf("%s: %s %q が %s と重複しています", file, kind, value, prev))
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
			errs = append(errs, fmt.Errorf("%s: slug %q が内線番号（%s）と同じなので、pjsip.conf のセクション名が重複します", p.File, p.Slug, prev))
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
