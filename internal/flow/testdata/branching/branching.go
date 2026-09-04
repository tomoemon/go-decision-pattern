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
