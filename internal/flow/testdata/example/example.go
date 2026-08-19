// Package example は decision-flow の解析対象の見本。テストからのみ使う。
package example

// ExampleDecision は解析対象。
//
//sumtype:decl
//decision:decl
type ExampleDecision interface {
	isExampleDecision()
}

// OtherDecision は decision:decl が付いていない sum type。対象外になることを確かめる。
//
//sumtype:decl
type OtherDecision interface {
	isOtherDecision()
}

type OtherOnly struct{}

func (OtherOnly) isOtherDecision() {}

type exampleFacts struct {
	UserID string
}

// ExampleDecided は終端。
type ExampleDecided struct {
	exampleFacts
	Reason string
}

func (ExampleDecided) isExampleDecision() {}

// ExampleFailed は失敗の終端。
type ExampleFailed struct {
	Err error
}

func (ExampleFailed) isExampleDecision() {}

// ExampleUnreachable はどこからも返されない状態。警告の対象になる。
type ExampleUnreachable struct{}

func (ExampleUnreachable) isExampleDecision() {}

func (ExampleUnreachable) Decide(_ bool) ExampleDecision {
	return ExampleDecided{Reason: "unreachable"}
}

func NewExampleDecision(id string) ExampleDecision {
	return ExampleNeedUser{UserID: id}
}

type ExampleNeedUser struct {
	UserID string
}

func (ExampleNeedUser) isExampleDecision() {}

func (s ExampleNeedUser) Decide(deleted bool) ExampleDecision {
	if deleted {
		return ExampleFailed{Err: errDeleted}
	}
	return ExampleNeedBlock{exampleFacts: exampleFacts{UserID: s.UserID}}
}

type ExampleNeedBlock struct {
	exampleFacts
}

func (ExampleNeedBlock) isExampleDecision() {}

// Decide は分岐をヘルパーに切り出している。中まで辿れることを確かめる。
func (s ExampleNeedBlock) Decide(blocked bool, count int) ExampleDecision {
	if blocked {
		return ExampleFailed{Err: errBlocked}
	}
	return s.byCount(count)
}

func (s ExampleNeedBlock) byCount(count int) ExampleDecision {
	if count == 0 {
		return ExampleDecided{exampleFacts: s.exampleFacts, Reason: "empty"}
	}
	return ExampleDecided{exampleFacts: s.exampleFacts, Reason: "ok"}
}

var (
	errDeleted = newErr("deleted")
	errBlocked = newErr("blocked")
)

type simpleErr string

func (e simpleErr) Error() string { return string(e) }

func newErr(s string) error { return simpleErr(s) }
