package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"gorm.io/gorm"

	"ykt.dev/aisaas/internal/platform/database"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/ids"
	"ykt.dev/aisaas/internal/platform/metering"
	"ykt.dev/aisaas/internal/platform/tenant"
	"ykt.dev/aisaas/pkg/openaiclient"
)

// ToolDO ykt_aisaas_mcp_tool。
type ToolDO struct {
	database.BaseDO
	Name        string `gorm:"column:toolName" json:"toolName"`
	Description string `gorm:"column:description" json:"description"`
	ToolType    string `gorm:"column:toolType" json:"toolType"`
	Endpoint    string `gorm:"column:endpoint" json:"endpoint"`
	Method      string `gorm:"column:method" json:"method"`
	AuthToken   string `gorm:"column:authToken" json:"-"`
	InputSchema string `gorm:"column:inputSchema" json:"inputSchema"`
	TimeoutMs   int    `gorm:"column:timeoutMs" json:"timeoutMs"`
	CallCount   int64  `gorm:"column:callCount" json:"callCount"`
	Status      int8   `gorm:"column:status" json:"status"`
	IsDeleted   int8   `gorm:"column:isDeleted" json:"-"`
}

func (ToolDO) TableName() string { return "ykt_aisaas_mcp_tool" }

// BindingDO ykt_aisaas_tenant_mcp_binding。
type BindingDO struct {
	ID         int64     `gorm:"column:id;primaryKey"`
	TenantID   int64     `gorm:"column:tenantId"`
	McpToolID  int64     `gorm:"column:mcpToolId"`
	Enabled    int8      `gorm:"column:enabled"`
	CreateTime time.Time `gorm:"column:createTime"`
}

func (BindingDO) TableName() string { return "ykt_aisaas_tenant_mcp_binding" }

// Repo 数据访问。
type Repo struct{ db *gorm.DB }

func NewRepo(db *gorm.DB) *Repo { return &Repo{db: db} }

// Create 注册工具（租户私有；tenantId 由 GORM 插件填充）。
func (r *Repo) Create(ctx context.Context, do *ToolDO) error {
	return r.db.WithContext(ctx).Create(do).Error
}

// ListForTenant 租户可见工具：自有 + 已绑定的全局工具。
func (r *Repo) ListForTenant(ctx context.Context) ([]*ToolDO, error) {
	var out []*ToolDO
	// 自有工具（tenantId 过滤由插件保证）
	if err := r.db.WithContext(ctx).Where("isDeleted = 0 AND status = 1").Find(&out).Error; err != nil {
		return nil, err
	}
	// 绑定的全局工具（binding 表含 tenantId，走插件；mcp_tool 为跨租户表需显式查）
	var boundIDs []int64
	if err := r.db.WithContext(ctx).Model(&BindingDO{}).
		Where("enabled = 1").Pluck("mcpToolId", &boundIDs).Error; err != nil {
		return nil, err
	}
	if len(boundIDs) > 0 {
		var globals []*ToolDO
		if err := r.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
			Table(ToolDO{}.TableName()).
			Where("id IN ? AND tenantId IS NULL AND isDeleted = 0 AND status = 1", boundIDs).
			Find(&globals).Error; err != nil {
			return nil, err
		}
		out = append(out, globals...)
	}
	return out, nil
}

func (r *Repo) GetByID(ctx context.Context, id int64) (*ToolDO, error) {
	var do ToolDO
	err := r.db.WithContext(ctx).Where("id = ? AND isDeleted = 0", id).Take(&do).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errs.New(errs.ResourceNotFound, "工具不存在")
	}
	return &do, err
}

// GetGlobalByName 按名取全局工具（chat 执行时解析）。
func (r *Repo) findByName(ctx context.Context, tid int64, name string) (*ToolDO, error) {
	sess := r.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table(ToolDO{}.TableName()).
		Where("toolName = ? AND isDeleted = 0 AND status = 1", name)
	if tid > 0 {
		sess = sess.Where("tenantId = ? OR tenantId IS NULL", tid)
	} else {
		sess = sess.Where("tenantId IS NULL")
	}
	var rows []*ToolDO
	if err := sess.Order("tenantId DESC").Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errs.New(errs.ResourceNotFound, "工具未注册或未绑定: "+name)
	}
	return rows[0], nil
}

func (r *Repo) Bind(ctx context.Context, toolID int64, enabled bool) error {
	tid, _ := tenant.FromSafe(ctx)
	do := &BindingDO{ID: ids.Next(), TenantID: tid, McpToolID: toolID}
	if enabled {
		do.Enabled = 1
	}
	return r.db.WithContext(ctx).
		Clauses(clauseOnConflict()).
		Create(do).Error
}

// BumpCallCount 计数（best-effort）。
func (r *Repo) BumpCallCount(ctx context.Context, id int64) {
	_ = r.db.Session(&gorm.Session{SkipHooks: true, Context: ctx}).
		Table(ToolDO{}.TableName()).Where("id = ?", id).
		Update("callCount", gorm.Expr("callCount + 1")).Error
}

// ---- 内置工具注册表 ----

type builtinFn func(args json.RawMessage) (any, error)

var builtins = map[string]builtinFn{}

// RegisterBuiltin 注册内置工具（进程启动时调用）。
func RegisterBuiltin(name string, fn builtinFn) { builtins[name] = fn }

func init() {
	RegisterBuiltin("get_time", func(args json.RawMessage) (any, error) {
		var in struct {
			Timezone string `json:"timezone"`
		}
		_ = json.Unmarshal(args, &in)
		loc := time.Local
		if in.Timezone != "" {
			var err error
			loc, err = time.LoadLocation(in.Timezone)
			if err != nil {
				return nil, fmt.Errorf("bad timezone %q: %w", in.Timezone, err)
			}
		}
		return map[string]any{"now": time.Now().In(loc).Format(time.RFC3339)}, nil
	})
}

// ---- Service：ToolSource 实现（chat 集成）----

// Service MCP 工具服务。
type Service struct {
	repo  *Repo
	http  *http.Client
	meter *metering.Recorder
}

func NewService(repo *Repo, meter *metering.Recorder) *Service {
	return &Service{repo: repo, http: &http.Client{Timeout: 15 * time.Second}, meter: meter}
}

// ListTools 租户可用工具 → OpenAI Tool 定义。
func (s *Service) ListTools(ctx context.Context) []openaiclient.Tool {
	dos, err := s.repo.ListForTenant(ctx)
	if err != nil {
		return nil
	}
	var out []openaiclient.Tool
	for _, d := range dos {
		var params map[string]any
		if json.Unmarshal([]byte(d.InputSchema), &params) != nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, openaiclient.Tool{
			Type: "function",
			Function: openaiclient.ToolFunc{
				Name: d.Name, Description: d.Description, Parameters: params,
			},
		})
	}
	return out
}

// ToolCallEvent 工具调用事件（SSE x-tool-call / 非流式扩展）。
type ToolCallEvent struct {
	Name   string `json:"name"`
	Args   string `json:"args"`
	Result any    `json:"result"`
	Err    string `json:"error,omitempty"`
}

// Execute 执行工具调用。
func (s *Service) Execute(ctx context.Context, name string, args json.RawMessage) ToolCallEvent {
	ev := ToolCallEvent{Name: name, Args: string(args)}
	tid, _ := tenant.FromSafe(ctx)
	do, err := s.repo.findByName(ctx, tid, name)
	if err != nil {
		ev.Err = err.Error()
		return ev
	}

	var result any
	switch do.ToolType {
	case "builtin":
		fn, ok := builtins[do.Name]
		if !ok {
			ev.Err = "内置工具未实现: " + do.Name
			return ev
		}
		result, err = fn(args)
	case "http":
		result, err = s.callHTTP(ctx, do, args)
	default:
		ev.Err = "未知工具类型: " + do.ToolType
		return ev
	}
	if err != nil {
		ev.Err = err.Error()
		return ev
	}
	ev.Result = result
	go s.repo.BumpCallCount(context.Background(), do.ID)
	if s.meter != nil {
		s.meter.RecordWithCtx(ctx, metering.Record{
			BizType: "mcp", Dimension: "mcp_calls", Amount: 1,
			ModelID: do.Name, Status: 1,
		})
	}
	return ev
}

func (s *Service) callHTTP(ctx context.Context, do *ToolDO, args json.RawMessage) (any, error) {
	if do.Endpoint == "" || !strings.HasPrefix(do.Endpoint, "http") {
		return nil, fmt.Errorf("工具 endpoint 无效")
	}
	timeout := time.Duration(do.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body := bytes.NewReader(args)
	req, err := http.NewRequestWithContext(cctx, do.Method, do.Endpoint, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if do.AuthToken != "" {
		req.Header.Set("Authorization", "Bearer "+do.AuthToken)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("工具上游 %d: %s", resp.StatusCode, truncStr(string(raw), 200))
	}
	var out any
	if json.Unmarshal(raw, &out) != nil {
		return map[string]any{"raw": string(raw)}, nil
	}
	return out, nil
}

func truncStr(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "..."
}
