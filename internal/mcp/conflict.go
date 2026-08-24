package mcp

import "gorm.io/gorm/clause"

func clauseOnConflict() clause.Expression {
	return clause.OnConflict{
		Columns:   []clause.Column{{Name: "tenantId"}, {Name: "mcpToolId"}},
		DoUpdates: clause.AssignmentColumns([]string{"enabled"}),
	}
}
