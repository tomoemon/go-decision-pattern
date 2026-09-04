// Package typeswitch は型 switch を通る判定の例。
package typeswitch

//decision:decl
//sumtype:decl
type TypeDecision interface{ isTypeDecision() }

type TypeDecided struct{ Reason string }
type TypeNeedX struct{ typeFacts }

func (TypeDecided) isTypeDecision() {}
func (TypeNeedX) isTypeDecision()   {}

type typeFacts struct{ Kind string }

type shape interface{ isShape() }

type circle struct{}
type square struct{}

func (circle) isShape() {}
func (square) isShape() {}

// NewTypeDecision は型 switch で分岐する。default を持たないので、
// 抜けた先には「どの型でもなかった」という条件が乗る。
func NewTypeDecision(s shape) TypeDecision {
	switch s.(type) {
	case circle:
		return TypeDecided{Reason: "circle"}
	case square:
		return TypeNeedX{typeFacts{Kind: "square"}}
	}
	return TypeDecided{Reason: "unknown"}
}

func (s TypeNeedX) Decide(ok bool) TypeDecision {
	if !ok {
		return TypeDecided{Reason: "ng"}
	}
	return TypeDecided{Reason: "ok"}
}
