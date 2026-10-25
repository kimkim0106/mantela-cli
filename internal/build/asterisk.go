package build

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/kimkim0106/mantela-cli/internal/station"
)

// 生成するコンテキストの名前
const (
	ctxInternal = "mantela-internal"
	ctxOutbound = "mantela-outbound"
	ctxPhones   = "mantela-phones"
)

func ctxFrom(slug string) string { return "mantela-from-" + slug }

// generatesTrunk は、provider の pjsip 設定と発信ルートを作るかを返す。
func generatesTrunk(p *station.Provider) bool {
	return p.Enabled && p.Trunk != nil
}

// skipReason は、provider の pjsip 設定と発信ルートを作らない理由を、生成物のコメント用に返す。
func skipReason(p *station.Provider) string {
	if !p.Enabled {
		return "enabled = false なので"
	}
	return "[trunk] が無いので"
}

// kv は道具が生成するキーと値。
type kv struct{ key, value string }

// section は pjsip.conf の1セクションを作る。
//
// キーは「type → 道具が生成する骨組みのキー → 道具が補う context → 利用者のキー（書いた順）」の順に出す。
// 利用者が [pjsip.*] に type や骨組みのキーを書いていたらエラーにする。
// defaultContext は、利用者が context を書かなかったときに補う値（空なら補わない）。
func section(file, name, template, typ string, generated []kv, defaultContext string, user station.PjsipSection) (string, error) {
	var b strings.Builder
	b.WriteString("[" + name + "]")
	if template != "" {
		b.WriteString("(" + template + ")")
	}
	b.WriteString("\n")

	reserved := map[string]bool{"type": true}
	writeKV(&b, "type", typ)
	for _, g := range generated {
		reserved[g.key] = true
		writeKV(&b, g.key, g.value)
	}
	if defaultContext != "" && !user.Has("context") {
		writeKV(&b, "context", defaultContext)
	}

	var errs []error
	for _, k := range user {
		if reserved[k.Name] {
			errs = append(errs, fmt.Errorf("%s: [pjsip.%s] %s は mantela-cli が生成するキーなので書けません", file, typ, k.Name))
			continue
		}
		for _, v := range k.Values {
			writeKV(&b, k.Name, v)
		}
	}
	return b.String(), errors.Join(errs...)
}

func writeKV(b *strings.Builder, key, value string) {
	b.WriteString(key + " = " + value + "\n")
}

// PjsipConf は、生成する pjsip.conf の本文（生成物の印より後ろ）を作る。
//
// 先頭で secrets ファイルを #include し、内線（内線番号の文字列順）、トランク（slug の文字列順）の順に並べる。
// 1つの中は auth → aor → endpoint → registration → identify の順。
func PjsipConf(st *station.Station) (string, error) {
	var blocks []string
	var errs []error
	add := func(block string, err error) {
		blocks = append(blocks, block)
		errs = append(errs, err)
	}

	blocks = append(blocks, `#include "`+st.Secrets+`"`)

	blocks = append(blocks, "; ==== 内線 ====")
	for _, e := range st.Extensions {
		n := e.Number
		add(section(e.File, n, "secret-"+n, "auth",
			[]kv{{"auth_type", "userpass"}, {"username", n}}, "", e.Pjsip["auth"]))
		add(section(e.File, n, "", "aor", nil, "", e.Pjsip["aor"]))
		add(section(e.File, n, "", "endpoint",
			[]kv{{"aors", n}, {"auth", n}}, ctxPhones, e.Pjsip["endpoint"]))
	}

	header := []string{"; ==== トランク ===="}
	for _, p := range st.Providers {
		if !generatesTrunk(p) {
			header = append(header, fmt.Sprintf("; %s は %s作らない", p.Slug, skipReason(p)))
		}
	}
	blocks = append(blocks, strings.Join(header, "\n"))
	for _, p := range st.Providers {
		if !generatesTrunk(p) {
			continue
		}
		s, t := p.Slug, p.Trunk
		uri := "sip:" + t.Host
		auth, err := section(p.File, s, "secret-trunk-"+s, "auth",
			[]kv{{"auth_type", "userpass"}, {"username", t.Username}}, "", p.Pjsip["auth"])
		add(fmt.Sprintf("; %s（mode = %q）\n", s, t.Mode)+auth, err)
		add(section(p.File, s, "", "aor", []kv{{"contact", uri}}, "", p.Pjsip["aor"]))
		add(section(p.File, s, "", "endpoint",
			[]kv{{"aors", s}, {"outbound_auth", s}}, ctxFrom(s), p.Pjsip["endpoint"]))
		add(section(p.File, s, "", "registration", []kv{
			{"outbound_auth", s},
			{"server_uri", uri},
			{"client_uri", "sip:" + t.Username + "@" + t.Host},
			{"contact_user", t.ContactUser},
		}, "", p.Pjsip["registration"]))
		add(section(p.File, s, "", "identify",
			[]kv{{"endpoint", s}, {"match", t.Host}}, "", p.Pjsip["identify"]))
	}

	return joinBlocks(blocks), errors.Join(errs...)
}

// ExtensionsConf は、生成する extensions.conf の本文（生成物の印より後ろ）を作る。
//
// 段階0では PBX 間ホップ（mantela-hop）は作らない。
func ExtensionsConf(st *station.Station) string {
	var blocks []string

	internal := []string{"[" + ctxInternal + "]"}
	for _, e := range st.Extensions {
		internal = append(internal,
			fmt.Sprintf("exten => %s,1,Dial(PJSIP/%s)", e.Number, e.Number),
			" same => n,Hangup()")
	}
	blocks = append(blocks, strings.Join(internal, "\n"))

	outbound := []string{"[" + ctxOutbound + "]"}
	for _, p := range st.Providers {
		if !generatesTrunk(p) {
			outbound = append(outbound, fmt.Sprintf("; %s は %s発信ルートを作らない", p.Slug, skipReason(p)))
		}
	}
	for _, p := range st.Providers {
		if !generatesTrunk(p) {
			continue
		}
		number := "${EXTEN}"
		if p.Trunk.StripPrefix {
			number = fmt.Sprintf("${EXTEN:%d}", utf8.RuneCountInString(p.Prefix))
		}
		outbound = append(outbound,
			fmt.Sprintf("exten => _%sX.,1,Dial(PJSIP/%s%s@%s)", p.Prefix, p.Trunk.Prepend, number, p.Slug),
			" same => n,Hangup()")
	}
	blocks = append(blocks, strings.Join(outbound, "\n"))

	blocks = append(blocks, strings.Join([]string{
		"[" + ctxPhones + "]",
		"include => " + ctxInternal,
		"include => " + ctxOutbound,
	}, "\n"))

	for _, p := range st.Providers {
		if !generatesTrunk(p) {
			continue
		}
		blocks = append(blocks, strings.Join([]string{
			"[" + ctxFrom(p.Slug) + "]",
			"include => " + ctxInternal,
		}, "\n"))
	}

	return joinBlocks(blocks)
}

// joinBlocks は、ブロックを空行で区切って本文にする。
// 本文は空行で始め（生成物の印との区切り）、改行1つで終える。
func joinBlocks(blocks []string) string {
	for i, b := range blocks {
		blocks[i] = strings.TrimSuffix(b, "\n")
	}
	return "\n" + strings.Join(blocks, "\n\n") + "\n"
}
