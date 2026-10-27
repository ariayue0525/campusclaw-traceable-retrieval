package retrieval

import (
	"strings"

	"campusclaw/backend/internal/model"
)

// RouteSkill expects only currently bound, enabled skills from the store.
// A zero score intentionally returns nil; no skill is activated by default.
func RouteSkill(question string,skills []model.Skill)*model.Skill{
	question=strings.ToLower(question)
	var selected *model.Skill
	best:=0
	for i:=range skills{
		if !skills[i].Enabled{continue}
		score:=0
		for _,word:=range strings.FieldsFunc(skills[i].TriggerText,func(r rune)bool{return r==','||r=='，'||r=='、'||r==';'||r=='；'||r=='|'}){
			word=strings.ToLower(strings.TrimSpace(word))
			if word!=""&&strings.Contains(question,word){score++}
		}
		if score>best||(score>0&&score==best&&(selected==nil||skills[i].ID<selected.ID)){
			best=score;selected=&skills[i]
		}
	}
	return selected
}
