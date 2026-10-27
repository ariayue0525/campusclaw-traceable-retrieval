package skillfile

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"campusclaw/backend/internal/model"
)

var ErrInvalid = errors.New("技能文件格式不正确")
var externalToolName = regexp.MustCompile(`^[a-z][a-z0-9_]*@[a-z][a-z0-9-]*$`)

// Parse accepts a UTF-8 metadata block followed by one blank line and the
// instructional body. Unknown or repeated metadata is an error, not ignored.
func Parse(source string) (model.Skill, error) {
	var out model.Skill
	if !utf8.ValidString(source) || utf8.RuneCountInString(source) > 8000 {
		return out, ErrInvalid
	}
	text := strings.ReplaceAll(strings.ReplaceAll(source, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(text, "\n")
	fields := map[string]string{}
	end := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			end = i
			break
		}
		key, value, ok := strings.Cut(line, "：")
		if !ok {
			key, value, ok = strings.Cut(line, ":")
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || value == "" || !allowed[key] || fields[key] != "" {
			return out, ErrInvalid
		}
		fields[key] = value
	}
	if end < 0 || fields["名称"] == "" || fields["适用条件"] == "" || fields["启用"] == "" {
		return out, ErrInvalid
	}
	body := strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	if body == "" || utf8.RuneCountInString(fields["名称"]) > 32 ||
		utf8.RuneCountInString(fields["适用条件"]) > 120 ||
		utf8.RuneCountInString(fields["资料"]) > 32 ||
		utf8.RuneCountInString(body) > 2000 {
		return out, ErrInvalid
	}
	enabled, ok := parseEnabled(fields["启用"])
	if !ok { return out, ErrInvalid }
	tools := []string{}
	if fields["工具"] != "" {
		seen:=map[string]bool{}
		for _, item := range splitList(fields["工具"]) {
			item=strings.TrimSpace(item)
			if (item!="knowledge_search"&&!externalToolName.MatchString(item))||seen[item]{return out,ErrInvalid}
			seen[item]=true
			tools = append(tools, item)
		}
		if len(tools) == 0 { return out, ErrInvalid }
	}
	out = model.Skill{Name: fields["名称"], TriggerText: fields["适用条件"], SubjectText: fields["资料"], Description: body, Tools: tools, SourceText: source, Enabled: enabled}
	return out, nil
}

var allowed = map[string]bool{"名称":true, "适用条件":true, "资料":true, "工具":true, "启用":true}

func parseEnabled(value string) (bool, bool) {
	switch strings.ToLower(value) {
	case "是", "true", "启用": return true, true
	case "否", "false", "停用": return false, true
	default: return false, false
	}
}

func splitList(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '，' || r == '、' || r == ';' || r == '；' || r == '|' })
}
