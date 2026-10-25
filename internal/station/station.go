// Package station は局ディレクトリ（station.toml、extensions/、providers/）を読み込む。
//
// 段階0（MVP）では defaults.toml は読まない。内線は kind = "device"、
// トランクは mode = "register" だけに対応する。
package station

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/kimkim0106/mantela-cli/internal/ordered"
)

// FormatVersion は、この mantela-cli が読める局ディレクトリの形式の版。
const FormatVersion = 1

// DefaultSecrets は [asterisk] secrets の組み込みの既定値。
const DefaultSecrets = "/etc/asterisk/pjsip_secrets.conf"

// Station は読み込んだ局ディレクトリ全体。
type Station struct {
	Dir string

	// MantelaVersion は [mantela] version（出力する Mantela の版）。
	MantelaVersion string
	// AboutMe は [mantela.aboutMe] の中身。キーは書いた順に並ぶ。
	AboutMe *ordered.Map
	// Secrets は [asterisk] secrets（生成した pjsip.conf の先頭で #include するファイル）。
	Secrets string

	// Extensions は内線番号の文字列順に並ぶ。
	Extensions []*Extension
	// Providers は slug の文字列順に並ぶ。
	Providers []*Provider

	// Warnings は読み込み中に見つかった警告（未知のキーなど）。
	Warnings []string
}

// Extension は extensions/<番号>.toml の1つ分。
type Extension struct {
	Number string // ファイル名から取った内線番号
	File   string

	Name       string
	Type       string // 省略時は kind から決めた値（device なら phone）
	Identifier string // 空なら Mantela に出力しない
	Model      string
	Publish    bool

	Pjsip map[string]PjsipSection // キーは endpoint / aor / auth
}

// Provider は providers/<slug>.toml の1つ分。
type Provider struct {
	Slug string // ファイル名から取った識別名
	File string

	Name       string
	Prefix     string
	Identifier string
	URL        string // Mantela の mantela として出力する
	Enabled    bool
	Publish    bool

	Trunk *Trunk                  // [trunk] が無ければ nil（Mantela 掲載のみ）
	Pjsip map[string]PjsipSection // キーは endpoint / aor / auth / registration / identify
}

// Trunk は providers/<slug>.toml の [trunk]。
type Trunk struct {
	Mode        string // 段階0では "register" だけ
	Host        string
	StripPrefix bool
	Prepend     string
	Username    string // 省略時は slug
	ContactUser string // 省略時は slug
}

// PjsipSection は [pjsip.<種別>] の1つ分。キーは書いた順に並ぶ。
type PjsipSection []PjsipKey

// PjsipKey は [pjsip.*] の1つのキー。配列は Values が複数になり、同じキーを複数行で出力する。
type PjsipKey struct {
	Name   string
	Values []string
}

// Has は、セクションに name のキーがあるかを返す。
func (s PjsipSection) Has(name string) bool {
	return slices.ContainsFunc(s, func(k PjsipKey) bool { return k.Name == name })
}

type stationFile struct {
	MantelaCLI struct {
		Version *int64 `toml:"version"`
	} `toml:"mantela-cli"`
	Mantela struct {
		Version string         `toml:"version"`
		AboutMe map[string]any `toml:"aboutMe"`
	} `toml:"mantela"`
	Asterisk struct {
		Secrets *string `toml:"secrets"`
		Hop     *bool   `toml:"hop"` // 段階0では読むだけで使わない（PBX 間ホップは段階2）
	} `toml:"asterisk"`
}

type extensionFile struct {
	Name       string                    `toml:"name"`
	Kind       string                    `toml:"kind"`
	Type       string                    `toml:"type"`
	Identifier string                    `toml:"identifier"`
	Model      string                    `toml:"model"`
	Publish    *bool                     `toml:"publish"`
	Note       string                    `toml:"note"`
	Pjsip      map[string]map[string]any `toml:"pjsip"`
}

type providerFile struct {
	Name       string                    `toml:"name"`
	Prefix     string                    `toml:"prefix"`
	Identifier string                    `toml:"identifier"`
	URL        string                    `toml:"url"`
	Enabled    *bool                     `toml:"enabled"`
	Publish    *bool                     `toml:"publish"`
	Note       string                    `toml:"note"`
	Trunk      *trunkFile                `toml:"trunk"`
	Pjsip      map[string]map[string]any `toml:"pjsip"`
}

type trunkFile struct {
	Mode        string `toml:"mode"`
	Host        string `toml:"host"`
	StripPrefix *bool  `toml:"strip_prefix"`
	Prepend     string `toml:"prepend"`
	Username    string `toml:"username"`
	ContactUser string `toml:"contact_user"`
}

// 要素ごとに書ける [pjsip.*] の種別
var (
	extensionSections = []string{"auth", "aor", "endpoint"}
	registerSections  = []string{"auth", "aor", "endpoint", "registration", "identify"}
)

// Load は dir の局ディレクトリを読み込む。
//
// エラーがあっても、station.toml が読めていれば Station を返す（Warnings を表示できるように）。
func Load(dir string) (*Station, error) {
	st := &Station{Dir: dir}
	if err := st.loadStation(); err != nil {
		return st, err
	}
	var errs []error
	errs = append(errs, st.loadExtensions()...)
	errs = append(errs, st.loadProviders()...)
	return st, errors.Join(errs...)
}

func (st *Station) loadStation() error {
	path := filepath.Join(st.Dir, "station.toml")
	var f stationFile
	md, err := toml.DecodeFile(path, &f)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%s not found; specify the station directory with -C", path)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	st.warnUndecoded(path, md, toml.Key{"mantela", "aboutMe"})

	if !md.IsDefined("mantela-cli") {
		return fmt.Errorf("%s: [mantela-cli] is missing; this does not look like a station directory (check the directory given with -C)", path)
	}
	v := f.MantelaCLI.Version
	switch {
	case v == nil:
		return fmt.Errorf("%s: [mantela-cli] version is missing", path)
	case *v > FormatVersion:
		return fmt.Errorf("%s: station directory format version %d is not supported by this mantela-cli (up to %d); use a newer mantela-cli", path, *v, FormatVersion)
	case *v < 1:
		return fmt.Errorf("%s: [mantela-cli] version = %d is invalid (must be an integer >= 1)", path, *v)
	}

	st.MantelaVersion = f.Mantela.Version
	if f.Mantela.AboutMe != nil {
		st.AboutMe = orderedValue(md, toml.Key{"mantela", "aboutMe"}, f.Mantela.AboutMe).(*ordered.Map)
	}
	st.Secrets = DefaultSecrets
	if f.Asterisk.Secrets != nil {
		st.Secrets = *f.Asterisk.Secrets
	}
	return nil
}

func (st *Station) loadExtensions() []error {
	files, err := tomlFiles(filepath.Join(st.Dir, "extensions"))
	if err != nil {
		return []error{err}
	}
	var errs []error
	for _, file := range files {
		e, ferrs := st.loadExtension(file)
		errs = append(errs, ferrs...)
		if e != nil {
			st.Extensions = append(st.Extensions, e)
		}
	}
	return errs
}

func (st *Station) loadExtension(file tomlFile) (*Extension, []error) {
	var f extensionFile
	md, err := toml.DecodeFile(file.path, &f)
	if err != nil {
		return nil, []error{fmt.Errorf("%s: %w", file.path, err)}
	}
	st.warnUndecoded(file.path, md, toml.Key{"pjsip"})

	var errs []error
	switch f.Kind {
	case "", "device":
	case "group", "dialplan":
		errs = append(errs, fmt.Errorf("%s: kind = %q is not supported yet (this version supports only kind = \"device\")", file.path, f.Kind))
	default:
		errs = append(errs, fmt.Errorf("%s: kind = %q is invalid (device / group / dialplan)", file.path, f.Kind))
	}
	pjsip, perrs := pjsipSections(md, file.path, f.Pjsip, extensionSections, "extensions")
	errs = append(errs, perrs...)
	if len(errs) > 0 {
		return nil, errs
	}

	e := &Extension{
		Number:     file.name,
		File:       file.path,
		Name:       f.Name,
		Type:       f.Type,
		Identifier: f.Identifier,
		Model:      f.Model,
		Publish:    boolOr(f.Publish, true),
		Pjsip:      pjsip,
	}
	if e.Type == "" {
		e.Type = "phone" // kind = "device" の既定
	}
	return e, nil
}

func (st *Station) loadProviders() []error {
	files, err := tomlFiles(filepath.Join(st.Dir, "providers"))
	if err != nil {
		return []error{err}
	}
	var errs []error
	for _, file := range files {
		p, ferrs := st.loadProvider(file)
		errs = append(errs, ferrs...)
		if p != nil {
			st.Providers = append(st.Providers, p)
		}
	}
	return errs
}

func (st *Station) loadProvider(file tomlFile) (*Provider, []error) {
	var f providerFile
	md, err := toml.DecodeFile(file.path, &f)
	if err != nil {
		return nil, []error{fmt.Errorf("%s: %w", file.path, err)}
	}
	st.warnUndecoded(file.path, md, toml.Key{"pjsip"})

	p := &Provider{
		Slug:       file.name,
		File:       file.path,
		Name:       f.Name,
		Prefix:     f.Prefix,
		Identifier: f.Identifier,
		URL:        f.URL,
		Enabled:    boolOr(f.Enabled, true),
		Publish:    boolOr(f.Publish, true),
	}

	var errs []error
	if f.Trunk != nil {
		t := f.Trunk
		switch t.Mode {
		case "register":
			if t.Host == "" {
				errs = append(errs, fmt.Errorf("%s: [trunk] host is required for mode = \"register\"", file.path))
			}
		case "accept", "ip":
			errs = append(errs, fmt.Errorf("%s: [trunk] mode = %q is not supported yet (this version supports only mode = \"register\")", file.path, t.Mode))
		case "":
			errs = append(errs, fmt.Errorf("%s: [trunk] mode is missing", file.path))
		default:
			errs = append(errs, fmt.Errorf("%s: [trunk] mode = %q is invalid (register / accept / ip)", file.path, t.Mode))
		}
		p.Trunk = &Trunk{
			Mode:        t.Mode,
			Host:        t.Host,
			StripPrefix: boolOr(t.StripPrefix, true),
			Prepend:     t.Prepend,
			Username:    stringOr(t.Username, file.name),
			ContactUser: stringOr(t.ContactUser, file.name),
		}
	}
	// [trunk] が無ければ pjsip の設定は作らないので、[pjsip.*] も読まない
	if p.Trunk != nil {
		pjsip, perrs := pjsipSections(md, file.path, f.Pjsip, registerSections, "mode = \"register\" trunks")
		errs = append(errs, perrs...)
		p.Pjsip = pjsip
	} else if len(f.Pjsip) > 0 {
		st.warnf("%s: [pjsip.*] is ignored because there is no [trunk]", file.path)
	}
	if len(errs) > 0 {
		return nil, errs
	}
	return p, nil
}

func (st *Station) warnf(format string, args ...any) {
	st.Warnings = append(st.Warnings, fmt.Sprintf(format, args...))
}

// warnUndecoded は、どの項目にも当たらなかった未知のキーを警告する。
// テーブルごと未知なら、テーブルの名前だけを知らせる。
//
// passthrough には、中身を解釈せずにそのまま使うテーブルを渡す。BurntSushi/toml は、
// map[string]any に読んだテーブルの中のインラインテーブルのキーも Undecoded() に含めるので、
// その中のキーは見ない。
func (st *Station) warnUndecoded(path string, md toml.MetaData, passthrough toml.Key) {
	undecoded := md.Undecoded()
	for _, k := range undecoded {
		if hasPrefix(k, passthrough) {
			continue
		}
		if slices.ContainsFunc(undecoded, func(p toml.Key) bool { return len(p) < len(k) && hasPrefix(k, p) }) {
			continue
		}
		st.warnf("%s: unknown key %s is ignored", path, k)
	}
}

type tomlFile struct {
	name string // 拡張子を除いたファイル名（内線番号や slug）
	path string
}

// tomlFiles は dir にある *.toml を、拡張子を除いた名前の文字列順で返す。
// ディレクトリが無ければ空を返す。. で始まるファイル（エディタの一時ファイルなど）は読まない。
func tomlFiles(dir string) ([]tomlFile, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var files []tomlFile
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".toml") {
			continue
		}
		files = append(files, tomlFile{name: strings.TrimSuffix(name, ".toml"), path: filepath.Join(dir, name)})
	}
	slices.SortFunc(files, func(a, b tomlFile) int { return strings.Compare(a.name, b.name) })
	return files, nil
}

func boolOr(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func stringOr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
