package flow

import (
	"fmt"
	"go/token"
	"strings"
)

// RenderDecisionShape は判断をノードにした Mermaid の flowchart を含む Markdown を返す。
//
// Render は状態だけをノードにするので、判断は辺のラベルに落ちる。経路上の条件が
// すべて連なるため、分岐が深いほどラベルが伸びて図が横に広がる。同じ判断が複数の
// 経路に現れると、そのぶん複製もされる。
//
// こちらは分岐そのものをひし形のノードにする。辺のラベルは 1 つの枝の名前だけに
// なり、同じ分岐は Guard.Origin が同じなので 1 ノードにまとまる。ヘルパー関数の
// 中の分岐に複数の経路から入る形も、合流として描ける。
func RenderDecisionShape(d *Decision) string {
	return renderMarkdown(d, func(b *strings.Builder) {
		g := newShapeGraph()
		for i, entry := range d.Entries {
			start := fmt.Sprintf("start%d", i)
			fmt.Fprintf(b, "  %s([\"%s\"])\n", start, escape(entry.Func))
			for _, e := range entry.Edges {
				g.addPath(start, e)
			}
		}
		for _, n := range d.Nodes {
			for _, e := range n.Edges {
				g.addPath(n.ID, e)
			}
		}

		for _, q := range g.decisions {
			fmt.Fprintf(b, "  %s{\"%s\"}\n", q.id, q.label)
		}
		writeStateNodes(b, d)
		for _, e := range g.edges {
			if e.label == "" {
				fmt.Fprintf(b, "  %s --> %s\n", e.from, e.to)
				continue
			}
			fmt.Fprintf(b, "  %s -- \"%s\" --> %s\n", e.from, escape(e.label), e.to)
		}
	})
}

// shapeDecision は 1 つの if / switch。
type shapeDecision struct {
	id    string
	label string
}

// decisionLabel はひし形の中身。switch は値による多分岐で、if の真偽判定とは
// 辺のラベルの意味が変わるので、見出しで区別できるようにする。
func decisionLabel(g Guard) string {
	if g.IsSwitch {
		return "switch:<br/>" + escape(g.Subject)
	}
	return escape(g.Subject)
}

type shapeEdge struct {
	from, label, to string
}

type shapeGraph struct {
	decisions []*shapeDecision
	byOrigin  map[token.Pos]*shapeDecision
	edges     []shapeEdge
	seen      map[shapeEdge]bool
}

func newShapeGraph() *shapeGraph {
	return &shapeGraph{
		byOrigin: map[token.Pos]*shapeDecision{},
		seen:     map[shapeEdge]bool{},
	}
}

// addPath は 1 本の遷移を、通った分岐のノードを挟んだ経路に展開する。
//
// 同じ分岐から続けて Guard が積まれることがある。タグなし switch の
// case 2 は「case 1 に当たらない」と「case 2 に当たる」の 2 つを積むし、
// default は先行 case の否定を case の数だけ積む。これは 1 つのひし形から
// 1 本の枝が出ているだけなので、繋ぐと自分自身への辺になってしまう。
// 連続する同じ分岐はまとめ、最後の枝名だけを辺のラベルにする。
func (g *shapeGraph) addPath(from string, e Edge) {
	prev, arm := from, ""
	for _, guard := range e.Guards {
		q := g.decision(guard)
		// 判断の id は状態の id と重ならないので、直前が同じ判断かは id で分かる。
		if q.id == prev {
			arm = guard.Arm
			continue
		}
		g.addEdge(prev, arm, q.id)
		prev, arm = q.id, guard.Arm
	}
	g.addEdge(prev, arm, e.To)
}

// decision は分岐元の位置で判断ノードを引く。同じ分岐なら同じノードになるので、
// 別々の経路から同じ判断に入る形が合流として描ける。
func (g *shapeGraph) decision(guard Guard) *shapeDecision {
	if q, ok := g.byOrigin[guard.Origin]; ok {
		return q
	}
	q := &shapeDecision{
		id:    fmt.Sprintf("q%d", len(g.decisions)),
		label: decisionLabel(guard),
	}
	g.byOrigin[guard.Origin] = q
	g.decisions = append(g.decisions, q)
	return q
}

func (g *shapeGraph) addEdge(from, label, to string) {
	e := shapeEdge{from: from, label: label, to: to}
	if g.seen[e] {
		return
	}
	g.seen[e] = true
	g.edges = append(g.edges, e)
}
