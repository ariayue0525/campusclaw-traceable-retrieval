package skillfile

import (
	"strings"
	"testing"
)

const valid = "名称：解题引导\n适用条件：方程,应用题\n资料：数学\n工具：knowledge_search\n启用：是\n\n先追问学生的思路，不直接给最终答案。"

func TestParse(t *testing.T) {
	s, err := Parse(valid)
	if err != nil { t.Fatal(err) }
	if s.Name != "解题引导" || len(s.Tools) != 1 || s.Tools[0] != "knowledge_search" || !s.Enabled || s.SourceText != valid { t.Fatalf("unexpected skill: %+v", s) }
	withExternal:=strings.Replace(valid,"knowledge_search","knowledge_search,formula_hint@campusclaw",1)
	if selected,err:=Parse(withExternal);err!=nil||len(selected.Tools)!=2{t.Fatalf("external tool syntax: %+v %v",selected,err)}
	for name, source := range map[string]string{
		"duplicate": strings.Replace(valid, "启用：是", "名称：重复\n启用：是", 1),
		"unknown": strings.Replace(valid, "启用：是", "权限：管理员\n启用：是", 1),
		"missing": strings.Replace(valid, "适用条件：方程,应用题\n", "", 1),
		"tool": strings.Replace(valid, "knowledge_search", "shell_exec", 1),
		"oversize file": valid + strings.Repeat("字", 8000),
		"oversize body": strings.Split(valid, "\n\n")[0] + "\n\n" + strings.Repeat("字", 2001),
		"no body": strings.Split(valid, "\n\n")[0] + "\n\n",
		"invalid enabled": strings.Replace(valid, "启用：是", "启用：也许", 1),
	} {
		t.Run(name, func(t *testing.T) { if _, err := Parse(source); err == nil { t.Fatal("expected invalid source") } })
	}
}
