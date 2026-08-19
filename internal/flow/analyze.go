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

// Edge は状態遷移。Label は遷移を発生させた分岐条件のソース。
type Edge struct {
	To    string
	Label string
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
	guards  []string
	unknown string
}

// collectReturns は Decide の本体から return を集める。
//
// return が同じパッケージの関数呼び出しになっている場合はその関数の中まで辿る。
// 辿らないと「ヘルパーに切り出した分岐」が図から丸ごと消える。
func (a *analyzer) collectReturns(fd *ast.FuncDecl, guards []string, seen map[*types.Func]bool) []returnSite {
	if fd == nil || fd.Body == nil {
		return nil
	}
	sig := a.signatureOf(fd)
	var out []returnSite
	a.walkStmts(fd.Body.List, guards, func(ret *ast.ReturnStmt, g []string) {
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
func (a *analyzer) resolveReturnStmt(ret *ast.ReturnStmt, sig *types.Signature, guards []string, seen map[*types.Func]bool) []returnSite {
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

func (a *analyzer) resolveReturn(expr ast.Expr, guards []string, seen map[*types.Func]bool) []returnSite {
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
func (a *analyzer) walkStmts(stmts []ast.Stmt, guards []string, emit func(*ast.ReturnStmt, []string)) {
	for _, stmt := range stmts {
		a.walkStmt(stmt, guards, emit)
	}
}

func (a *analyzer) walkStmt(stmt ast.Stmt, guards []string, emit func(*ast.ReturnStmt, []string)) {
	switch s := stmt.(type) {
	case *ast.ReturnStmt:
		emit(s, guards)
	case *ast.BlockStmt:
		a.walkStmts(s.List, guards, emit)
	case *ast.IfStmt:
		cond := a.src(s.Cond)
		// 初期化付きの if は条件だけ出すと "ok" のような無意味なラベルになる。
		// 何を評価した ok なのかは初期化節にしか書かれていない。
		if s.Init != nil {
			cond = a.src(s.Init) + "; " + cond
		}
		a.walkStmts(s.Body.List, append(cloneGuards(guards), cond), emit)
		if s.Else != nil {
			a.walkStmt(s.Else, append(cloneGuards(guards), "!("+cond+")"), emit)
		}
	case *ast.ForStmt:
		a.walkStmt(s.Body, guards, emit)
	case *ast.RangeStmt:
		a.walkStmt(s.Body, guards, emit)
	case *ast.SwitchStmt:
		for _, c := range s.Body.List {
			cc, ok := c.(*ast.CaseClause)
			if !ok {
				continue
			}
			a.walkStmts(cc.Body, append(cloneGuards(guards), a.caseLabel(s.Tag, cc)), emit)
		}
	case *ast.TypeSwitchStmt:
		for _, c := range s.Body.List {
			cc, ok := c.(*ast.CaseClause)
			if !ok {
				continue
			}
			a.walkStmts(cc.Body, append(cloneGuards(guards), a.caseLabel(nil, cc)), emit)
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

func cloneGuards(g []string) []string {
	out := make([]string, len(g), len(g)+1)
	copy(out, g)
	return out
}

func (a *analyzer) addEdge(from *Node, r returnSite) {
	label := strings.Join(r.guards, " かつ ")
	if label == "" {
		label = "それ以外"
	}
	if r.typ == nil {
		a.warnf("%s の return が静的に解決できない: %s", from.Type, r.unknown)
		return
	}
	to := a.stateNode(r.typ, r.lit)
	for _, e := range from.Edges {
		if e.To == to.ID && e.Label == label {
			return
		}
	}
	from.Edges = append(from.Edges, Edge{To: to.ID, Label: label})
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
	var entries []Entry
	var fns []*types.Func
	for fn := range a.plainFn {
		sig, ok := fn.Type().(*types.Signature)
		if !ok || sig.Results().Len() == 0 {
			continue
		}
		if sig.Results().At(0).Type() != a.ifaceObj.Type() {
			continue
		}
		// 遷移の途中で中身を辿った関数は状態を返すヘルパーであって開始点ではない。
		// 名前で判定すると、コンストラクタの命名から外れた瞬間に開始点が消える。
		if a.inlined[fn] {
			continue
		}
		fns = append(fns, fn)
	}
	sort.Slice(fns, func(i, j int) bool { return fns[i].Name() < fns[j].Name() })
	for _, fn := range fns {
		entry := Entry{Func: fn.Name()}
		for _, r := range a.collectReturns(a.plainFn[fn], nil, map[*types.Func]bool{}) {
			if r.typ == nil {
				a.warnf("%s の return が静的に解決できない: %s", fn.Name(), r.unknown)
				continue
			}
			label := strings.Join(r.guards, " かつ ")
			if label == "" {
				label = "それ以外"
			}
			entry.Edges = append(entry.Edges, Edge{To: a.stateNode(r.typ, r.lit).ID, Label: label})
		}
		if len(entry.Edges) > 0 {
			entries = append(entries, entry)
		}
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
		if trimmed := strings.TrimPrefix(name, a.prefix); trimmed != "" && trimmed != name {
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
