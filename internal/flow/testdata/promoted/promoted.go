// Package promoted は、終端に埋め込んだ事実をどの書き方で渡しても
// ラベルに載らないことを確かめるための例。
package promoted

//decision:decl
//sumtype:decl
type PromotedDecision interface{ isPromotedDecision() }

type promotedFactsUser struct{ UserID string }

type PromotedNeedUser struct{ UserID string }

type PromotedDecided struct {
	promotedFactsUser
	Admin bool
}

func (PromotedNeedUser) isPromotedDecision() {}
func (PromotedDecided) isPromotedDecision()  {}

func NewPromotedDecision(userID string) PromotedDecision {
	return PromotedNeedUser{UserID: userID}
}

func (s PromotedNeedUser) Decide(admin, nested bool) PromotedDecision {
	if admin {
		// Go 1.27 から書ける昇格したフィールド
		return PromotedDecided{UserID: s.UserID, Admin: true}
	}
	if nested {
		return PromotedDecided{promotedFactsUser: promotedFactsUser{UserID: s.UserID}, Admin: false}
	}
	return PromotedDecided{UserID: "guest", Admin: false}
}
