package flow

import (
	"slices"
	"strings"
	"testing"
)

// 判断ノード形式では、辺のラベルが枝の名前 1 つになる。
// 状態遷移図のように経路上の条件が連なることはない。
func TestRenderDecisionShapeLabelsAreSingleArm(t *testing.T) {
	out := RenderDecisionShape(analyzeDir(t, "./testdata/branching"))
	if strings.Contains(out, " かつ ") {
		t.Errorf("辺のラベルに連言が残っている\n---\n%s", out)
	}
}

// 同じ分岐は 1 つのノードにまとまる。ヘルパーに切り出した判断へ複数の経路から
// 入る形が、合流として描けることを確かめる。
func TestRenderDecisionShapeMergesSharedDecision(t *testing.T) {
	out := RenderDecisionShape(analyzeDir(t, "./testdata/sharedhelper"))

	// byLimit の中の判断。呼び出し元が 2 つあっても 1 ノード。
	if n := strings.Count(out, `{"n #gt; limit"}`); n != 1 {
		t.Errorf("共有された判断が %d ノードある。1 つにまとまるはず\n---\n%s", n, out)
	}
	// 両方の経路がそこへ入る。
	for _, want := range []string{`q0 -- "no" --> q1`, `q2 -- "no" --> q1`} {
		if !strings.Contains(out, want) {
			t.Errorf("合流の辺 %q が無い\n---\n%s", want, out)
		}
	}
}

// switch は値による多分岐なので、真偽を返す if とは見出しで区別する。
func TestRenderDecisionShapeMarksSwitch(t *testing.T) {
	out := RenderDecisionShape(analyzeDir(t, "./testdata/branching"))
	if !strings.Contains(out, `switch:<br/>k`) {
		t.Errorf("タグ付き switch の見出しが付いていない\n---\n%s", out)
	}
}

// armRoutes は入口から出る各経路を「通った枝の列 -> 行き先」の形で返す。
// 判断ノードの ID は登場順に振られるので、構文の違う実装どうしを比べるには
// ID ではなく枝の並びで見る。
func armRoutes(d *Decision, fn string) []string {
	edges := entryEdges(d, fn)
	routes := make([]string, 0, len(edges))
	for _, edge := range edges {
		arms := make([]string, 0, len(edge.Guards))
		for _, g := range edge.Guards {
			arms = append(arms, g.Arm)
		}
		routes = append(routes, strings.Join(arms, "/")+" -> "+edge.To)
	}
	return routes
}

// 判断ノード形式でも、同じ分岐はどの構文で書いても同じ形になってほしい。
// タグなし switch を 1 つの判断として扱うと、条件の違う枝が同じひし形に
// ぶら下がって if / else if 版と形が変わってしまう。
func TestRenderDecisionShapeSyntaxesAgree(t *testing.T) {
	d := analyzeDir(t, "./testdata/branching")

	want := armRoutes(d, "NewBranchElseIf")
	if len(want) == 0 {
		t.Fatalf("NewBranchElseIf の経路が取れていない")
	}
	for _, fn := range []string{"NewBranchEarlyReturn", "NewBranchSwitch", "NewBranchElseIfNoElse"} {
		if got := armRoutes(d, fn); !slices.Equal(got, want) {
			t.Errorf("%s が if/else if 版と一致しない\ngot:  %v\nwant: %v", fn, got, want)
		}
	}
}

// タグなし switch は if / else if と同義なので、値による多分岐としては扱わない。
// 1 つのひし形にまとめると、中身の書けない判断ノードができる。
func TestRenderDecisionShapeSplitsTaglessSwitch(t *testing.T) {
	out := RenderDecisionShape(analyzeDir(t, "./testdata/branching"))
	if strings.Contains(out, `{""}`) {
		t.Errorf("見出しの無い判断ノードがある\n---\n%s", out)
	}
	if strings.Contains(out, `{"switch"}`) {
		t.Errorf("対象式の無い switch ノードがある\n---\n%s", out)
	}
}
