package persona

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
)

// SystemPromptBuilder 将 Persona 转为 system prompt 字符串。
// 用于 xiaozhi-server-go 启动时拉取并注入到 LLM system prompt。
type SystemPromptBuilder struct {
	cfg PersonaConfig
}

// NewSystemPromptBuilder 构造。
func NewSystemPromptBuilder(cfg PersonaConfig) *SystemPromptBuilder {
	return &SystemPromptBuilder{cfg: cfg}
}

// Build 根据 Persona 生成 system prompt 字符串。
// 优先级：cfg.DefaultPromptTemplate > 内建模板。
func (b *SystemPromptBuilder) Build(ctx context.Context, persona *PersonaDO) (string, error) {
	tmplStr := b.cfg.DefaultPromptTemplate
	if tmplStr == "" {
		tmplStr = defaultTemplate()
	}

	tmpl, err := template.New("persona").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse persona prompt template: %w", err)
	}

	// 构造模板数据
	data := promptData{
		Name:         persona.Name,
		Code:         persona.Code,
		SystemPrompt: persona.SystemPrompt,
		PersonalityTraits: prettyJSON(persona.PersonalityTraits),
		RelationshipStages: prettyJSON(persona.RelationshipStages),
		Tags:              prettyJSON(persona.Tags),
		Description:       persona.Description,
	}
	if data.PersonalityTraits == "" {
		data.PersonalityTraits = ""
	}
	if data.RelationshipStages == "" {
		data.RelationshipStages = ""
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute persona prompt template: %w", err)
	}

	return strings.TrimSpace(buf.String()), nil
}

// ToSystemPrompt 便捷方法：将 PersonaDO 转为 system prompt（Service 层调用）。
func (s *Service) ToSystemPrompt(ctx context.Context, persona *PersonaDO) string {
	result, err := s.promptBuilder.Build(ctx, persona)
	if err != nil {
		// 降级：返回原始 systemPrompt
		return persona.SystemPrompt
	}
	return result
}

// promptData 模板渲染数据。
type promptData struct {
	Name               string
	Code               string
	SystemPrompt       string
	PersonalityTraits  string
	RelationshipStages string
	Tags               string
	Description        string
}

// defaultTemplate 内建 system prompt 模板。
func defaultTemplate() string {
	return `你是 {{.Name}}。

{{if .Description}}【角色描述】
{{.Description}}{{end}}

{{if .SystemPrompt}}【核心指令】
{{.SystemPrompt}}{{end}}

{{if .PersonalityTraits}}【性格特征】
{{.PersonalityTraits}}{{end}}

{{if .RelationshipStages}}【关系阶段】
{{.RelationshipStages}}{{end}}

{{if .Tags}}【标签】
{{.Tags}}{{end}}`
}

// prettyJSON 将 json.RawMessage 格式化为可读字符串。
func prettyJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// 尝试 pretty-print
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(raw)
	}
	return string(b)
}