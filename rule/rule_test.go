package rule

import (
	"strings"
	"testing"
)

// 版は本文にしか無いので、消えたり形が変わったりすると取り込み側が
// 突き合わせる手段を失う。落ちるようにしておく。
func TestVersionIsDeclared(t *testing.T) {
	if Version() == "" {
		t.Fatal("規約本文に「規約バージョン: vX.Y.Z」の行が無い")
	}
}

func TestRenderIncludesVersionAndNotice(t *testing.T) {
	out := Render([]string{"domain/**/*.go"})
	for _, want := range []string{
		"---\npaths:\n  - \"domain/**/*.go\"\n---\n",
		"DO NOT EDIT",
		"規約バージョン: " + Version(),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q が無い\n---\n%s", want, out[:min(len(out), 400)])
		}
	}
}

func TestRenderWithoutPathsHasNoFrontmatter(t *testing.T) {
	if strings.HasPrefix(Render(nil), "---") {
		t.Error("paths を渡していないのに frontmatter が付いている")
	}
}
