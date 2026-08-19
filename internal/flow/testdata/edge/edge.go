// Package edge はルール上は書けるが解析が難しい形を集めた見本。テストからのみ使う。
package edge

//sumtype:decl
//decision:decl
type EdgeDecision interface {
	isEdgeDecision()
}

type EdgeDecided struct{ Reason string }

func (EdgeDecided) isEdgeDecision() {}

type EdgeFailed struct{ Err error }

func (EdgeFailed) isEdgeDecision() {}

func NewEdgeDecision() EdgeDecision {
	return EdgeNeedA{}
}

// (a) 非メソッドのヘルパーが interface を返す。開始点と紛れないか。
func pickTail(ok bool) EdgeDecision {
	if ok {
		return EdgeDecided{Reason: "tail-ok"}
	}
	return EdgeFailed{Err: nil}
}

type EdgeNeedA struct{}

func (EdgeNeedA) isEdgeDecision() {}

func (EdgeNeedA) Decide(ok bool) EdgeDecision {
	if !ok {
		return EdgeFailed{Err: nil}
	}
	return EdgeNeedB{}
}

// (b) Decide が error も返す。
type EdgeNeedB struct{}

func (EdgeNeedB) isEdgeDecision() {}

func (EdgeNeedB) Decide(n int) (EdgeDecision, error) {
	if n < 0 {
		return nil, errNegative
	}
	return EdgeNeedC{}, nil
}

// (c) リテラルではなく変数を返す。
type EdgeNeedC struct{}

func (EdgeNeedC) isEdgeDecision() {}

func (EdgeNeedC) Decide(hit bool) EdgeDecision {
	d := EdgeDecided{Reason: "from-var"}
	if hit {
		return d
	}
	return pickTail(hit)
}

// (d) 遷移メソッドの名前が Decide ではない。
type EdgeNeedD struct{}

func (EdgeNeedD) isEdgeDecision() {}

func (EdgeNeedD) Resolve(ok bool) EdgeDecision {
	if ok {
		return EdgeDecided{Reason: "resolved"}
	}
	return EdgeFailed{Err: nil}
}

var errNegative = simpleErr("negative")

type simpleErr string

func (e simpleErr) Error() string { return string(e) }
