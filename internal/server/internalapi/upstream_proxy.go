package internalapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// newUpstreamProxy 用同一代 upstream client 替前端试调一次，回包完整 body + status。
func newUpstreamProxy(baseURL, apiKey, path string, body []byte) (map[string]any, error) {
	url := strings.TrimRight(baseURL, "/") + path
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	cli := &http.Client{Timeout: 20 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	out := map[string]any{
		"status":  resp.StatusCode,
		"headers": flattenHeaders(resp.Header),
	}
	// 试解 JSON，失败也保留原文
	var j any
	if json.Unmarshal(respBody, &j) == nil {
		out["body"] = j
	} else {
		out["bodyRaw"] = string(respBody)
	}
	return out, nil
}

func flattenHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}