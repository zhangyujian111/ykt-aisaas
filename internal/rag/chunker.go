package rag

import (
	"strings"
	"unicode/utf8"
)

// Chunk 按段落优先、固定窗口兜底的方式切片（rune 计，中文友好）。
// 段落（\n\n 或单 \n）≤ size 时整段成块；超长段落按 size-overlap 滑窗。
func Chunk(text string, size, overlap int) []string {
	if size <= 0 {
		size = 800
	}
	if overlap < 0 || overlap >= size {
		overlap = size / 8
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	var out []string
	for _, para := range splitParagraphs(text) {
		r := []rune(para)
		if len(r) <= size {
			out = appendChunk(out, para)
			continue
		}
		step := size - overlap
		for i := 0; i < len(r); i += step {
			end := i + size
			if end > len(r) {
				end = len(r)
			}
			out = appendChunk(out, string(r[i:end]))
			if end == len(r) {
				break
			}
		}
	}
	return out
}

func splitParagraphs(text string) []string {
	norm := strings.ReplaceAll(text, "\r\n", "\n")
	parts := strings.Split(norm, "\n")
	var paras []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			paras = append(paras, strings.TrimSpace(p))
		}
	}
	if paras == nil {
		paras = []string{text}
	}
	return paras
}

func appendChunk(out []string, c string) []string {
	c = strings.TrimSpace(c)
	if utf8.RuneCountInString(c) >= 2 {
		out = append(out, c)
	}
	return out
}
