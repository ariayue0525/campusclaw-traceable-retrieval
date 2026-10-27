package retrieval

import (
	"testing"

	"campusclaw/backend/internal/model"
)

func TestRouteSkill(t *testing.T){
	skills:=[]model.Skill{
		{ID:9,TriggerText:"方程,面积",Enabled:true},
		{ID:3,TriggerText:"方程、面积",Enabled:true},
		{ID:1,TriggerText:"方程,面积,步骤",Enabled:false},
		{ID:2,TriggerText:"英文",Enabled:true},
	}
	if got:=RouteSkill("请看这个方程的面积",skills);got==nil||got.ID!=3{t.Fatalf("tie route: %+v",got)}
	if got:=RouteSkill("这个 ENgLiSh 题",[]model.Skill{{ID:2,TriggerText:"english",Enabled:true}});got==nil||got.ID!=2{t.Fatalf("case folding: %+v",got)}
	if got:=RouteSkill("完全不相干",skills);got!=nil{t.Fatalf("zero score selected %+v",got)}
}
