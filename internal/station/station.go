// Package station loads a station directory (station.toml, extensions/, providers/).
//
// Stage 0 (MVP) does not read defaults.toml, and supports only kind = "device"
// extensions and mode = "register" trunks.
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

// FormatVersion is the station directory format version this mantela-cli can read.
const FormatVersion = 1

// DefaultSecrets is the built-in default of [asterisk] secrets.
const DefaultSecrets = "/etc/asterisk/pjsip_secrets.conf"

// Station is a loaded station directory.
type Station struct {
	Dir string

	// MantelaVersion is [mantela] version (the Mantela version to output).
	MantelaVersion string
	// AboutMe is the content of [mantela.aboutMe], with keys in the order written.
	AboutMe *ordered.Map
	// Secrets is [asterisk] secrets (the file #included at the top of the generated pjsip.conf).
	Secrets string

	// Extensions are ordered by extension number as a string.
	Extensions []*Extension
	// Providers are ordered by slug as a string.
	Providers []*Provider

	// Warnings found while loading (unknown keys, etc.).
	Warnings []string
}

// Extension is one extensions/<number>.toml.
type Extension struct {
	Number string // extension number taken from the file name
	File   string

	Name       string
	Type       string // derived from kind when omitted (phone for device)
	Identifier string // not output to Mantela when empty
	Model      string
	Publish    bool

	Pjsip map[string]PjsipSection // keyed by endpoint / aor / auth
}

// Provider is one providers/<slug>.toml.
type Provider struct {
	Slug string // identifier taken from the file name
	File string

	Name       string
	Prefix     string
	Identifier string
	URL        string // output as mantela in Mantela
	Enabled    bool
	Publish    bool

	Trunk *Trunk                  // nil without [trunk] (listed in Mantela only)
	Pjsip map[string]PjsipSection // keyed by endpoint / aor / auth / registration / identify
}

// Trunk is [trunk] in providers/<slug>.toml.
type Trunk struct {
	Mode        string // only "register" in stage 0
	Host        string
	StripPrefix bool
	Prepend     string
	Username    string // slug when omitted
	ContactUser string // slug when omitted
}

// PjsipSection is one [pjsip.<type>], with keys in the order written.
type PjsipSection []PjsipKey

// PjsipKey is one key in [pjsip.*]. An array gives multiple Values, written as repeated lines.
type PjsipKey struct {
	Name   string
	Values []string
}

// Has reports whether the section has the key name.
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
		Hop     *bool   `toml:"hop"` // read but unused in stage 0 (inter-PBX hop comes in stage 2)
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

// [pjsip.*] section types allowed for each element
var (
	extensionSections = []string{"auth", "aor", "endpoint"}
	registerSections  = []string{"auth", "aor", "endpoint", "registration", "identify"}
)

// Load loads the station directory dir.
//
// Even on error, it returns the Station once station.toml is read, so that Warnings can be shown.
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
		e.Type = "phone" // default for kind = "device"
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
	// Without [trunk] no pjsip config is generated, so [pjsip.*] is not read
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

// warnUndecoded warns about unknown keys that matched no field.
// If a whole table is unknown, only the table name is reported.
//
// passthrough is a table used as is without interpretation. Keys under it are skipped,
// because BurntSushi/toml also reports keys of inline tables inside a table decoded
// into map[string]any in Undecoded().
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
	name string // file name without the extension (extension number or slug)
	path string
}

// tomlFiles returns *.toml in dir, ordered by name without the extension.
// It returns nothing if dir does not exist, and skips files starting with . (editor temp files, etc.).
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
