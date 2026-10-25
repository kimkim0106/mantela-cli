// mantela-cli は、局ディレクトリ（TOML）から、公開用の mantela.json と
// Asterisk（pjsip）の設定断片を生成する。
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/kimkim0106/mantela-cli/internal/build"
	"github.com/kimkim0106/mantela-cli/internal/station"
)

const usage = `使い方: mantela-cli [-C DIR] <コマンド> [引数]

コマンド:
  build [--mantela FILE] [--asterisk DIR]
        局ディレクトリから mantela.json と Asterisk 用の設定を生成する

オプション:
  -C DIR  局ディレクトリ（既定はカレントディレクトリ）
`

// 終了コード
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("mantela-cli", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, usage) }
	dir := fs.String("C", ".", "局ディレクトリ")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}

	if fs.NArg() == 0 {
		fs.Usage()
		return exitUsage
	}
	cmd, rest := fs.Arg(0), fs.Args()[1:]
	switch cmd {
	case "build":
		return runBuild(*dir, rest, stderr)
	case "help":
		fmt.Fprint(stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "mantela-cli: 知らないコマンドです: %s\n\n", cmd)
		fs.Usage()
		return exitUsage
	}
}

func runBuild(dir string, args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("build", flag.ContinueOnError)
	fs.SetOutput(stderr)
	mantelaPath := fs.String("mantela", "", "mantela.json の出力先 `FILE`")
	asteriskDir := fs.String("asterisk", "", "Asterisk 用の設定の出力先 `DIR`（mantela-cli 専用のディレクトリ）")
	fs.Usage = func() {
		fmt.Fprint(stderr, "使い方: mantela-cli [-C DIR] build [--mantela FILE] [--asterisk DIR]\n\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}
		return exitUsage
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "mantela-cli build: 余分な引数があります: %v\n", fs.Args())
		return exitUsage
	}
	if *mantelaPath == "" && *asteriskDir == "" {
		fmt.Fprint(stderr, "mantela-cli build: 出力先を --mantela か --asterisk（または両方）で指定してください\n")
		return exitUsage
	}

	if err := buildStation(dir, *mantelaPath, *asteriskDir, stderr); err != nil {
		fmt.Fprintf(stderr, "エラー:\n%s\n", indent(err))
		return exitError
	}
	return exitOK
}

// buildStation は局ディレクトリを読み、検査し、生成物を書き出す。
// 生成と上書きの確認をすべて済ませてから書くので、エラーのときは何も書かない。
func buildStation(dir, mantelaPath, asteriskDir string, stderr io.Writer) error {
	st, err := station.Load(dir)
	for _, w := range st.Warnings {
		fmt.Fprintf(stderr, "警告: %s\n", w)
	}
	if err != nil {
		return err
	}
	if err := st.Validate(); err != nil {
		return err
	}

	type output struct {
		path      string
		data      []byte
		protected bool // 印の無いファイルや手で編集された生成物を上書きしない
	}
	var outputs []output
	var errs []error

	if mantelaPath != "" {
		data, err := build.MantelaJSON(st)
		if err != nil {
			return err
		}
		outputs = append(outputs, output{path: mantelaPath, data: data})
	}
	if asteriskDir != "" {
		pjsip, err := build.PjsipConf(st)
		errs = append(errs, err)
		outputs = append(outputs,
			output{filepath.Join(asteriskDir, "pjsip.conf"), build.WithMark(pjsip), true},
			output{filepath.Join(asteriskDir, "extensions.conf"), build.WithMark(build.ExtensionsConf(st)), true},
		)
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}

	for _, o := range outputs {
		if o.protected {
			errs = append(errs, build.CheckOverwrite(o.path))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}

	for _, o := range outputs {
		if err := build.WriteFile(o.path, o.data); err != nil {
			return err
		}
	}
	return nil
}

// indent は、複数行のエラーを1行ずつ字下げする。
func indent(err error) string {
	return "  " + strings.ReplaceAll(err.Error(), "\n", "\n  ")
}
