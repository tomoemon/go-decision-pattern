// decision-pattern は Decision パターンの検証と、規約そのものの配布を行う。
//
// Decision パターンは判定を domain の純粋関数に閉じ込め、取得を呼び出し側の
// interpreter に任せる。その結果、実際にどの順でどのデータを取り、どの条件でどこへ
// 分岐してどの結末に着くのかが、domain と呼び出し側のどちらを読んでも一度には
// 見えない。状態遷移は型として書かれているので、そこから機械的に復元する。
//
// サブコマンド:
//
//	flow   状態遷移からフローチャート (Mermaid) を生成する
//	rule   規約の本文を書き出す
//
// 規約の本文はこのモジュールに埋め込んである。ツールの版と規約の版が go.mod で
// 一緒に固定され、どの版に従っているかがコミットに残る。
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tomoemon/go-decision-pattern/internal/flow"
	"github.com/tomoemon/go-decision-pattern/rule"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "decision-pattern:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return fmt.Errorf("サブコマンドを指定してください")
	}
	switch args[0] {
	case "flow":
		return runFlow(args[1:])
	case "rule":
		return runRule(args[1:])
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("不明なサブコマンド: %s", args[0])
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `使い方: decision-pattern <サブコマンド> [フラグ] [引数]

  flow   状態遷移からフローチャート (Mermaid) を生成する
  rule   規約の本文を書き出す

各サブコマンドの -h でフラグを表示します。
`)
}

func runFlow(args []string) error {
	fs := flag.NewFlagSet("flow", flag.ContinueOnError)
	outDir := fs.String("o", "", "出力先ディレクトリ。省略時は標準出力にまとめて書く")
	dir := fs.String("C", ".", "解析を実行する起点ディレクトリ")
	strict := fs.Bool("strict", false, "警告が 1 件でもあれば終了コード 1 にする")
	if err := fs.Parse(args); err != nil {
		return err
	}

	patterns := fs.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}

	decisions, err := flow.Analyze(*dir, patterns)
	if err != nil {
		return err
	}
	if len(decisions) == 0 {
		return fmt.Errorf("%s が付いた型が見つかりません", flow.Tag)
	}

	if *outDir == "" {
		flow.WriteStdout(os.Stdout, decisions)
	} else if err := flow.WriteFiles(*outDir, decisions, os.Stderr); err != nil {
		return err
	}

	if flow.ReportWarnings(os.Stderr, decisions) && *strict {
		return fmt.Errorf("警告があるため失敗として扱います (-strict)")
	}
	return nil
}

func runRule(args []string) error {
	fs := flag.NewFlagSet("rule", flag.ContinueOnError)
	out := fs.String("o", "", "出力先ファイル。省略時は標準出力")
	var paths pathList
	fs.Var(&paths, "path", "frontmatter の paths に入れる glob。複数指定できる")
	if err := fs.Parse(args); err != nil {
		return err
	}

	body := rule.Render(paths)
	if *out == "" {
		fmt.Print(body)
		return nil
	}
	if err := os.WriteFile(*out, []byte(body), 0o644); err != nil {
		return fmt.Errorf("failed to write %s: %w", *out, err)
	}
	fmt.Fprintln(os.Stderr, "wrote", *out)
	return nil
}

// pathList は -path を繰り返し受け取る。
type pathList []string

func (p *pathList) String() string { return fmt.Sprint([]string(*p)) }

func (p *pathList) Set(v string) error {
	*p = append(*p, v)
	return nil
}
