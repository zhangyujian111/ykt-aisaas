// Package ids 全局雪花 ID 生成器（进程内唯一 node，避免多实例 Node(1) 撞号）。
package ids

import (
	"sync"

	"github.com/bwmarrin/snowflake"
)

var (
	once sync.Once
	node *snowflake.Node
)

// Next 生成 int64 雪花 ID。
func Next() int64 {
	once.Do(func() {
		node, _ = snowflake.NewNode(1)
	})
	return node.Generate().Int64()
}
