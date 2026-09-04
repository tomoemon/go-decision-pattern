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

// タグなし switch は if / else if と同義なので、値による多分岐としては扱わない。
// 1 つのひし形にまとめると、中身の書けない判断ノードができる。
//
// 抜けた先の条件も case ごとに積む必要がある。まとめて 1 つの Guard にすると、
// そこだけがどの判断にも属さない枝になり、無地のひし形が別に生える。
// testdata の NewBranchTaglessNoDefault がその経路を通る。
func TestRenderDecisionShapeSplitsTaglessSwitch(t *testing.T) {
	d := analyzeDir(t, "./testdata/branching")
	out := RenderDecisionShape(d)
	if strings.Contains(out, `{""}`) {
		t.Errorf("見出しの無い判断ノードがある\n---\n%s", out)
	}
	if strings.Contains(out, `{"switch"}`) {
		t.Errorf("対象式の無い switch ノードがある\n---\n%s", out)
	}

	// 抜けた先は最後の case の「no」に続く。開始点から直接生えてはいけない。
	got := entryRoutes(d, "NewBranchTaglessNoDefault")
	want := entryRoutes(d, "NewBranchElseIfNoElse")
	if !slices.Equal(got, want) {
		t.Errorf("default の無いタグなし switch が if 連鎖と一致しない\ngot:  %v\nwant: %v", got, want)
	}
}

// 型 switch も他の分岐と同じように扱う。case のラベルを出す以上、
// 抜けた先にも「どの型でもなかった」という条件が乗る必要がある。
func TestAnalyzeTypeSwitch(t *testing.T) {
	d := analyzeDir(t, "./testdata/typeswitch")

	got := entryLabels(d, "NewTypeDecision")
	want := "s.(type) は circle / square のいずれでもない"
	if !slices.Contains(got, want) {
		t.Errorf("型 switch を抜けた先の条件が無い\ngot: %v", got)
	}

	out := RenderDecisionShape(d)
	if !strings.Contains(out, `switch:<br/>s.(type)`) {
		t.Errorf("型 switch の見出しが対象式になっていない\n---\n%s", out)
	}
}
