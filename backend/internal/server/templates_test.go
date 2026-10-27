package server

import (
	"strings"
	"testing"
)

func TestValidateTemplate(t *testing.T) {
	if err:=validateTemplate("辅导", "帮助 {{student_name}} 理解 {{class_name}} 的课程");err!=nil{t.Fatal(err)}
	for _,content:=range []string{"",strings.Repeat("字",2001),"{{api_key}}","{{student_name","}}"} {
		if err:=validateTemplate("辅导",content);err==nil{t.Fatalf("accepted invalid template %q",content)}
	}
	if err:=validateTemplate(strings.Repeat("字",65),"正文");err==nil{t.Fatal("accepted long name")}
}
