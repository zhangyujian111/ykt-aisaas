import axios from 'axios'

// portal (JWT) — 租户/客户接口
const portal = axios.create({ baseURL: '/portal/api/v1', timeout: 15000 })
portal.interceptors.request.use((c) => {
  const t = localStorage.getItem('token')
  if (t) c.headers.Authorization = 'Bearer ' + t
  return c
})
portal.interceptors.response.use(
  (r) => (r.data.code === 0 ? r.data.data : Promise.reject(r.data)),
  (e) => Promise.reject(e.response?.data || { message: e.message })
)

// internal (X-Internal-Token) — 运营接口（Models CRUD / APIKey 发放）
const internalToken = 'dev-internal-token'
const admin = axios.create({ baseURL: '/internal/api', timeout: 30000 })
admin.interceptors.request.use((c) => {
  c.headers['X-Internal-Token'] = localStorage.getItem('aisaas_internal_token') || internalToken
  return c
})
admin.interceptors.response.use(
  (r) => (r.data.code === 0 ? r.data : Promise.reject(r.data)),
  (e) => Promise.reject(e.response?.data || { message: e.message })
)

// bearer API（/api/v1/*）— 模型/记忆/会话/persona/mcp
const bearer = axios.create({ baseURL: '/api/v1', timeout: 30000 })
bearer.interceptors.request.use((c) => {
  c.headers.Authorization = 'Bearer ' + (localStorage.getItem('aisaas_bearer_key') || '')
  return c
})
bearer.interceptors.response.use(
  (r) => {
    if (r.data && typeof r.data === 'object' && 'code' in r.data) {
      return r.data.code === 0 ? r.data.data : Promise.reject(r.data)
    }
    return r.data
  },
  (e) => Promise.reject(e.response?.data || { message: e.message })
)

export default {
  // portal
  register: (d) => portal.post('/auth/register', d),
  login: (d) => portal.post('/auth/login', d),
  devices: () => portal.get('/devices'),
  bind: (d) => portal.post('/devices/bind', d),
  plans: () => portal.get('/plans'),
  subscribe: (tid, planId) => portal.post(`/devices/${tid}/subscribe`, { planId }),
  recharge: (tid, amountCents) => portal.post(`/devices/${tid}/recharge`, { amountCents }),
  payMock: (id) => portal.post(`/orders/${id}/pay-mock`),
  orders: () => portal.get('/orders'),

  // models (internal API)
  models: () => admin.get('/v1/models'),
  createModel: (d) => admin.post('/v1/models', d),
  updateModel: (id, d) => admin.put(`/v1/models/${id}`, d),
  deleteModel: (id) => admin.delete(`/v1/models/${id}`),
  testModel: (d) => admin.post('/v1/models/test', d),

  // apikeys (Bearer) — GET /api/v1/apikeys（用 sk-aisaas 自动带 tenant 过滤）
  apikeys: () => bearer.get('/apikeys'),

  // personas (Bearer) — 需要 ?deviceId=
  personas: (deviceId) => bearer.get(`/personas`, { params: { deviceId } }),
  createPersona: (d) => bearer.post('/personas', d),
  updatePersona: (id, d) => bearer.put(`/personas/${id}`, d),
  deletePersona: (id) => bearer.delete(`/personas/${id}`),

  // memory — 后端路由: GET /memories/:deviceId/messages
  memory: (deviceId) => bearer.get(`/memories/${deviceId}/messages`),

  // sessions — 后端新增 GET /sessions/:deviceId 路由
  sessions: (deviceId) => bearer.get(`/sessions/${deviceId}`),

  // mcp
  mcpTools: () => bearer.get('/mcp/tools'),

  // billing
  balance: () => bearer.get('/billing/balance'),
  usageOverview: () => bearer.get('/usage/overview'),

  // knowledge base
  knowledgeBases: () => bearer.get('/knowledge-bases'),
}