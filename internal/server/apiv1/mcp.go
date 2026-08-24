package apiv1

import (
	"encoding/json"

	"github.com/gin-gonic/gin"

	"ykt.dev/aisaas/internal/mcp"
	"ykt.dev/aisaas/internal/platform/errs"
	"ykt.dev/aisaas/internal/platform/ids"
	"ykt.dev/aisaas/internal/platform/web"
)

// MCPHandler /api/v1/mcp/tools。
type MCPHandler struct {
	Repo *mcp.Repo
	Svc  *mcp.Service
}

type createToolReq struct {
	Name        string          `json:"name" binding:"required,max=64"`
	Description string          `json:"description"`
	ToolType    string          `json:"toolType" binding:"required,oneof=http builtin"`
	Endpoint    string          `json:"endpoint"`
	Method      string          `json:"method"`
	AuthToken   string          `json:"authToken"`
	InputSchema json.RawMessage `json:"inputSchema" binding:"required"`
	TimeoutMs   int             `json:"timeoutMs"`
}

// Create POST /api/v1/mcp/tools。
func (h *MCPHandler) Create(c *gin.Context) {
	var req createToolReq
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidParam, err.Error()))
		return
	}
	if req.ToolType == "http" && req.Endpoint == "" {
		web.Abort(c, errs.New(errs.InvalidParam, "http 工具必须提供 endpoint"))
		return
	}
	method := req.Method
	if method == "" {
		method = "POST"
	}
	timeout := req.TimeoutMs
	if timeout <= 0 {
		timeout = 10000
	}
	do := &mcp.ToolDO{
		Name: req.Name, Description: req.Description, ToolType: req.ToolType,
		Endpoint: req.Endpoint, Method: method, AuthToken: req.AuthToken,
		InputSchema: string(req.InputSchema), TimeoutMs: timeout, Status: 1,
	}
	do.ID = ids.Next()
	if err := h.Repo.Create(c.Request.Context(), do); err != nil {
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}
	web.OK(c, do)
}

// List GET /api/v1/mcp/tools（自有 + 绑定的全局）。
func (h *MCPHandler) List(c *gin.Context) {
	dos, err := h.Repo.ListForTenant(c.Request.Context())
	if err != nil {
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}
	web.OK(c, dos)
}

type bindReq struct {
	ToolID  int64 `json:"toolId" binding:"required"`
	Enabled bool  `json:"enabled"`
}

// Bind POST /api/v1/mcp/tools/bind（绑定全局工具到租户）。
func (h *MCPHandler) Bind(c *gin.Context) {
	var req bindReq
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidParam, err.Error()))
		return
	}
	if err := h.Repo.Bind(c.Request.Context(), req.ToolID, req.Enabled); err != nil {
		web.Abort(c, errs.Wrap(errs.Internal, err))
		return
	}
	web.OK(c, gin.H{"toolId": req.ToolID, "enabled": req.Enabled})
}

type invokeReq struct {
	Name string          `json:"name" binding:"required"`
	Args json.RawMessage `json:"args"`
}

// Test POST /api/v1/mcp/tools/test（直接调用工具）。
func (h *MCPHandler) Test(c *gin.Context) {
	var req invokeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		web.Abort(c, errs.New(errs.InvalidParam, err.Error()))
		return
	}
	if len(req.Args) == 0 {
		req.Args = json.RawMessage("{}")
	}
	ev := h.Svc.Execute(c.Request.Context(), req.Name, req.Args)
	if ev.Err != "" {
		web.Abort(c, errs.New(errs.ProviderError, ev.Name+": "+ev.Err))
		return
	}
	web.OK(c, ev)
}
