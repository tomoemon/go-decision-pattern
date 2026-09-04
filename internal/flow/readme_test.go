package flow

import (
	"os"
	"regexp"
	"testing"
)

var mermaidBlock = regexp.MustCompile("(?s)```mermaid\n.*?\n```")

// README に貼った図は手で書き写したものなので、ツールの出力とずれる。
// ずれた図は仕様の説明として読まれるので、コードの間違いより害が大きい。
//
// 貼り替えるときは README.md の該当ブロックを次の出力で置き換える。
//
//	go run ./cmd/decision-pattern flow -C internal/flow/testdata/publish ./...
//	go run ./cmd/decision-pattern flow -C internal/flow/testdata/publish -shape=state ./...
func TestReadmeFiguresMatchRenderers(t *testing.T) {
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatalf("README.md が読めない: %v", err)
	}
	got := mermaidBlock.FindAllString(string(raw), -1)
	d := analyzeDir(t, "./testdata/publish")

	// 載っている順に、既定 (decision)、次に state。
	shapes := []string{ShapeDecision, ShapeState}
	if len(got) != len(shapes) {
		t.Fatalf("README の mermaid ブロックが %d 個。%d 個のはず", len(got), len(shapes))
	}
	for i, shape := range shapes {
		// レンダラの出力は見出しも含むので、図の部分だけ取り出す。
		want := mermaidBlock.FindString(Renderers[shape](d))
		if got[i] != want {
			t.Errorf("%d 番目の図が %s の出力と一致しない\ngot:\n%s\n\nwant:\n%s", i, shape, got[i], want)
		}
	}
}
