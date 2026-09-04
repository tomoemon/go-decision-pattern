// Package sharedhelper は、引数を取るヘルパーを複数の経路から呼ぶ例。
package sharedhelper

//decision:decl
//sumtype:decl
type ShareDecision interface{ isShareDecision() }

type ShareDecided struct{ Reason string }
type ShareNeedX struct{ shareFacts }

func (ShareDecided) isShareDecision() {}
func (ShareNeedX) isShareDecision()   {}

type shareFacts struct{ N int }

// byLimit は上限との比較だけを行うヘルパー。呼び出しごとに limit が変わる。
func byLimit(n, limit int) ShareDecision {
	if n > limit {
		return ShareDecided{Reason: "over"}
	}
	return ShareNeedX{shareFacts{N: n}}
}

func NewShareA(n int) ShareDecision {
	if n < 0 {
		return ShareDecided{Reason: "negative"}
	}
	return byLimit(n, 10)
}

func NewShareB(n int) ShareDecision {
	if n == 0 {
		return ShareDecided{Reason: "zero"}
	}
	return byLimit(n, 100)
}

func (s ShareNeedX) Decide(ok bool) ShareDecision {
	if ok {
		return ShareDecided{Reason: "ok"}
	}
	return ShareDecided{Reason: "ng"}
}
