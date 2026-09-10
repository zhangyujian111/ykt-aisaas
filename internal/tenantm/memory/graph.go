package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"ykt.dev/aisaas/internal/platform/tenant"
)

// GetGraph 拉取长期记忆图谱。
func (s *Service) GetGraph(ctx context.Context, deviceID string, limit int, dimension string) (*MemoryGraphResp, error) {
	tid, _ := tenant.FromSafe(ctx)
	_ = tid

	// 按维度筛选实体类型
	var entityTypeFilter string
	if dimension != "" {
		entityTypeFilter = mapDimensionToEntityType(dimension)
	}

	entities, err := s.GetEntitiesByDevice(ctx, deviceID, limit, entityTypeFilter)
	if err != nil {
		return nil, fmt.Errorf("get entities: %w", err)
	}

	// 更新访问时间
	ids := make([]int64, len(entities))
	for i, e := range entities {
		ids[i] = e.ID
	}
	if len(ids) > 0 {
		go s.UpdateLastAccess(context.Background(), ids)
	}

	resp := &MemoryGraphResp{
		DeviceID:  deviceID,
		Entities:  make([]*MemoryEntity, 0),
		Preferences: make([]*MemoryPreference, 0),
		Events:    make([]*MemoryEvent, 0),
		UpdatedAt: time.Now(),
	}

	for _, e := range entities {
		switch e.EntityType {
		case "PREFERENCE":
			pref := entityToPreference(e)
			resp.Preferences = append(resp.Preferences, pref)
		case "EVENT":
			ev := entityToEvent(e)
			resp.Events = append(resp.Events, ev)
		default:
			ent := entityToMemoryEntity(e)
			resp.Entities = append(resp.Entities, ent)
		}
	}

	return resp, nil
}

// entityToMemoryEntity 转换实体为 MemoryEntity。
func entityToMemoryEntity(e *MemoryDO) *MemoryEntity {
	ent := &MemoryEntity{
		ID:         strconv.FormatInt(e.ID, 10),
		Type:       mapEntityType(e.EntityType),
		Name:       e.EntityKey,
		Confidence: e.Importance,
	}
	if e.Content != "" && e.Content != "{}" {
		var content map[string]any
		if err := json.Unmarshal([]byte(e.Content), &content); err == nil {
			ent.Attributes = content
			if facts, ok := content["facts"].([]any); ok {
				ent.Mentions = make([]string, 0, len(facts))
				for _, f := range facts {
					if s, ok := f.(string); ok {
						ent.Mentions = append(ent.Mentions, s)
					}
				}
			}
		}
	}
	return ent
}

// entityToPreference 转换实体为 MemoryPreference。
func entityToPreference(e *MemoryDO) *MemoryPreference {
	pref := &MemoryPreference{
		Dimension:  e.EntityType,
		Value:      e.EntityKey,
		Confidence: e.Importance,
	}
	if e.Content != "" && e.Content != "{}" {
		var content map[string]any
		if err := json.Unmarshal([]byte(e.Content), &content); err == nil {
			if dim, ok := content["dimension"].(string); ok {
				pref.Dimension = dim
			}
		}
	}
	return pref
}

// entityToEvent 转换实体为 MemoryEvent。
func entityToEvent(e *MemoryDO) *MemoryEvent {
	ev := &MemoryEvent{
		ID:          strconv.FormatInt(e.ID, 10),
		EventType:   "interaction",
		Description: e.EntityKey,
		EventTime:   e.CreateTime,
	}
	if e.Content != "" && e.Content != "{}" {
		var content map[string]any
		if err := json.Unmarshal([]byte(e.Content), &content); err == nil {
			if et, ok := content["eventType"].(string); ok {
				ev.EventType = et
			}
			if desc, ok := content["description"].(string); ok {
				ev.Description = desc
			}
			ev.Metadata = content
		}
	}
	return ev
}

// mapEntityType 将内部 entityType 映射为 OpenAPI 类型。
func mapEntityType(t string) string {
	switch t {
	case "PERSON":
		return "person"
	case "OBJECT":
		return "object"
	case "LOCATION":
		return "place"
	case "ORGANIZATION":
		return "organization"
	case "KNOWLEDGE":
		return "concept"
	default:
		return "concept"
	}
}

// mapDimensionToEntityType 将 OpenAPI dimension 映射为内部 entityType。
func mapDimensionToEntityType(d string) string {
	switch d {
	case "entity":
		return "" // 全部非 preference/event
	case "preference":
		return "PREFERENCE"
	case "event":
		return "EVENT"
	case "skill":
		return "KNOWLEDGE"
	default:
		return ""
	}
}