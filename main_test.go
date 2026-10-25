package main

import (
	"bytes"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/kimkim0106/mantela-cli/internal/build"
)

var update = flag.Bool("update", false, "golden テストの期待ファイルを、いまの出力で書き換える")

// TestGolden は、testdata/<名前>/station を build した出力を、testdata/<名前>/want と比べる。
//
// want/stderr があれば、stderr（警告）も比べる。
func TestGolden(t *testing.T) {
	cases, err := filepath.Glob("testdata/*/station")
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("testdata に golden テストがありません")
	}
	for _, stationDir := range cases {
		caseDir := filepath.Dir(stationDir)
		t.Run(filepath.Base(caseDir), func(t *testing.T) {
			out := t.TempDir()
			var stdout, stderr bytes.Buffer
			code := run([]string{"-C", stationDir, "build",
				"--mantela", filepath.Join(out, "mantela.json"),
				"--asterisk", filepath.Join(out, "asterisk"),
			}, &stdout, &stderr)
			if code != exitOK {
				t.Fatalf("終了コード %d\nstderr:\n%s", code, stderr.String())
			}
			if stderr.Len() > 0 {
				if err := os.WriteFile(filepath.Join(out, "stderr"), stderr.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
			}

			want := filepath.Join(caseDir, "want")
			if *update {
				if err := os.RemoveAll(want); err != nil {
					t.Fatal(err)
				}
				if err := os.CopyFS(want, os.DirFS(out)); err != nil {
					t.Fatal(err)
				}
				return
			}
			compareDirs(t, want, out)
		})
	}
}

// compareDirs は、2つのディレクトリにあるファイルの一覧と中身が同じかを確かめる。
func compareDirs(t *testing.T, want, got string) {
	t.Helper()
	wantFiles, gotFiles := listFiles(t, want), listFiles(t, got)
	if !slices.Equal(wantFiles, gotFiles) {
		t.Errorf("ファイルの一覧が違います\nwant: %v\ngot:  %v", wantFiles, gotFiles)
	}
	for _, name := range wantFiles {
		if !slices.Contains(gotFiles, name) {
			continue
		}
		w := readFile(t, filepath.Join(want, name))
		g := readFile(t, filepath.Join(got, name))
		if w != g {
			t.Errorf("%s が期待と違います\n--- want\n%s\n--- got\n%s", name, w, g)
		}
	}
}

func listFiles(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		files = append(files, filepath.ToSlash(rel))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// writeStation は、files（局ディレクトリからの相対パス → 中身）から局ディレクトリを作る。
func writeStation(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

const minimalStation = `
[mantela-cli]
version = 1

[mantela]
version = "0.1.0"

[mantela.aboutMe]
identifier = "123e4567-e89b-12d3-a456-426614174000"
name = "Example Station"
preferredPrefix = ["200"]
`

const minimalProvider = `
name = "Station A"
prefix = "300"
identifier = "987fcdeb-51a2-43d1-9c4e-5a6b7c8d9e0f"
url = "https://station-a.example.org/.well-known/mantela.json"

[trunk]
mode = "register"
host = "192.0.2.10"
`

// TestBuildErrors は、build がエラーで止まり、何も書かないことを確かめる。
func TestBuildErrors(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string // stderr に含まれるべき文字列
	}{
		{
			name:  "station.toml が無い",
			files: map[string]string{"extensions/6001.toml": `name = "a"`},
			want:  "station.toml がありません",
		},
		{
			name:  "[mantela-cli] が無い",
			files: map[string]string{"station.toml": `[mantela]` + "\n" + `version = "0.1.0"`},
			want:  "[mantela-cli] がありません",
		},
		{
			name:  "[mantela-cli] version が無い",
			files: map[string]string{"station.toml": "[mantela-cli]\n"},
			want:  "[mantela-cli] version がありません",
		},
		{
			name:  "形式の版が新しい",
			files: map[string]string{"station.toml": "[mantela-cli]\nversion = 2\n"},
			want:  "新しい mantela-cli を使ってください",
		},
		{
			name: "aboutMe の必須項目が無い",
			files: map[string]string{"station.toml": `
[mantela-cli]
version = 1
[mantela]
version = "0.1.0"
[mantela.aboutMe]
name = "Example Station"
`},
			want: "[mantela.aboutMe] identifier は必須です",
		},
		{
			name: "Mantela の版が 0.1.x でない",
			files: map[string]string{
				"station.toml": strings.Replace(minimalStation, `version = "0.1.0"`, `version = "0.2.0"`, 1),
			},
			want: `[mantela] version = "0.2.0" には対応していません`,
		},
		{
			name: "kind = group はまだ扱わない",
			files: map[string]string{
				"station.toml":         minimalStation,
				"extensions/6000.toml": "name = \"全台\"\nkind = \"group\"\nmembers = [\"6001\"]\n",
			},
			want: `kind = "group" にはまだ対応していません`,
		},
		{
			name: "mode = ip はまだ扱わない",
			files: map[string]string{
				"station.toml":             minimalStation,
				"providers/station-c.toml": strings.Replace(minimalProvider, `"register"`, `"ip"`, 1),
			},
			want: `[trunk] mode = "ip" にはまだ対応していません`,
		},
		{
			name: "register に host が無い",
			files: map[string]string{
				"station.toml":             minimalStation,
				"providers/station-a.toml": strings.Replace(minimalProvider, `host = "192.0.2.10"`, "", 1),
			},
			want: `mode = "register" には [trunk] host が必要です`,
		},
		{
			name: "公開する内線に name が無い",
			files: map[string]string{
				"station.toml":         minimalStation,
				"extensions/6001.toml": `type = "phone"`,
			},
			want: "Mantela に載せる内線には name が必要です",
		},
		{
			name: "公開する provider に url が無い",
			files: map[string]string{
				"station.toml":             minimalStation,
				"providers/station-a.toml": strings.Replace(minimalProvider, `url = "https://station-a.example.org/.well-known/mantela.json"`, "", 1),
			},
			want: "Mantela に載せる provider には url が必要です",
		},
		{
			name: "内線の identifier の重複",
			files: map[string]string{
				"station.toml":         minimalStation,
				"extensions/6001.toml": "name = \"a\"\nidentifier = \"A1B2C3D4\"\n",
				"extensions/6002.toml": "name = \"b\"\nidentifier = \"A1B2C3D4\"\n",
			},
			want: `identifier "A1B2C3D4" が`,
		},
		{
			name: "provider の prefix の重複",
			files: map[string]string{
				"station.toml":             minimalStation,
				"providers/station-a.toml": minimalProvider,
				"providers/station-b.toml": strings.Replace(minimalProvider, "987fcdeb", "11111111", 1),
			},
			want: `prefix "300" が`,
		},
		{
			name: "道具が生成するキーを [pjsip.*] に書いた",
			files: map[string]string{
				"station.toml":         minimalStation,
				"extensions/6001.toml": "name = \"a\"\n[pjsip.endpoint]\naors = \"x\"\n",
			},
			want: "[pjsip.endpoint] aors は mantela-cli が生成するキーなので書けません",
		},
		{
			name: "内線に書けない [pjsip.*] の種別",
			files: map[string]string{
				"station.toml":         minimalStation,
				"extensions/6001.toml": "name = \"a\"\n[pjsip.registration]\nexpiration = 60\n",
			},
			want: "[pjsip.registration] は内線には書けません",
		},
		{
			name: "[pjsip.*] の値の型",
			files: map[string]string{
				"station.toml":         minimalStation,
				"extensions/6001.toml": "name = \"a\"\n[pjsip.aor]\nmax_contacts = 1.5\n",
			},
			want: "[pjsip.aor] max_contacts の値は",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := writeStation(t, tt.files)
			out := t.TempDir()
			var stdout, stderr bytes.Buffer
			code := run([]string{"-C", dir, "build",
				"--mantela", filepath.Join(out, "mantela.json"),
				"--asterisk", filepath.Join(out, "asterisk"),
			}, &stdout, &stderr)
			if code != exitError {
				t.Errorf("終了コード %d、want %d", code, exitError)
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("stderr に %q がありません\nstderr:\n%s", tt.want, stderr.String())
			}
			if files := listFiles(t, out); len(files) > 0 {
				t.Errorf("エラーなのにファイルを書いています: %v", files)
			}
		})
	}
}

// TestOverwriteProtection は、印の無いファイルと手で編集された生成物を上書きしないことを確かめる。
func TestOverwriteProtection(t *testing.T) {
	dir := writeStation(t, map[string]string{
		"station.toml":         minimalStation,
		"extensions/6001.toml": `name = "a"`,
	})
	buildTo := func(out string) (int, string) {
		var stdout, stderr bytes.Buffer
		code := run([]string{"-C", dir, "build",
			"--mantela", filepath.Join(out, "mantela.json"),
			"--asterisk", filepath.Join(out, "asterisk"),
		}, &stdout, &stderr)
		return code, stderr.String()
	}

	t.Run("生成物は上書きできる", func(t *testing.T) {
		out := t.TempDir()
		for range 2 {
			if code, stderr := buildTo(out); code != exitOK {
				t.Fatalf("終了コード %d\n%s", code, stderr)
			}
		}
	})

	t.Run("印の無いファイル", func(t *testing.T) {
		out := t.TempDir()
		userConf := "[global]\ntype = global\n"
		writeFile(t, filepath.Join(out, "asterisk", "pjsip.conf"), userConf)
		code, stderr := buildTo(out)
		if code != exitError || !strings.Contains(stderr, "mantela-cli の生成物ではない") {
			t.Fatalf("終了コード %d\n%s", code, stderr)
		}
		if got := readFile(t, filepath.Join(out, "asterisk", "pjsip.conf")); got != userConf {
			t.Errorf("利用者のファイルが書き換わっています:\n%s", got)
		}
		// 1つでも上書きできなければ、ほかの生成物も書かない
		if files := listFiles(t, out); !slices.Equal(files, []string{"asterisk/pjsip.conf"}) {
			t.Errorf("ほかの生成物を書いています: %v", files)
		}
	})

	t.Run("手で編集された生成物", func(t *testing.T) {
		out := t.TempDir()
		if code, stderr := buildTo(out); code != exitOK {
			t.Fatalf("終了コード %d\n%s", code, stderr)
		}
		path := filepath.Join(out, "asterisk", "extensions.conf")
		edited := readFile(t, path) + "exten => 9999,1,Hangup()\n"
		writeFile(t, path, edited)
		code, stderr := buildTo(out)
		if code != exitError || !strings.Contains(stderr, "手で編集されている") {
			t.Fatalf("終了コード %d\n%s", code, stderr)
		}
		if got := readFile(t, path); got != edited {
			t.Errorf("編集されたファイルが書き換わっています")
		}
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestMark は、生成物の印のハッシュが、印の行より後ろの本文の sha256 であることを確かめる。
// samples/out/asterisk の手書きの期待値と同じ方式であることも確かめる。
func TestMark(t *testing.T) {
	for _, path := range []string{"samples/out/asterisk/pjsip.conf", "samples/out/asterisk/extensions.conf"} {
		if err := build.CheckOverwrite(path); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
	data := string(build.WithMark("\n[a]\n"))
	if !strings.HasPrefix(data, build.MarkLine+"\n; sha256:") {
		t.Errorf("印の形が違います:\n%s", data)
	}
}

func TestUsage(t *testing.T) {
	tests := []struct {
		args []string
		code int
	}{
		{nil, exitUsage},
		{[]string{"unknown"}, exitUsage},
		{[]string{"build"}, exitUsage}, // 出力先の指定が無い
		{[]string{"build", "extra"}, exitUsage},
		{[]string{"-h"}, exitOK},
	}
	for _, tt := range tests {
		var stdout, stderr bytes.Buffer
		if code := run(tt.args, &stdout, &stderr); code != tt.code {
			t.Errorf("run(%q) = %d、want %d\n%s", tt.args, code, tt.code, stderr.String())
		}
	}
}
