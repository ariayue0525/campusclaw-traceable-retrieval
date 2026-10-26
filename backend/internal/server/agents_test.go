package server

import (
	"strings"
	"testing"
)

func TestValidateAgentRequestLimitsAndDuplicateBindings(t *testing.T) {
	valid := agentRequest{Name: "解题助手", Prompt: "耐心引导", Enabled: true, SkillIDs: []int64{1, 2}, MaterialIDs: []int64{3}}
	if err := validateAgentRequest(valid); err != nil {
		t.Fatal(err)
	}
	duplicate := valid
	duplicate.SkillIDs = []int64{1, 1}
	if err := validateAgentRequest(duplicate); err == nil || !strings.Contains(err.Error(), "不能重复") {
		t.Fatalf("duplicate IDs must be rejected explicitly: %v", err)
	}
	tooLong := valid
	tooLong.Name = strings.Repeat("字", 65)
	if err := validateAgentRequest(tooLong); err == nil {
		t.Fatal("65-rune name must be rejected")
	}
	tooLong = valid
	tooLong.Prompt = strings.Repeat("字", 2001)
	if err := validateAgentRequest(tooLong); err == nil {
		t.Fatal("2001-rune prompt must be rejected")
	}
}
