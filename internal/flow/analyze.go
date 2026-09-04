package flow

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/printer"
	"go/token"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Tag は解析対象を宣言するコメント。sumtype:decl は go-check-sumtype 用の汎用
// マーカーで Decision パターン以外の sum type にも付くため、対象はこのタグで絞る。
const Tag = "//decision:decl"

// NodeKind はノードの種類。Need は Decide を持つ中間状態、Terminal は持たない終端状態。
type NodeKind int

const (
	KindNeed NodeKind = iota
	KindTerminal
)

// Node はフローチャートの 1 ノード。
//
// Need 状態は型ごとに 1 ノードだが、終端状態は返却リテラルのフィールドごとに分ける。
// 同じ Decided 型でも Reason が違えば別の結末なので、まとめると
// 「どの条件でどの結末になるか」が読めなくなる。
type Node struct {
	ID   string
	Type string
	// Fields は状態自身が持つもの。Need なら取得キーと確定した事実、
	// 終端なら返却リテラルのフィールド。
	Fields []string
	// Params は Decide に外から渡されるもの。Need のみ。
	Params []string
	Kind   NodeKind
	Edges  []Edge
}

// Guard は経路上の 1 つの分岐で、どの枝を選んだかを表す。
//
// Text だけあれば状態遷移図は描けるが、それだと「この条件とあの条件は同じ if の
// 表と裏」という関係が失われる。判断をノードにして描くには、どの分岐から来たかを
// 知る必要があるので、分岐元の位置を持たせる。ヘルパー関数の中の分岐は複数の経路
// から辿られるが、同じ AST ノードなので Origin も同じ値になり、1 つの判断として
// まとめられる。
type Guard struct {
	Text     string    // 枝の条件。状態遷移図のラベルはこれを連ねたもの
	Origin   token.Pos // 分岐元の if / switch の位置
	Subject  string    // 判断そのもの。if なら条件式、switch なら対象の式
	IsSwitch bool      // switch の分岐か
	Arm      string    // 枝の名前。yes / no、または case の値
}

// Edge は状態遷移。表示用のラベルは Guards から guardsLabel で導く。
// 同じものをフィールドにも持つと、片方だけ更新される余地が残る。
type Edge struct {
	To     string
	Guards []Guard
}

// Entry は開始点。1 つのコンストラクタが条件ごとに違う状態から始める場合があるので、
// 関数ごとに 1 つにまとめて分岐は Edge のラベルに載せる。関数呼び出しごとにノードを
// 作ると、分岐が多いコンストラクタで同じ名前のノードが並んで読めなくなる。
type Entry struct {
	Func  string
	Edges []Edge
}

// Decision は //decision:decl が付いた sum type 1 つ分の解析結果。
type Decision struct {
	Name     string
	Package  string
	Doc      string
	Entries  []Entry
	Nodes    []*Node
	Warnings []string
}

type analyzer struct {
	pkg      *packages.Package
	iface    *types.Interface
	ifaceObj types.Object
	funcDecl map[*types.Func]*ast.FuncDecl
	plainFn  map[*types.Func]*ast.FuncDecl

	nodes  map[string]*Node
	order  []string
	warns  []string
	prefix string

	// inlined は遷移の途中で中身を辿った関数。開始点の候補から外すために使う。
	inlined map[*types.Func]bool
	// seenStates は図に現れた状態の型名。1 つも現れない状態を見つけるために使う。
	seenStates map[string]bool
}

// Analyze は指定パターンのパッケージを読み、//decision:decl が付いた sum type ごとの
// フローを返す。
func Analyze(dir string, patterns []string) ([]*Decision, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo | packages.NeedImports | packages.NeedDeps,
		Dir: dir,
	}
	pkgs, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, fmt.Errorf("failed to load packages: %w", err)
	}
	var loadErr error
	packages.Visit(pkgs, nil, func(p *packages.Package) {
		for _, e := range p.Errors {
			// 対象パッケージ自体のエラーだけを致命扱いにする。依存側の軽微な
			// エラーで全体を止めると、解析したいパッケージが 1 つも見られなくなる。
			if p.PkgPath != "" && len(p.Syntax) > 0 && loadErr == nil {
				loadErr = fmt.Errorf("%s: %v", p.PkgPath, e)
			}
		}
	})
	if loadErr != nil {
		return nil, loadErr
	}

	var decisions []*Decision
	for _, pkg := range pkgs {
		decisions = append(decisions, analyzePackage(pkg)...)
	}
	sort.Slice(decisions, func(i, j int) bool {
		if decisions[i].Package != decisions[j].Package {
			return decisions[i].Package < decisions[j].Package
		}
		return decisions[i].Name < decisions[j].Name
	})
	return decisions, nil
}

func analyzePackage(pkg *packages.Package) []*Decision {
	if pkg.Types == nil || pkg.TypesInfo == nil {
		return nil
	}
	funcDecl, plainFn := indexFuncs(pkg)

	var out []*Decision
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				doc := ts.Doc
				if doc == nil {
					doc = gen.Doc
				}
				if !hasDecisionTag(doc) {
					continue
				}
				a := &analyzer{
					pkg:        pkg,
					funcDecl:   funcDecl,
					plainFn:    plainFn,
					nodes:      map[string]*Node{},
					inlined:    map[*types.Func]bool{},
					seenStates: map[string]bool{},
				}
				if d := a.analyzeInterface(ts, doc); d != nil {
					out = append(out, d)
				}
			}
		}
	}
	return out
}

func hasDecisionTag(doc *ast.CommentGroup) bool {
	if doc == nil {
		return false
	}
	for _, c := range doc.List {
		if strings.TrimSpace(c.Text) == Tag {
			return true
		}
	}
	return false
}

// indexFuncs は型情報から AST の関数宣言を引けるようにする。
// メソッドと非メソッドを分けるのは、開始状態の探索が非メソッドだけを見るため。
func indexFuncs(pkg *packages.Package) (methods, plain map[*types.Func]*ast.FuncDecl) {
	methods = map[*types.Func]*ast.FuncDecl{}
	plain = map[*types.Func]*ast.FuncDecl{}
	for _, file := range pkg.Syntax {
		for _, decl := range file.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok {
				continue
			}
			fn, ok := pkg.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			if fd.Recv != nil {
				methods[fn] = fd
			} else {
				plain[fn] = fd
			}
		}
	}
	return methods, plain
}

func (a *analyzer) analyzeInterface(ts *ast.TypeSpec, doc *ast.CommentGroup) *Decision {
	obj := a.pkg.TypesInfo.Defs[ts.Name]
	if obj == nil {
		return nil
	}
	named, ok := obj.Type().(*types.Named)
	if !ok {
		return nil
	}
	iface, ok := named.Underlying().(*types.Interface)
	if !ok {
		a.warnf("%s に %s が付いているが interface ではない", ts.Name.Name, Tag)
		return nil
	}
	a.iface = iface
	a.ifaceObj = obj
	a.prefix = strings.TrimSuffix(ts.Name.Name, "Decision")

	states := a.findStates()
	if len(states) == 0 {
		a.warnf("%s を満たす状態が同じパッケージに見つからない", ts.Name.Name)
	}

	d := &Decision{
		Name:    ts.Name.Name,
		Package: a.pkg.PkgPath,
		Doc:     firstDocLine(doc),
	}

	// 先にすべての Need 状態のノードを作る。遷移先として現れない状態
	// (到達不能) を検出するため、エッジから作らず状態一覧から作る。
	for _, st := range states {
		if a.decideMethod(st) != nil {
			a.needNode(st)
		}
	}
	for _, st := range states {
		fd := a.decideMethod(st)
		if fd == nil {
			continue
		}
		from := a.needNode(st)
		for _, r := range a.collectReturns(fd, nil, map[*types.Func]bool{}) {
			a.addEdge(from, r)
		}
	}

	d.Entries = a.findEntries()
	for _, id := range a.order {
		d.Nodes = append(d.Nodes, a.nodes[id])
	}
	a.checkMissingStates(states)
	a.checkUnreachable(d)
	orderByFlow(d)
	d.Warnings = a.warns
	return d
}

// orderByFlow はノードを開始点からの幅優先順に並べ替え、ID を振り直す。
// 型名の順のままだと、生成物のテキストを読んだときに流れを追えない
// (Mermaid の描画は変わらないが、差分レビューでは順序が意味を持つ)。
func orderByFlow(d *Decision) {
	byID := map[string]*Node{}
	for _, n := range d.Nodes {
		byID[n.ID] = n
	}
	var ordered []*Node
	seen := map[string]bool{}
	var queue []string
	for _, entry := range d.Entries {
		for _, e := range entry.Edges {
			queue = append(queue, e.To)
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		n := byID[id]
		ordered = append(ordered, n)
		for _, e := range n.Edges {
			queue = append(queue, e.To)
		}
	}
	// 到達不能なノードも落とさずに末尾へ回す。警告と突き合わせられるようにする。
	for _, n := range d.Nodes {
		if !seen[n.ID] {
			ordered = append(ordered, n)
		}
	}

	rename := make(map[string]string, len(ordered))
	for i, n := range ordered {
		rename[n.ID] = fmt.Sprintf("n%d", i)
	}
	for _, n := range ordered {
		n.ID = rename[n.ID]
		for i := range n.Edges {
			n.Edges[i].To = rename[n.Edges[i].To]
		}
	}
	for i := range d.Entries {
		for j := range d.Entries[i].Edges {
			d.Entries[i].Edges[j].To = rename[d.Entries[i].Edges[j].To]
		}
	}
	d.Nodes = ordered
}

// findStates は interface を満たす、同じパッケージの名前付き型を集める。
func (a *analyzer) findStates() []*types.Named {
	var out []*types.Named
	scope := a.pkg.Types.Scope()
	for _, name := range scope.Names() {
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || tn.IsAlias() {
			continue
		}
		named, ok := tn.Type().(*types.Named)
		if !ok || types.IsInterface(named) {
			continue
		}
		if types.Implements(named, a.iface) {
			out = append(out, named)
			continue
		}
		// 値で返す規約に反してポインタだけが満たしている場合は、
		// go-check-sumtype は通るが interpreter の case にマッチしない。
		if types.Implements(types.NewPointer(named), a.iface) {
			a.warnf("%s は値では %s を満たしていない (ポインタのみ)。case にマッチせず default に落ちる",
				named.Obj().Name(), a.ifaceObj.Name())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Obj().Name() < out[j].Obj().Name() })
	return out
}

func (a *analyzer) decideMethod(named *types.Named) *ast.FuncDecl {
	for i := range named.NumMethods() {
		m := named.Method(i)
		if m.Name() != "Decide" {
			continue
		}
		return a.funcDecl[m]
	}
	return nil
}

// returnSite は 1 つの return 文が返す状態と、そこに至る分岐条件。
type returnSite struct {
	typ     *types.Named
	lit     *ast.CompositeLit
	guards  []Guard
	unknown string
}

// collectReturns は Decide の本体から return を集める。
//
// return が同じパッケージの関数呼び出しになっている場合はその関数の中まで辿る。
// 辿らないと「ヘルパーに切り出した分岐」が図から丸ごと消える。
func (a *analyzer) collectReturns(fd *ast.FuncDecl, guards []Guard, seen map[*types.Func]bool) []returnSite {
	if fd == nil || fd.Body == nil {
		return nil
	}
	sig := a.signatureOf(fd)
	var out []returnSite
	a.walkStmts(fd.Body.List, guards, func(ret *ast.ReturnStmt, g []Guard) {
		out = append(out, a.resolveReturnStmt(ret, sig, g, seen)...)
	})
	return out
}

func (a *analyzer) signatureOf(fd *ast.FuncDecl) *types.Signature {
	fn, ok := a.pkg.TypesInfo.Defs[fd.Name].(*types.Func)
	if !ok {
		return nil
	}
	sig, _ := fn.Type().(*types.Signature)
	return sig
}

// resolveReturnStmt は return 文のうち Decision を返している位置だけを見る。
// Decide が (XxxDecision, error) を返す形もルール上は書けるので、位置を見ずに
// 全オペランドを解決すると error や nil が状態として図に出てしまう。
func (a *analyzer) resolveReturnStmt(ret *ast.ReturnStmt, sig *types.Signature, guards []Guard, seen map[*types.Func]bool) []returnSite {
	if sig == nil {
		return nil
	}
	results := sig.Results()
	if len(ret.Results) == 0 {
		if a.hasDecisionResult(sig) {
			a.warnf("名前付き戻り値の naked return は解決できない")
		}
		return nil
	}
	// 1 つの呼び出しが複数の戻り値をそのまま埋めている形。
	if len(ret.Results) == 1 && results.Len() > 1 {
		return a.resolveReturn(ret.Results[0], guards, seen)
	}
	var out []returnSite
	for i, expr := range ret.Results {
		if i >= results.Len() || !a.isDecisionType(results.At(i).Type()) {
			continue
		}
		out = append(out, a.resolveReturn(expr, guards, seen)...)
	}
	return out
}

func (a *analyzer) hasDecisionResult(sig *types.Signature) bool {
	for i := range sig.Results().Len() {
		if a.isDecisionType(sig.Results().At(i).Type()) {
			return true
		}
	}
	return false
}

// isDecisionType は interface そのものか、それを満たす状態かを返す。
func (a *analyzer) isDecisionType(t types.Type) bool {
	if types.Identical(t, a.ifaceObj.Type()) {
		return true
	}
	named, ok := t.(*types.Named)
	return ok && !types.IsInterface(named) && types.Implements(named, a.iface)
}

func (a *analyzer) resolveReturn(expr ast.Expr, guards []Guard, seen map[*types.Func]bool) []returnSite {
	typ := a.pkg.TypesInfo.TypeOf(expr)
	if typ == nil {
		return []returnSite{{guards: guards, unknown: a.src(expr)}}
	}
	// 状態は必ず値で返す規約なので nil は来ない。来ると interpreter が状態を
	// 受け取れず、case にも当たらないまま回り続ける。
	if basic, ok := typ.(*types.Basic); ok && basic.Kind() == types.UntypedNil {
		a.warnf("Decision に nil を返している。interpreter が状態を受け取れない")
		return nil
	}
	if named, ok := typ.(*types.Named); ok && !types.IsInterface(named) {
		if !types.Implements(named, a.iface) {
			// 状態ではない型。Decision を返す位置に来ることは無いはずなので、
			// 黙って捨てずに知らせる。
			a.warnf("%s は %s を満たさないので状態として扱えない", named.Obj().Name(), a.ifaceObj.Name())
			return nil
		}
		lit, _ := ast.Unparen(expr).(*ast.CompositeLit)
		return []returnSite{{typ: named, lit: lit, guards: guards}}
	}
	// interface 型のまま返っている。同じパッケージの関数呼び出しなら中を辿る。
	call, ok := ast.Unparen(expr).(*ast.CallExpr)
	if !ok {
		return []returnSite{{guards: guards, unknown: a.src(expr)}}
	}
	fn := a.calleeFunc(call)
	if fn == nil || seen[fn] {
		return []returnSite{{guards: guards, unknown: a.src(expr)}}
	}
	fd := a.funcDecl[fn]
	if fd == nil {
		fd = a.plainFn[fn]
	}
	if fd == nil {
		return []returnSite{{guards: guards, unknown: a.src(expr)}}
	}
	seen[fn] = true
	defer delete(seen, fn)
	a.inlined[fn] = true
	return a.collectReturns(fd, guards, seen)
}

func (a *analyzer) calleeFunc(call *ast.CallExpr) *types.Func {
	var ident *ast.Ident
	switch fun := ast.Unparen(call.Fun).(type) {
	case *ast.Ident:
		ident = fun
	case *ast.SelectorExpr:
		ident = fun.Sel
	default:
		return nil
	}
	fn, _ := a.pkg.TypesInfo.Uses[ident].(*types.Func)
	if fn == nil || fn.Pkg() == nil || fn.Pkg() != a.pkg.Types {
		return nil
	}
	return fn
}

// walkStmts は文を辿りながら、その return に至るまでの分岐条件を積む。
func (a *analyzer) walkStmts(stmts []ast.Stmt, guards []Guard, emit func(*ast.ReturnStmt, []Guard)) {
	for _, stmt := range stmts {
		a.walkStmt(stmt, guards, emit)
		if negs := a.guardsAfter(stmt); len(negs) > 0 {
			next := make([]Guard, 0, len(guards)+len(negs))
			guards = append(append(next, guards...), negs...)
		}
	}
}

// guardsAfter は stmt を通過して次の文へ進んだときに成り立つ条件を返す。
// 通過する経路が無い、または条件で表せないときは nil。
//
// 分岐の全枝が脱出するなら、その後ろに書かれた文は「どの枝にも入らなかった場合」
// にあたる。else を書いたときに乗る否定条件と同じものを、以降の文へ伝播させる。
func (a *analyzer) guardsAfter(stmt ast.Stmt) []Guard {
	switch s := stmt.(type) {
	case *ast.IfStmt:
		return a.ifFallthroughGuards(s)
	case *ast.SwitchStmt:
		return a.switchFallthroughGuards(s)
	case *ast.TypeSwitchStmt:
		return a.typeSwitchFallthroughGuards(s)
	}
	return nil
}

// ifFallthroughGuards は if 連鎖を抜けた先に積むべき否定条件を返す。
// if / else if の枝がすべて脱出するときだけ、全条件の否定を返す。
// 途中に脱出しない枝があると抜けた先へ複数の経路が合流するので、条件で表せない。
func (a *analyzer) ifFallthroughGuards(ifs *ast.IfStmt) []Guard {
	var negs []Guard
	for {
		if !terminates(ifs.Body, true) {
			return nil
		}
		// else 側に乗る条件と、連鎖を抜けた先に乗る条件は同じもの。
		negs = append(negs, a.ifGuard(ifs, false))
		switch els := ifs.Else.(type) {
		case nil:
			return negs
		case *ast.IfStmt:
			ifs = els
		case *ast.BlockStmt:
			// 最後の else も脱出するなら if 連鎖の後ろには到達しない。
			if terminates(els, true) {
				return nil
			}
			return negs
		default:
			return nil
		}
	}
}

// switchFallthroughGuards は switch を抜けた先に積むべき否定条件を返す。
// default 節があるか、脱出しない case があれば nil。
//
// case ごとに 1 つずつ返す。タグなし switch は case ごとに別の判断として扱って
// いるので、まとめて 1 つの Guard にすると、抜けた先だけがどの判断にも属さない
// 宙に浮いた枝になる。タグ付きでは全 case が同じ判断に属するため、同じ Origin の
// Guard が並ぶ形になる。
func (a *analyzer) switchFallthroughGuards(sw *ast.SwitchStmt) []Guard {
	var negs []Guard
	for _, c := range sw.Body.List {
		cc, ok := c.(*ast.CaseClause)
		if !ok {
			return nil
		}
		if len(cc.List) == 0 { // default があるなら switch を素通りしない
			return nil
		}
		// case の中の break は switch を出るだけなので、抜けた先へ進む経路が
		// 残る。脱出とは見なせない。
		if !terminatesList(cc.Body, false) {
			return nil
		}
		negs = append(negs, a.switchNegGuard(sw, cc))
	}
	return negs
}

// ifDecision / caseDecision は「どの判断に属するか」だけを決める。
// 枝の名前と条件の文字列は呼び出し側で足す。判断の同定は肯定・否定に依らないので、
// ここを分けておかないと同じ規則を 4 箇所に書くことになる。
func (a *analyzer) ifDecision(ifs *ast.IfStmt) Guard {
	return Guard{Origin: ifs.Pos(), Subject: a.condLabel(ifs.Init, a.src(ifs.Cond))}
}

// caseDecision はタグの有無で分ける。タグ付きは値による多分岐なので switch 全体で
// 1 つの判断。タグなしは if / else if と同義なので case ごとに別の判断になる。
// まとめてしまうと、条件の違う枝が 1 つのひし形にぶら下がって見出しが書けなくなる。
func (a *analyzer) caseDecision(sw *ast.SwitchStmt, cc *ast.CaseClause) Guard {
	if sw.Tag == nil {
		return Guard{Origin: cc.Pos(), Subject: a.caseLabel(nil, cc)}
	}
	return Guard{Origin: sw.Pos(), Subject: a.src(sw.Tag), IsSwitch: true}
}

// ifGuard は if の枝に入る条件を表す Guard を作る。
func (a *analyzer) ifGuard(ifs *ast.IfStmt, yes bool) Guard {
	g := a.ifDecision(ifs)
	if yes {
		g.Text, g.Arm = g.Subject, "yes"
		return g
	}
	g.Text, g.Arm = a.condLabel(ifs.Init, a.negateExpr(ifs.Cond)), "no"
	return g
}

// typeSwitchFallthroughGuards は型 switch を抜けた先に積むべき条件を返す。
// 判断の生成側で型 switch は使われないが、case のラベルは出しているので、
// 抜けた先だけ条件が付かないのは不揃いになる。
func (a *analyzer) typeSwitchFallthroughGuards(sw *ast.TypeSwitchStmt) []Guard {
	var cases []string
	for _, c := range sw.Body.List {
		cc, ok := c.(*ast.CaseClause)
		if !ok {
			return nil
		}
		if len(cc.List) == 0 { // default があるなら素通りしない
			return nil
		}
		if !terminatesList(cc.Body, false) {
			return nil
		}
		cases = append(cases, a.caseLabel(nil, cc))
	}
	if len(cases) == 0 {
		return nil
	}
	subject := a.typeSwitchSubject(sw)
	return []Guard{{
		Text:     subject + " は " + strings.Join(cases, " / ") + " のいずれでもない",
		Origin:   sw.Pos(),
		Subject:  subject,
		IsSwitch: true,
		Arm:      "それ以外",
	}}
}

// switchGuard は case 節が選ばれたことを表す Guard を作る。
func (a *analyzer) switchGuard(sw *ast.SwitchStmt, cc *ast.CaseClause) Guard {
	g := a.caseDecision(sw, cc)
	// タグなしの case は判断そのものが条件なので、Subject をそのまま使える。
	g.Text, g.Arm = g.Subject, "yes"
	if g.IsSwitch {
		g.Text, g.Arm = a.caseLabel(sw.Tag, cc), a.caseLabel(nil, cc) // 枝はタグを外した値だけ
	}
	return g
}

// switchNegGuard は case 節に当たらなかったことを表す Guard を作る。
func (a *analyzer) switchNegGuard(sw *ast.SwitchStmt, cc *ast.CaseClause) Guard {
	g := a.caseDecision(sw, cc)
	g.Text = a.negateCase(cc, sw.Tag)
	g.Arm = "no"
	if g.IsSwitch {
		g.Arm = "それ以外"
	}
	return g
}

func (a *analyzer) typeSwitchSubject(s *ast.TypeSwitchStmt) string {
	var expr ast.Expr
	switch assign := s.Assign.(type) {
	case *ast.AssignStmt:
		if len(assign.Rhs) == 1 {
			expr = assign.Rhs[0]
		}
	case *ast.ExprStmt:
		expr = assign.X
	}
	ta, ok := expr.(*ast.TypeAssertExpr)
	if !ok {
		return "type switch"
	}
	return a.src(ta.X) + ".(type)"
}

// switchSubject は判断ノードに出す switch の見出しを作る。
// タグなし switch は if / else if と同義なので、対象の式を持たない。
func (a *analyzer) switchSubject(tag ast.Expr) string {
	if tag == nil {
		return ""
	}
	return a.src(tag)
}

// terminates はブロックが必ず脱出するかを判定する。Go 仕様の terminating
// statement のうち、判定の中に現れうるものを実装する。
//
// 実装しないもの: goto、ラベル付き break の追跡、条件を持たない for。
// いずれも判定関数の中では使われないため、false を返して伝播を諦める。
// 諦めた側に倒すと、ラベルが付かないだけで嘘は出ない。
//
// allowBreak は break を脱出と見なすかどうか。囲みが for なら break でその先へ
// 進まないが、switch の case なら break は switch を出るだけなので数えられない。
func terminates(b *ast.BlockStmt, allowBreak bool) bool {
	if b == nil {
		return false
	}
	return terminatesList(b.List, allowBreak)
}

// terminatesList は文の並びが必ず脱出するかを判定する。末尾の 1 文で決まる。
func terminatesList(list []ast.Stmt, allowBreak bool) bool {
	if len(list) == 0 {
		return false
	}
	return terminatesStmt(list[len(list)-1], allowBreak)
}

func terminatesStmt(stmt ast.Stmt, allowBreak bool) bool {
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		return true
	case *ast.BranchStmt:
		return s.Tok == token.CONTINUE || (s.Tok == token.BREAK && allowBreak)
	case *ast.ExprStmt:
		call, ok := s.X.(*ast.CallExpr)
		if !ok {
			return false
		}
		id, ok := call.Fun.(*ast.Ident)
		return ok && id.Name == "panic"
	case *ast.BlockStmt:
		return terminates(s, allowBreak)
	case *ast.LabeledStmt:
		return terminatesStmt(s.Stmt, allowBreak)
	case *ast.IfStmt:
		// else が無ければ、条件が偽のときに素通りする。
		if s.Else == nil {
			return false
		}
		return terminates(s.Body, allowBreak) && terminatesStmt(s.Else, allowBreak)
	case *ast.SwitchStmt:
		return terminatesClauses(s.Body, true)
	case *ast.TypeSwitchStmt:
		return terminatesClauses(s.Body, true)
	case *ast.SelectStmt:
		// select は必ずどれかの通信が選ばれるので default は要らない。
		return terminatesClauses(s.Body, false)
	}
	return false
}

// terminatesClauses は switch / select の全節が脱出するかを判定する。
// needDefault が真なら、どの節にも当たらない経路を塞ぐために default が要る。
func terminatesClauses(body *ast.BlockStmt, needDefault bool) bool {
	if body == nil || len(body.List) == 0 {
		return false
	}
	hasDefault := false
	for _, c := range body.List {
		var list []ast.Stmt
		switch cc := c.(type) {
		case *ast.CaseClause:
			hasDefault = hasDefault || len(cc.List) == 0
			list = cc.Body
		case *ast.CommClause:
			hasDefault = hasDefault || cc.Comm == nil
			list = cc.Body
		default:
			return false
		}
		// 節の中の break は switch / select を出るだけで、脱出ではない。
		if !terminatesList(list, false) {
			return false
		}
	}
	return hasDefault || !needDefault
}

// negateCase は case 節の条件全体を否定する。case A, B: は A || B なので、
// 否定は !(A) かつ !(B) になる。
func (a *analyzer) negateCase(cc *ast.CaseClause, tag ast.Expr) string {
	parts := make([]string, 0, len(cc.List))
	for _, e := range cc.List {
		cond := e
		if tag != nil {
			cond = &ast.BinaryExpr{X: tag, Op: token.EQL, Y: e}
		}
		parts = append(parts, a.negateExpr(cond))
	}
	return joinConds(parts)
}

// negateExpr は条件を否定した表示用の文字列を返す。二重否定を畳み、
// 比較演算子は反転させて読みやすくする。
// 判定は式の構造で行う。文字列で見ると "(a == b) != c" のように
// 括弧や引数の内側の演算子を掴んでしまい、別の条件になってしまう。
func (a *analyzer) negateExpr(e ast.Expr) string {
	switch x := ast.Unparen(e).(type) {
	case *ast.UnaryExpr:
		if x.Op == token.NOT {
			return a.src(ast.Unparen(x.X))
		}
	case *ast.BinaryExpr:
		switch x.Op {
		case token.EQL:
			return a.src(x.X) + " != " + a.src(x.Y)
		case token.NEQ:
			return a.src(x.X) + " == " + a.src(x.Y)
		}
	case *ast.Ident, *ast.SelectorExpr, *ast.CallExpr, *ast.IndexExpr:
		// 単項の ! を前に置いても意味が変わらない形。括弧は要らない。
		return "!" + a.src(x)
	}
	return "!(" + a.src(e) + ")"
}

// condLabel は if の条件を、初期化節があればそれも添えてラベルにする。
// 初期化付きの if は条件だけ出すと "ok" のような無意味なラベルになる。
// 何を評価した ok なのかは初期化節にしか書かれていない。
func (a *analyzer) condLabel(init ast.Stmt, cond string) string {
	if init == nil {
		return cond
	}
	return a.src(init) + "; " + cond
}

func (a *analyzer) walkStmt(stmt ast.Stmt, guards []Guard, emit func(*ast.ReturnStmt, []Guard)) {
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		emit(s, guards)
	case *ast.BlockStmt:
		a.walkStmts(s.List, guards, emit)
	case *ast.IfStmt:
		a.walkStmts(s.Body.List, append(cloneGuards(guards), a.ifGuard(s, true)), emit)
		if s.Else != nil {
			a.walkStmt(s.Else, append(cloneGuards(guards), a.ifGuard(s, false)), emit)
		}
	case *ast.ForStmt:
		a.walkStmt(s.Body, guards, emit)
	case *ast.RangeStmt:
		a.walkStmt(s.Body, guards, emit)
	case *ast.SwitchStmt:
		// タグなし switch は if / else if と同義なので、先行 case の否定を積む。
		// タグ付きは値で排他になるので不要。
		var prior []Guard
		for _, c := range s.Body.List {
			cc, ok := c.(*ast.CaseClause)
			if !ok {
				continue
			}
			g := cloneGuards(guards)
			if len(cc.List) == 0 {
				// default は「どの case にも当たらなかった場合」。
				// タグの有無にかかわらず、先行 case の否定がその条件になる。
				a.walkStmts(cc.Body, append(g, prior...), emit)
				continue
			}
			// タグなし switch は if / else if と同義なので、先行 case の否定も
			// 積む。タグ付きは値で排他になるので case の条件だけでよい。
			if s.Tag == nil {
				g = append(g, prior...)
			}
			a.walkStmts(cc.Body, append(g, a.switchGuard(s, cc)), emit)
			prior = append(prior, a.switchNegGuard(s, cc))
		}
	case *ast.TypeSwitchStmt:
		subject := a.typeSwitchSubject(s)
		for _, c := range s.Body.List {
			cc, ok := c.(*ast.CaseClause)
			if !ok {
				continue
			}
			label := a.caseLabel(nil, cc)
			a.walkStmts(cc.Body, append(cloneGuards(guards), Guard{
				Text:     label,
				Origin:   s.Pos(),
				Subject:  subject,
				IsSwitch: true,
				Arm:      label,
			}), emit)
		}
	case *ast.LabeledStmt:
		a.walkStmt(s.Stmt, guards, emit)
	case *ast.SelectStmt:
		for _, c := range s.Body.List {
			if cc, ok := c.(*ast.CommClause); ok {
				a.walkStmts(cc.Body, guards, emit)
			}
		}
	}
}

func (a *analyzer) caseLabel(tag ast.Expr, cc *ast.CaseClause) string {
	if len(cc.List) == 0 {
		return "default"
	}
	parts := make([]string, len(cc.List))
	for i, e := range cc.List {
		parts[i] = a.src(e)
	}
	joined := strings.Join(parts, ", ")
	if tag != nil {
		return a.src(tag) + " == " + joined
	}
	return joined
}

// joinConds は条件を連言として 1 つの文字列にする。接続語はここだけに置く。
func joinConds(conds []string) string {
	return strings.Join(conds, " かつ ")
}

// guardsLabel は Guard の列を状態遷移図のラベルにする。
func guardsLabel(gs []Guard) string {
	if len(gs) == 0 {
		return "それ以外"
	}
	conds := make([]string, len(gs))
	for i, g := range gs {
		conds[i] = g.Text
	}
	return joinConds(conds)
}

func cloneGuards(g []Guard) []Guard {
	out := make([]Guard, len(g), len(g)+1)
	copy(out, g)
	return out
}

func (a *analyzer) addEdge(from *Node, r returnSite) {
	if r.typ == nil {
		a.warnf("%s の return が静的に解決できない: %s", from.Type, r.unknown)
		return
	}
	to := a.stateNode(r.typ, r.lit)
	label := guardsLabel(r.guards)
	for _, e := range from.Edges {
		if e.To == to.ID && guardsLabel(e.Guards) == label {
			return
		}
	}
	from.Edges = append(from.Edges, Edge{To: to.ID, Guards: r.guards})
}

// needNode は Need 状態のノードを返す。型ごとに 1 つ。
func (a *analyzer) needNode(named *types.Named) *Node {
	key := "need:" + named.Obj().Name()
	if n, ok := a.nodes[key]; ok {
		return n
	}
	a.seenStates[named.Obj().Name()] = true
	n := &Node{
		ID:     fmt.Sprintf("n%d", len(a.order)),
		Type:   a.shortName(named.Obj().Name()),
		Fields: a.stateFields(named),
		Params: a.decideParams(named),
		Kind:   KindNeed,
	}
	a.nodes[key] = n
	a.order = append(a.order, key)
	return n
}

// stateNode は遷移先のノードを返す。終端はリテラルの中身ごとに分ける。
func (a *analyzer) stateNode(named *types.Named, lit *ast.CompositeLit) *Node {
	if a.decideMethod(named) != nil {
		return a.needNode(named)
	}
	lines := a.literalLines(lit)
	key := "end:" + named.Obj().Name() + "{" + strings.Join(lines, ", ") + "}"
	if n, ok := a.nodes[key]; ok {
		return n
	}
	a.seenStates[named.Obj().Name()] = true
	n := &Node{
		ID:     fmt.Sprintf("n%d", len(a.order)),
		Type:   a.shortName(named.Obj().Name()),
		Fields: lines,
		Params: nil,
		Kind:   KindTerminal,
	}
	a.nodes[key] = n
	a.order = append(a.order, key)
	return n
}

// literalLines は終端リテラルのフィールドを "名前: 値" で 1 行ずつ返す。
// ゼロ値のフィールドは書かれないので、結末の違いだけが残る。
//
// 埋め込んだ事実の struct は除く。どの終端にも同じ形で運ばれるだけで結末を分けず、
// 載せるとラベルが事実の受け渡しで埋まる。
func (a *analyzer) literalLines(lit *ast.CompositeLit) []string {
	if lit == nil {
		return nil
	}
	var parts []string
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			parts = append(parts, a.src(elt))
			continue
		}
		if a.isEmbeddedField(lit, kv.Key) {
			continue
		}
		parts = append(parts, a.src(kv.Key)+": "+a.src(kv.Value))
	}
	return parts
}

func (a *analyzer) isEmbeddedField(lit *ast.CompositeLit, key ast.Expr) bool {
	ident, ok := key.(*ast.Ident)
	if !ok {
		return false
	}
	typ := a.pkg.TypesInfo.TypeOf(lit)
	if typ == nil {
		return false
	}
	st, ok := typ.Underlying().(*types.Struct)
	if !ok {
		return false
	}
	for i := range st.NumFields() {
		f := st.Field(i)
		if f.Name() == ident.Name {
			return f.Embedded()
		}
	}
	return false
}

// stateFields は状態が直接持つフィールドを 1 行ずつ返す。
//
// 埋め込んだ事実の struct は中を展開せず名前だけ出す。展開すると、事実を積み上げる
// Decision でノードが際限なく縦に伸びるうえ、同じ事実が全状態に繰り返し並ぶ。
// 名前が出ていれば、何を持ち回っているかは型を引けば分かる。
func (a *analyzer) stateFields(named *types.Named) []string {
	st, ok := named.Underlying().(*types.Struct)
	if !ok {
		return nil
	}
	lines := make([]string, 0, st.NumFields())
	for i := range st.NumFields() {
		f := st.Field(i)
		if f.Embedded() {
			lines = append(lines, f.Name())
			continue
		}
		lines = append(lines, f.Name()+" "+types.TypeString(f.Type(), a.qualifier))
	}
	return lines
}

// decideParams は Decide の引数を "名前 型" で 1 行ずつ返す。
// その状態で何を渡す必要があるかを示す。
//
// 型だけでは読めない。now / gracePeriod のように同じ型が並ぶ引数では、
// 意味を持っているのは名前のほうで、型は取得先の手がかりにしかならない。
func (a *analyzer) decideParams(named *types.Named) []string {
	for i := range named.NumMethods() {
		m := named.Method(i)
		if m.Name() != "Decide" {
			continue
		}
		sig, ok := m.Type().(*types.Signature)
		if !ok {
			return nil
		}
		params := sig.Params()
		lines := make([]string, 0, params.Len())
		for j := range params.Len() {
			p := params.At(j)
			typ := types.TypeString(p.Type(), a.qualifier)
			if name := p.Name(); name != "" && name != "_" {
				lines = append(lines, name+" "+typ)
				continue
			}
			lines = append(lines, typ)
		}
		return lines
	}
	return nil
}

// findEntries は interface を返す非メソッド関数を開始点として集める。
func (a *analyzer) findEntries() []Entry {
	var fns []*types.Func
	for fn := range a.plainFn {
		sig, ok := fn.Type().(*types.Signature)
		if !ok || sig.Results().Len() == 0 {
			continue
		}
		if sig.Results().At(0).Type() != a.ifaceObj.Type() {
			continue
		}
		fns = append(fns, fn)
	}
	sort.Slice(fns, func(i, j int) bool { return fns[i].Name() < fns[j].Name() })
	type candidate struct {
		fn    *types.Func
		entry Entry
	}
	candidates := make([]candidate, 0, len(fns))
	for _, fn := range fns {
		entry := Entry{Func: fn.Name()}
		for _, r := range a.collectReturns(a.plainFn[fn], nil, map[*types.Func]bool{}) {
			if r.typ == nil {
				a.warnf("%s の return が静的に解決できない: %s", fn.Name(), r.unknown)
				continue
			}
			entry.Edges = append(entry.Edges, Edge{
				To:     a.stateNode(r.typ, r.lit).ID,
				Guards: r.guards,
			})
		}
		if len(entry.Edges) > 0 {
			candidates = append(candidates, candidate{fn: fn, entry: entry})
		}
	}
	// 遷移の途中で中身を辿った関数は状態を返すヘルパーであって開始点ではない。
	// 名前で判定すると、コンストラクタの命名から外れた瞬間に開始点が消える。
	// inlined が埋まるのは上の走査中なので、ふるいにかけるのは走査を終えてから。
	// 先に見ると、あとで辿られたヘルパーが開始点として残ってしまう。
	entries := make([]Entry, 0, len(candidates))
	for _, c := range candidates {
		if a.inlined[c.fn] {
			continue
		}
		entries = append(entries, c.entry)
	}
	return entries
}

// checkMissingStates は図に 1 度も現れない状態を報告する。
//
// 遷移メソッドを持たず、どこからも返されない状態はノードにすらならないので、
// グラフの到達可能性では見つからない。遷移メソッドの名前が Decide でない場合も
// ここに落ちる (状態としては存在するが終端扱いになり、誰も返さない)。
func (a *analyzer) checkMissingStates(states []*types.Named) {
	for _, st := range states {
		name := st.Obj().Name()
		if a.seenStates[name] {
			continue
		}
		if m := a.transitionLikeMethod(st); m != "" {
			a.warnf("%s が図に現れない。%s が遷移メソッドに見えるが、解析は Decide という名前だけを見る",
				name, m)
			continue
		}
		a.warnf("%s が図に現れない。どこからも返されず、Decide も持たない", name)
	}
}

// transitionLikeMethod は Decide 以外で interface を返すメソッドの名前を返す。
// 遷移メソッドの改名を「到達しない状態」ではなく改名として報告するために使う。
func (a *analyzer) transitionLikeMethod(named *types.Named) string {
	for i := range named.NumMethods() {
		m := named.Method(i)
		if m.Name() == "Decide" {
			continue
		}
		sig, ok := m.Type().(*types.Signature)
		if !ok || sig.Results().Len() == 0 {
			continue
		}
		if a.isDecisionType(sig.Results().At(0).Type()) {
			return m.Name()
		}
	}
	return ""
}

// checkUnreachable は開始点からどの経路でも到達しない状態を報告する。
// go-check-sumtype は interpreter の switch の網羅しか見ないので、
// 「case は書いてあるが実際には到達しない状態」はここでしか分からない。
func (a *analyzer) checkUnreachable(d *Decision) {
	reachable := map[string]bool{}
	byID := map[string]*Node{}
	for _, n := range d.Nodes {
		byID[n.ID] = n
	}
	var visit func(id string)
	visit = func(id string) {
		if reachable[id] {
			return
		}
		reachable[id] = true
		for _, e := range byID[id].Edges {
			visit(e.To)
		}
	}
	for _, entry := range d.Entries {
		for _, e := range entry.Edges {
			visit(e.To)
		}
	}
	for _, n := range d.Nodes {
		if !reachable[n.ID] {
			a.warnf("%s はどの経路からも到達しない", n.Type)
		}
	}
}

func (a *analyzer) shortName(name string) string {
	if a.prefix != "" && name != a.prefix {
		// 語の途中で切れていないことを確かめる。interface が D なら Denied が
		// enied に、PublishDecision なら Publishing が ing になってしまう。
		if trimmed := strings.TrimPrefix(name, a.prefix); trimmed != name && token.IsExported(trimmed) {
			return trimmed
		}
	}
	return name
}

func (a *analyzer) src(node ast.Node) string {
	if node == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, a.pkg.Fset, node); err != nil {
		return "?"
	}
	return strings.Join(strings.Fields(buf.String()), " ")
}

func (a *analyzer) warnf(format string, args ...any) {
	a.warns = append(a.warns, fmt.Sprintf(format, args...))
}

// qualifier は型名の修飾を短くする。完全なインポートパスを出すとラベルが型名より
// 長くなって図が読めない。解析中のパッケージの型は修飾しない。ソース上も
// パッケージ名を付けずに書くので、付けると実物と見た目がずれる。
func (a *analyzer) qualifier(p *types.Package) string {
	if a.pkg != nil && p == a.pkg.Types {
		return ""
	}
	return p.Name()
}

// firstDocLine は doc コメントの最初の段落を 1 行にして返す。
// 1 行目だけを取ると、折り返された説明が文の途中で切れる。
func firstDocLine(doc *ast.CommentGroup) string {
	if doc == nil {
		return ""
	}
	var lines []string
	for _, c := range doc.List {
		line := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
		if strings.HasPrefix(line, "go:") || strings.Contains(line, ":decl") {
			continue
		}
		if line == "" {
			if len(lines) > 0 {
				break
			}
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "")
}
