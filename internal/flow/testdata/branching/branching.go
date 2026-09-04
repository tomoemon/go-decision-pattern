// Package branching は、同じ分岐を違う構文で書いても同じ図になることを確かめるための例。
package branching

//decision:decl
//sumtype:decl
type BranchDecision interface{ isBranchDecision() }

type BranchDecided struct{ Reason string }
type BranchNeedLow struct{ branchFacts }
type BranchNeedHigh struct{ branchFacts }

func (BranchDecided) isBranchDecision()  {}
func (BranchNeedLow) isBranchDecision()  {}
func (BranchNeedHigh) isBranchDecision() {}

type branchFacts struct{ Value int }

// NewBranchElseIf は if / else if / else で書いた版。
func NewBranchElseIf(v int) BranchDecision {
	f := branchFacts{Value: v}
	if v < 0 {
		return BranchDecided{Reason: "negative"}
	} else if v == 0 {
		return BranchNeedLow{f}
	} else {
		return BranchNeedHigh{f}
	}
}

// NewBranchEarlyReturn は early return を連ねた版。
func NewBranchEarlyReturn(v int) BranchDecision {
	f := branchFacts{Value: v}
	if v < 0 {
		return BranchDecided{Reason: "negative"}
	}
	if v == 0 {
		return BranchNeedLow{f}
	}
	return BranchNeedHigh{f}
}

// NewBranchElseIfNoElse は最後の else を書かず、続きを関数の末尾に置いた版。
func NewBranchElseIfNoElse(v int) BranchDecision {
	f := branchFacts{Value: v}
	if v < 0 {
		return BranchDecided{Reason: "negative"}
	} else if v == 0 {
		return BranchNeedLow{f}
	}
	return BranchNeedHigh{f}
}

// NewBranchSwitch はタグなし switch で書いた版。
func NewBranchSwitch(v int) BranchDecision {
	f := branchFacts{Value: v}
	switch {
	case v < 0:
		return BranchDecided{Reason: "negative"}
	case v == 0:
		return BranchNeedLow{f}
	default:
		return BranchNeedHigh{f}
	}
}

type kind string

const (
	kindA kind = "a"
	kindB kind = "b"
)

// NewBranchTagged はタグ付き switch。default を持たず、switch の後ろに return が続く。
func NewBranchTagged(k kind, v int) BranchDecision {
	f := branchFacts{Value: v}
	switch k {
	case kindA:
		return BranchNeedLow{f}
	case kindB:
		return BranchNeedHigh{f}
	}
	return BranchDecided{Reason: "unknown"}
}

// NewBranchBreakInCase は case を break で抜ける。break は switch を出るだけなので、
// switch の後ろへは kindA でも進む。全 case の否定は乗せられない。
func NewBranchBreakInCase(k kind, v int) BranchDecision {
	f := branchFacts{Value: v}
	switch k {
	case kindA:
		break
	case kindB:
		return BranchNeedHigh{f}
	}
	return BranchNeedLow{f}
}

// NewBranchNestedEq は括弧の内側に == がある条件。否定するのは外側の演算子だけ。
func NewBranchNestedEq(a, b, c bool, v int) BranchDecision {
	f := branchFacts{Value: v}
	if (a == b) != c {
		return BranchDecided{Reason: "mismatch"}
	}
	return BranchNeedLow{f}
}

func (s BranchNeedLow) Decide(ok bool) BranchDecision {
	if !ok {
		return BranchDecided{Reason: "low-ng"}
	}
	return BranchDecided{Reason: "low-ok"}
}

func (s BranchNeedHigh) Decide(ok bool) BranchDecision {
	if ok {
		return BranchDecided{Reason: "high-ok"}
	}
	return BranchDecided{Reason: "high-ng"}
}

// NewBranchNestedIf は、if の本体が if / else で終わる形。
// 内側で必ず脱出するので、後続の return には !(v < 0) が乗る。
func NewBranchNestedIf(v int, ok bool) BranchDecision {
	f := branchFacts{Value: v}
	if v < 0 {
		if ok {
			return BranchDecided{Reason: "neg-ok"}
		} else {
			return BranchDecided{Reason: "neg-ng"}
		}
	}
	return BranchNeedHigh{f}
}

// NewBranchNestedSwitch は、if の本体が default 付き switch で終わる形。
func NewBranchNestedSwitch(v int, k kind) BranchDecision {
	f := branchFacts{Value: v}
	if v < 0 {
		switch k {
		case kindA:
			return BranchDecided{Reason: "neg-a"}
		default:
			return BranchDecided{Reason: "neg-other"}
		}
	}
	return BranchNeedLow{f}
}

// NewBranchPanic は、if の本体が panic で終わる形。
// panic も脱出なので、後続の return には !(v < 0) が乗る。
func NewBranchPanic(v int) BranchDecision {
	if v < 0 {
		panic("negative")
	}
	return BranchNeedLow{branchFacts{Value: v}}
}
