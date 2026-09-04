// Package shortname は、interface 名が Decision で終わらない場合に
// 状態名が壊れないことを確かめるための例。
package shortname

//decision:decl
//sumtype:decl
type D interface{ isD() }

type Denied struct{ Reason string }
type Allowed struct{}
type NeedCheck struct{ facts }

func (Denied) isD()    {}
func (Allowed) isD()   {}
func (NeedCheck) isD() {}

type facts struct{ ID string }

func NewD(id string) D {
	return NeedCheck{facts{ID: id}}
}

func (s NeedCheck) Decide(ok bool) D {
	if !ok {
		return Denied{Reason: "ng"}
	}
	return Allowed{}
}
