package flow

import (
	"fmt"
	"strings"
)

// renderMarkdown は Decision 1 つ分の Markdown を組み立てる。
// 形式ごとに違うのは flowchart の中身だけなので、外枠はここに集める。
func renderMarkdown(d *Decision, body func(*strings.Builder)) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n\n", d.Name)
	fmt.Fprintf(&b, "`%s`\n\n", d.Package)
	if d.Doc != "" {
		fmt.Fprintf(&b, "%s\n\n", d.Doc)
	}

	b.WriteString("```mermaid\nflowchart TD\n")
	// ラベルは既定で中央寄せになり、箇条書きの先頭が行ごとにずれる。
	// htmlLabels が無効な環境では効かず中央寄せのままだが、崩れはしない。
	b.WriteString("  classDef box text-align:left;\n")
	body(&b)
	b.WriteString("```\n")

	if len(d.Warnings) > 0 {
		b.WriteString("\n### 警告\n\n")
		for _, w := range d.Warnings {
			fmt.Fprintf(&b, "- %s\n", strings.ReplaceAll(w, "\n", " "))
		}
	}
	return b.String()
}

// writeStateNodes は状態ノードの形を書く。どの形式でも状態の描き方は変わらない。
func writeStateNodes(b *strings.Builder, d *Decision) {
	for _, n := range d.Nodes {
		fmt.Fprintf(b, "  %s%s\n", n.ID, shape(n))
	}
}

// Render は Decision 1 つ分を Mermaid の flowchart を含む Markdown にする。
func Render(d *Decision) string {
	return renderMarkdown(d, func(b *strings.Builder) { writeStateFlow(b, d) })
}

func writeStateFlow(b *strings.Builder, d *Decision) {
	for i, entry := range d.Entries {
		start := fmt.Sprintf("start%d", i)
		fmt.Fprintf(b, "  %s([\"%s\"])\n", start, escape(entry.Func))
		for _, e := range entry.Edges {
			// 開始が 1 本しかないなら条件は自明なのでラベルを省く。
			if len(entry.Edges) == 1 {
				fmt.Fprintf(b, "  %s --> %s\n", start, e.To)
				continue
			}
			fmt.Fprintf(b, "  %s -- \"%s\" --> %s\n", start, escape(e.Label), e.To)
		}
	}
	writeStateNodes(b, d)
	for _, n := range d.Nodes {
		for _, e := range n.Edges {
			fmt.Fprintf(b, "  %s -- \"%s\" --> %s\n", n.ID, escape(e.Label), e.To)
		}
	}
}

// shape は Need を四角、終端をスタジアム型にする。
//
// 引数やフィールドは 1 行 1 つにする。カンマで 1 行に並べるとノードが横に伸び、
// 分岐の多い図でレイアウトが崩れる。
// エスケープは連結より前に行う。あとから掛けると <br/> まで実体参照になる。
func shape(n *Node) string {
	// "-" は状態自身が持つもの、"+" は Decide に外から渡されるもの。
	// 同じ記号で並べると、持ち物と入力が同じものに見える。
	items := make([]string, 0, len(n.Fields)+len(n.Params))
	for _, line := range n.Fields {
		items = append(items, "- "+escape(line))
	}
	for _, line := range n.Params {
		items = append(items, "+ "+escape(line))
	}
	label := escape(n.Type)
	if len(items) > 0 {
		label += "<br/>" + strings.Join(items, "<br/>")
	}
	if n.Kind == KindTerminal {
		return fmt.Sprintf("([\"%s\"]):::box", label)
	}
	return fmt.Sprintf("[\"%s\"]:::box", label)
}

// escape は Mermaid のラベルを壊す文字だけを実体参照にする。
// Go のソースをそのままラベルに載せるので、括弧や記号は読めるまま残す。
var labelReplacer = strings.NewReplacer(
	"&", "#amp;",
	`"`, "#quot;",
	"<", "#lt;",
	">", "#gt;",
)

func escape(s string) string {
	return labelReplacer.Replace(s)
}
