package audit

import "strings"

// sensitiveKeys 敏感字段名集合（脱敏规则：值为 [REDACTED]）。
var sensitiveKeys = map[string]bool{
	"password":   true,
	"apiKey":     true,
	"token":      true,
	"creditCard": true,
	"ssn":        true,
	"secret":     true,
	"privateKey": true,
	"accessKey":  true,
	"secretKey":  true,
	"passwd":     true,
	"pwd":        true,
	"authorization": true,
}

// redactValue 对 map 中敏感字段值替换为 [REDACTED]。
// 大小写不敏感匹配。
func redactValue(m map[string]any, extraKeys ...string) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		lower := strings.ToLower(k)
		if sensitiveKeys[lower] {
			out[k] = "[REDACTED]"
			continue
		}
		// 检查额外敏感键
		redacted := false
		for _, ek := range extraKeys {
			if strings.EqualFold(k, ek) {
				out[k] = "[REDACTED]"
				redacted = true
				break
			}
		}
		if !redacted {
			// 递归处理嵌套 map
			if nested, ok := v.(map[string]any); ok {
				out[k] = redactValue(nested, extraKeys...)
			} else {
				out[k] = v
			}
		}
	}
	return out
}

// redactString 对字符串中的敏感模式做替换（如 Authorization header）。
func redactString(s string) string {
	if s == "" {
		return s
	}
	// 截断 Bearer token 只保留前缀
	if strings.HasPrefix(strings.ToLower(s), "bearer ") {
		return "Bearer [REDACTED]"
	}
	if strings.HasPrefix(strings.ToLower(s), "sk-aisaas-") {
		return "sk-aisaas-[REDACTED]"
	}
	return s
}