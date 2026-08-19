package flow

import (
	"strings"
	"testing"
)

func analyzeExample(t *testing.T) *Decision {
	t.Helper()
	decisions, err := Analyze(".", []string{"./testdata/example"})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(decisions) != 1 {
		var names []string
		for _, d := range decisions {
			names = append(names, d.Name)
		}
		t.Fatalf("decision:decl が付いた型だけが対象になるはず: got %v", names)
	}
	return decisions[0]
}

// decision:decl の無い sum type を拾わないことが、このツールを sumtype:decl と
// 分けた理由そのものなので、件数の確認を独立したテストにする。
func TestAnalyzeSelectsOnlyTaggedInterface(t *testing.T) {
	d := analyzeExample(t)
	if d.Name != "ExampleDecision" {
		t.Errorf("Name = %q, want ExampleDecision", d.Name)
	}
}

func TestAnalyzeEntryAndTransitions(t *testing.T) {
	d := analyzeExample(t)
	out := Render(d)

	byID := map[string]*Node{}
	for _, n := range d.Nodes {
		byID[n.ID] = n
	}

	if len(d.Entries) != 1 || d.Entries[0].Func != "NewExampleDecision" {
		t.Fatalf("Entries = %+v, want NewExampleDecision 1 件", d.Entries)
	}
	if got := byID[d.Entries[0].Edges[0].To].Type; got != "NeedUser" {
		t.Errorf("開始状態 = %q, want NeedUser", got)
	}

	tests := []struct {
		name string
		want string
	}{
		// 分岐条件がそのままラベルになる
		{"失敗への分岐", `-- "deleted" -->`},
		// Decide の引数が「その状態で何を渡すか」として出る
		{"取得キーと引数を記号で分ける", `NeedBlock<br/>- exampleFacts<br/>+ blocked bool<br/>+ count int`},
		// ヘルパーに切り出した分岐の中まで辿る
		{"ヘルパー越しの遷移", `-- "count == 0" -->`},
		// 同じ終端型でもフィールドが違えば別ノードにする
		{"終端をリテラルで分ける", `Decided<br/>- Reason: #quot;ok#quot;`},
		{"終端をリテラルで分ける2", `Decided<br/>- Reason: #quot;empty#quot;`},
		// 条件のない末尾の return
		{"それ以外", `-- "それ以外" -->`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(out, tt.want) {
				t.Errorf("出力に %q が含まれない\n---\n%s", tt.want, out)
			}
		})
	}
}

// 到達不能な状態の検出は go-check-sumtype では代替できない。
// あちらは interpreter の switch の網羅しか見ないため。
func TestAnalyzeWarnsUnreachableState(t *testing.T) {
	d := analyzeExample(t)
	for _, w := range d.Warnings {
		if strings.Contains(w, "Unreachable") && strings.Contains(w, "到達しない") {
			return
		}
	}
	t.Errorf("到達不能な状態の警告が無い: %v", d.Warnings)
}

func TestAnalyzeUnreachableNodeIsStillRendered(t *testing.T) {
	d := analyzeExample(t)
	out := Render(d)
	if !strings.Contains(out, "Unreachable") {
		t.Errorf("到達不能な状態も図に残すべき\n---\n%s", out)
	}
}

// ルール上は書けるが解析が難しい形。黙って落とさず、何が起きたか分かる形で
// 報告できているかを確かめる。
func TestAnalyzeEdgeCases(t *testing.T) {
	decisions, err := Analyze(".", []string{"./testdata/edge"})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions = %d, want 1", len(decisions))
	}
	d := decisions[0]

	t.Run("ヘルパーは開始点にしない", func(t *testing.T) {
		for _, e := range d.Entries {
			if e.Func == "pickTail" {
				t.Errorf("状態を返すだけのヘルパーが開始点になっている: %+v", d.Entries)
			}
		}
		if len(d.Entries) != 1 || d.Entries[0].Func != "NewEdgeDecision" {
			t.Errorf("Entries = %+v, want NewEdgeDecision のみ", d.Entries)
		}
	})

	t.Run("状態でない戻り値をノードにしない", func(t *testing.T) {
		for _, n := range d.Nodes {
			if n.Type == "simpleErr" {
				t.Errorf("error 型がノードになっている: %+v", d.Nodes)
			}
		}
	})

	tests := []struct {
		name string
		want string
	}{
		{"nil 返却", "nil を返している"},
		{"Decide 以外の遷移メソッド", "Resolve が遷移メソッドに見える"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, w := range d.Warnings {
				if strings.Contains(w, tt.want) {
					return
				}
			}
			t.Errorf("警告に %q が無い: %v", tt.want, d.Warnings)
		})
	}
}

// README に載せている例。図が古くならないよう、生成結果をここで固定する。
// 規約どおりに書いた最小構成なので、警告が出たらどちらかが規約から外れている。
func TestAnalyzeReadmeExample(t *testing.T) {
	decisions, err := Analyze(".", []string{"./testdata/publish"})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}
	if len(decisions) != 1 {
		t.Fatalf("decisions = %d, want 1", len(decisions))
	}
	d := decisions[0]
	if len(d.Warnings) != 0 {
		t.Errorf("規約どおりの例で警告が出ている: %v", d.Warnings)
	}

	out := Render(d)
	want := []string{
		`n0["NeedArticle<br/>- ArticleID ArticleID<br/>+ article Article"]`,
		`n3["NeedAuthorSuspension<br/>- publishFactsArticle<br/>+ suspended bool<br/>+ now time.Time"]`,
		`n1(["Failed<br/>- Err: ErrNotDraft"])`,
		`n2(["Failed<br/>- Err: ErrEmptyBody"])`,
		`n5(["Decided<br/>- PublishedAt: now"])`,
		`n0 -- "article.Status != StatusDraft" --> n1`,
		`n3 -- "それ以外" --> n5`,
	}
	for _, w := range want {
		if !strings.Contains(out, w) {
			t.Errorf("README の図と一致しない。出力に %q が無い\n---\n%s", w, out)
		}
	}
}
