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

var update = flag.Bool("update", false, "rewrite the golden files with the current output")

// TestGolden builds testdata/<name>/station and compares the output with testdata/<name>/want.
//
// If want/stderr exists, stderr (warnings) is compared too.
func TestGolden(t *testing.T) {
	cases, err := filepath.Glob("testdata/*/station")
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) == 0 {
		t.Fatal("no golden tests in testdata")
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
				t.Fatalf("exit code %d\nstderr:\n%s", code, stderr.String())
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

// compareDirs checks that two directories have the same files with the same content.
func compareDirs(t *testing.T, want, got string) {
	t.Helper()
	wantFiles, gotFiles := listFiles(t, want), listFiles(t, got)
	if !slices.Equal(wantFiles, gotFiles) {
		t.Errorf("file lists differ\nwant: %v\ngot:  %v", wantFiles, gotFiles)
	}
	for _, name := range wantFiles {
		if !slices.Contains(gotFiles, name) {
			continue
		}
		w := readFile(t, filepath.Join(want, name))
		g := readFile(t, filepath.Join(got, name))
		if w != g {
			t.Errorf("%s differs\n--- want\n%s\n--- got\n%s", name, w, g)
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

// writeStation creates a station directory from files (path relative to the directory -> content).
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

// TestBuildErrors checks that build stops with an error and writes nothing.
func TestBuildErrors(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string // substring expected in stderr
	}{
		{
			name:  "no station.toml",
			files: map[string]string{"extensions/6001.toml": `name = "a"`},
			want:  "station.toml not found",
		},
		{
			name:  "no [mantela-cli]",
			files: map[string]string{"station.toml": `[mantela]` + "\n" + `version = "0.1.0"`},
			want:  "[mantela-cli] is missing",
		},
		{
			name:  "no [mantela-cli] version",
			files: map[string]string{"station.toml": "[mantela-cli]\n"},
			want:  "[mantela-cli] version is missing",
		},
		{
			name:  "newer format version",
			files: map[string]string{"station.toml": "[mantela-cli]\nversion = 2\n"},
			want:  "use a newer mantela-cli",
		},
		{
			name: "aboutMe missing a required field",
			files: map[string]string{"station.toml": `
[mantela-cli]
version = 1
[mantela]
version = "0.1.0"
[mantela.aboutMe]
name = "Example Station"
`},
			want: "[mantela.aboutMe] identifier is required",
		},
		{
			name: "Mantela version is not 0.1.x",
			files: map[string]string{
				"station.toml": strings.Replace(minimalStation, `version = "0.1.0"`, `version = "0.2.0"`, 1),
			},
			want: `[mantela] version = "0.2.0" is not supported`,
		},
		{
			name: "kind = group not supported yet",
			files: map[string]string{
				"station.toml":         minimalStation,
				"extensions/6000.toml": "name = \"全台\"\nkind = \"group\"\nmembers = [\"6001\"]\n",
			},
			want: `kind = "group" is not supported yet`,
		},
		{
			name: "mode = ip not supported yet",
			files: map[string]string{
				"station.toml":             minimalStation,
				"providers/station-c.toml": strings.Replace(minimalProvider, `"register"`, `"ip"`, 1),
			},
			want: `[trunk] mode = "ip" is not supported yet`,
		},
		{
			name: "register without host",
			files: map[string]string{
				"station.toml":             minimalStation,
				"providers/station-a.toml": strings.Replace(minimalProvider, `host = "192.0.2.10"`, "", 1),
			},
			want: `[trunk] host is required for mode = "register"`,
		},
		{
			name: "published extension without name",
			files: map[string]string{
				"station.toml":         minimalStation,
				"extensions/6001.toml": `type = "phone"`,
			},
			want: "name is required for extensions published in Mantela",
		},
		{
			name: "published provider without url",
			files: map[string]string{
				"station.toml":             minimalStation,
				"providers/station-a.toml": strings.Replace(minimalProvider, `url = "https://station-a.example.org/.well-known/mantela.json"`, "", 1),
			},
			want: "url is required for providers published in Mantela",
		},
		{
			name: "duplicate extension identifier",
			files: map[string]string{
				"station.toml":         minimalStation,
				"extensions/6001.toml": "name = \"a\"\nidentifier = \"A1B2C3D4\"\n",
				"extensions/6002.toml": "name = \"b\"\nidentifier = \"A1B2C3D4\"\n",
			},
			want: `identifier "A1B2C3D4" is also used in`,
		},
		{
			name: "duplicate provider prefix",
			files: map[string]string{
				"station.toml":             minimalStation,
				"providers/station-a.toml": minimalProvider,
				"providers/station-b.toml": strings.Replace(minimalProvider, "987fcdeb", "11111111", 1),
			},
			want: `prefix "300" is also used in`,
		},
		{
			name: "generated key set in [pjsip.*]",
			files: map[string]string{
				"station.toml":         minimalStation,
				"extensions/6001.toml": "name = \"a\"\n[pjsip.endpoint]\naors = \"x\"\n",
			},
			want: "[pjsip.endpoint] aors is generated by mantela-cli and must not be set",
		},
		{
			name: "[pjsip.*] type not allowed for extensions",
			files: map[string]string{
				"station.toml":         minimalStation,
				"extensions/6001.toml": "name = \"a\"\n[pjsip.registration]\nexpiration = 60\n",
			},
			want: "[pjsip.registration] is not allowed for extensions",
		},
		{
			name: "[pjsip.*] value type",
			files: map[string]string{
				"station.toml":         minimalStation,
				"extensions/6001.toml": "name = \"a\"\n[pjsip.aor]\nmax_contacts = 1.5\n",
			},
			want: "[pjsip.aor] max_contacts must be",
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
				t.Errorf("exit code %d, want %d", code, exitError)
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("stderr does not contain %q\nstderr:\n%s", tt.want, stderr.String())
			}
			if files := listFiles(t, out); len(files) > 0 {
				t.Errorf("files written despite the error: %v", files)
			}
		})
	}
}

// TestOverwriteProtection checks that files without the header and generated files edited
// by hand are not overwritten.
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

	t.Run("generated files can be overwritten", func(t *testing.T) {
		out := t.TempDir()
		for range 2 {
			if code, stderr := buildTo(out); code != exitOK {
				t.Fatalf("exit code %d\n%s", code, stderr)
			}
		}
	})

	t.Run("file without the header", func(t *testing.T) {
		out := t.TempDir()
		userConf := "[global]\ntype = global\n"
		writeFile(t, filepath.Join(out, "asterisk", "pjsip.conf"), userConf)
		code, stderr := buildTo(out)
		if code != exitError || !strings.Contains(stderr, "not generated by mantela-cli") {
			t.Fatalf("exit code %d\n%s", code, stderr)
		}
		if got := readFile(t, filepath.Join(out, "asterisk", "pjsip.conf")); got != userConf {
			t.Errorf("the user's file was rewritten:\n%s", got)
		}
		// If any file cannot be overwritten, no other output is written either
		if files := listFiles(t, out); !slices.Equal(files, []string{"asterisk/pjsip.conf"}) {
			t.Errorf("other outputs were written: %v", files)
		}
	})

	t.Run("generated file edited by hand", func(t *testing.T) {
		out := t.TempDir()
		if code, stderr := buildTo(out); code != exitOK {
			t.Fatalf("exit code %d\n%s", code, stderr)
		}
		path := filepath.Join(out, "asterisk", "extensions.conf")
		edited := readFile(t, path) + "exten => 9999,1,Hangup()\n"
		writeFile(t, path, edited)
		code, stderr := buildTo(out)
		if code != exitError || !strings.Contains(stderr, "edited by hand") {
			t.Fatalf("exit code %d\n%s", code, stderr)
		}
		if got := readFile(t, path); got != edited {
			t.Errorf("the edited file was rewritten")
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

// TestMark checks that the hash in the header is the sha256 of the body after the header,
// the same scheme as the hand-written expected files in samples/out/asterisk.
func TestMark(t *testing.T) {
	for _, path := range []string{"samples/out/asterisk/pjsip.conf", "samples/out/asterisk/extensions.conf"} {
		if err := build.CheckOverwrite(path); err != nil {
			t.Errorf("%s: %v", path, err)
		}
	}
	data := string(build.WithMark("\n[a]\n"))
	if !strings.HasPrefix(data, build.MarkLine+"\n; sha256:") {
		t.Errorf("unexpected header:\n%s", data)
	}
}

func TestUsage(t *testing.T) {
	tests := []struct {
		args []string
		code int
	}{
		{nil, exitUsage},
		{[]string{"unknown"}, exitUsage},
		{[]string{"build"}, exitUsage}, // no output specified
		{[]string{"build", "extra"}, exitUsage},
		{[]string{"-h"}, exitOK},
	}
	for _, tt := range tests {
		var stdout, stderr bytes.Buffer
		if code := run(tt.args, &stdout, &stderr); code != tt.code {
			t.Errorf("run(%q) = %d, want %d\n%s", tt.args, code, tt.code, stderr.String())
		}
	}
}
