// Package fallthru は、構文を抜けた先へ複数の経路が合流する形の例。
//
// 抜けた先に着く条件をどこまで出せるかは、合流の仕方で変わる。case どうしは
// 排他なので「脱出する case のどれにも当たらなかった」で言い切れるが、
// if 連鎖は順序を持つので、素通りする枝より後ろの否定は積めない。
package fallthru

//decision:decl
//sumtype:decl
type FallDecision interface{ isFallDecision() }

type FallDecided struct{ Reason string }
type FallNeedLow struct{ fallFacts }
type FallNeedHigh struct{ fallFacts }

func (FallDecided) isFallDecision()  {}
func (FallNeedLow) isFallDecision()  {}
func (FallNeedHigh) isFallDecision() {}

type fallFacts struct{ Value int }

type kind int

const (
	kindA kind = iota
	kindB
)

// NewFallEmptyCase は本体が空の case。switch を抜けて末尾へ進む。
func NewFallEmptyCase(k kind, v int) FallDecision {
	f := fallFacts{Value: v}
	switch k {
	case kindA:
	case kindB:
		return FallNeedHigh{f}
	}
	return FallNeedLow{f}
}

// NewFallWorkCase は return せずに処理だけして抜ける case。
func NewFallWorkCase(k kind, v int) FallDecision {
	f := fallFacts{Value: v}
	switch k {
	case kindA:
		f.Value = -v
	case kindB:
		return FallNeedHigh{f}
	}
	return FallNeedLow{f}
}

// NewFallManyArms は脱出する case が複数ある場合。抜けた先にはその全部の
// 否定が積まれ、素通りする case の否定は積まれない。
func NewFallManyArms(k kind, v int) FallDecision {
	f := fallFacts{Value: v}
	switch k {
	case kindA:
	case kindB:
		return FallNeedHigh{f}
	case kind(2):
		return FallDecided{Reason: "two"}
	}
	return FallNeedLow{f}
}

// NewFallPartialReturn は case の中で一部だけ return する形。抜けた先に着くには
// case の中の分岐も要るので、連言では表せない。条件は出さない。
func NewFallPartialReturn(k kind, v int) FallDecision {
	f := fallFacts{Value: v}
	switch k {
	case kindA:
		if v < 0 {
			return FallDecided{Reason: "negative"}
		}
	case kindB:
		return FallNeedHigh{f}
	}
	return FallNeedLow{f}
}

// NewFallTagless はタグなし switch で素通りする case がある形。
// 枝が順序を持つので条件は出せない。
func NewFallTagless(v int) FallDecision {
	f := fallFacts{Value: v}
	switch {
	case v < 0:
	case v == 0:
		return FallNeedHigh{f}
	}
	return FallNeedLow{f}
}

// NewFallIfChain は if 連鎖の後ろの枝が素通りする形。枝が順序を持つので
// 条件は出せない。ifFallthroughGuards は素通りの枝に当たった時点で、
// それまで貯めた否定ごと捨てる。
func NewFallIfChain(v int) FallDecision {
	f := fallFacts{Value: v}
	if v < 0 {
		return FallDecided{Reason: "negative"}
	} else if v == 0 {
		f.Value = 1
	}
	return FallNeedLow{f}
}

// NewFallThrough は fallthrough で次の case へ落ちる形。switch の先へは進まない
// ので、この case の否定を落としてはいけない。落とすと「kindA なら Low」という
// 嘘の条件が出る。
func NewFallThrough(k kind, v int) FallDecision {
	f := fallFacts{Value: v}
	switch k {
	case kindA:
		fallthrough
	case kindB:
		return FallNeedHigh{f}
	}
	return FallNeedLow{f}
}

// NewFallPanicInCase は case の中で panic する形。抜けた先へ進むとは限らない。
func NewFallPanicInCase(k kind, v int) FallDecision {
	f := fallFacts{Value: v}
	switch k {
	case kindA:
		if v < 0 {
			panic("negative")
		}
	case kindB:
		return FallNeedHigh{f}
	}
	return FallNeedLow{f}
}

// NewFallNonConstCase は case 式が定数でない形。実行時に重なりうるので上から順に
// 評価され、タグ付きでも排他にならない。
func NewFallNonConstCase(k, a, b kind, v int) FallDecision {
	f := fallFacts{Value: v}
	switch k {
	case a:
	case b:
		return FallNeedHigh{f}
	}
	return FallNeedLow{f}
}

// NewFallLoopInCase は case の中に抜けない for がある形。先へ進むとは限らない。
func NewFallLoopInCase(k kind, v int) FallDecision {
	f := fallFacts{Value: v}
	switch k {
	case kindA:
		for {
			f.Value++
		}
	case kindB:
		return FallNeedHigh{f}
	}
	return FallNeedLow{f}
}
