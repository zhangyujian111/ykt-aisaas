package rag

import (
	"strings"
	"testing"
)

func TestChunkBasic(t *testing.T) {
	text := strings.Repeat("小智设备使用说明。", 300) // 2700 rune 单段
	chunks := Chunk(text, 800, 100)
	if len(chunks) < 3 {
		t.Fatalf("long para should slide-window, got %d chunks", len(chunks))
	}
	for _, c := range chunks {
		n := len([]rune(c))
		if n > 800 {
			t.Fatalf("chunk exceeds size: %d", n)
		}
	}
}

func TestChunkParagraphsPreferred(t *testing.T) {
	text := "第一段内容。\n\n第二段内容。\n\n第三段内容。"
	chunks := Chunk(text, 800, 100)
	if len(chunks) != 3 {
		t.Fatalf("expect 3 paragraph chunks, got %d", len(chunks))
	}
}

func TestChunkOverlap(t *testing.T) {
	text := strings.Repeat("字", 1000)
	chunks := Chunk(text, 400, 100)
	if len(chunks) < 3 {
		t.Fatalf("expect >=3 chunks, got %d", len(chunks))
	}
	r1 := []rune(chunks[0])
	r2 := []rune(chunks[1])
	tail := string(r1[len(r1)-100:])
	head := string(r2[:100])
	if tail != head {
		t.Fatalf("overlap mismatch: tail=%q head=%q", tail, head)
	}
}

func TestChunkEmpty(t *testing.T) {
	if got := Chunk("  \n ", 800, 100); got != nil {
		t.Fatalf("empty should be nil, got %v", got)
	}
}
