package rag

import "ykt.dev/aisaas/internal/platform/database"

func databaseBase(id int64) database.BaseDO {
	return database.BaseDO{ID: id}
}
