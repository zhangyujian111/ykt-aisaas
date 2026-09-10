package persona

import "time"

// PersonaConfig Persona 模块配置。
type PersonaConfig struct {
	// DefaultPromptTemplate 默认 system prompt 模板（Go template 语法）。
	// 可用变量：{{.Name}}, {{.PersonalityTraits}}, {{.RelationshipStages}}, {{.SystemPrompt}}
	DefaultPromptTemplate string `json:"defaultPromptTemplate" yaml:"defaultPromptTemplate"`

	// CacheTTL persona 缓存时间（xiaozhi-server-go 端用，aisaas 端可忽略）。
	CacheTTL time.Duration `json:"cacheTTL" yaml:"cacheTTL"`

	// MaxBindPerDevice 单设备最大绑定数（默认 1）。
	MaxBindPerDevice int `json:"maxBindPerDevice" yaml:"maxBindPerDevice"`
}

// DefaultConfig 返回默认配置。
func DefaultConfig() PersonaConfig {
	return PersonaConfig{
		DefaultPromptTemplate: `{{.SystemPrompt}}

{{if .PersonalityTraits}}【性格特征】
{{.PersonalityTraits}}{{end}}
{{if .RelationshipStages}}【关系阶段】
{{.RelationshipStages}}{{end}}`,
		CacheTTL:          1 * time.Hour,
		MaxBindPerDevice:  1,
	}
}