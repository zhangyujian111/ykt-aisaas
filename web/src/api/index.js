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

// internal (X-Internal-Token) — 运营接口（Models / OSS / Templates CRUD）
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

// bearer API（/api/v1/*）— 模型/记忆/会话/persona/mcp/kb
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
  // ---- portal ----
  register: (d) => portal.post('/auth/register', d),
  login: (d) => portal.post('/auth/login', d),
  devices: () => portal.get('/devices'),
  bind: (d) => portal.post('/devices/bind', d),
  plans: () => portal.get('/plans'),
  subscribe: (tid, planId) => portal.post(`/devices/${tid}/subscribe`, { planId }),
  recharge: (tid, amountCents) => portal.post(`/devices/${tid}/recharge`, { amountCents }),
  payMock: (id) => portal.post(`/orders/${id}/pay-mock`),
  orders: () => portal.get('/orders'),
  accountMe: () => portal.get('/me'),
  changePassword: (d) => portal.post('/auth/change-password', d),

  // ---- models (internal API) ----
  models: (params) => admin.get('/v1/models', { params }),
  createModel: (d) => admin.post('/v1/models', d),
  updateModel: (id, d) => admin.put(`/v1/models/${id}`, d),
  deleteModel: (id) => admin.delete(`/v1/models/${id}`),
  testModel: (d) => admin.post('/v1/models/test', d),

  // ---- oss config (internal API) ----
  ossConfigs: () => admin.get('/v1/oss'),
  createOss: (d) => admin.post('/v1/oss', d),
  updateOss: (id, d) => admin.put(`/v1/oss/${id}`, d),
  deleteOss: (id) => admin.delete(`/v1/oss/${id}`),

  // ---- prompt templates (internal API) ----
  templates: (params) => admin.get('/v1/templates', { params }),
  createTemplate: (d) => admin.post('/v1/templates', d),
  updateTemplate: (id, d) => admin.put(`/v1/templates/${id}`, d),
  deleteTemplate: (id) => admin.delete(`/v1/templates/${id}`),

  // ---- apikeys (Bearer) ----
  apikeys: () => bearer.get('/apikeys'),

  // ---- personas (Bearer) ----
  personas: (deviceId) => bearer.get(`/personas`, { params: { deviceId } }),
  createPersona: (d) => bearer.post('/personas', d),
  updatePersona: (id, d) => bearer.put(`/personas/${id}`, d),
  deletePersona: (id) => bearer.delete(`/personas/${id}`),

  // ---- memory ----
  memory: (deviceId) => bearer.get(`/memories/${deviceId}/messages`),
  extractMemory: (deviceId) => bearer.post(`/memories/${deviceId}/extract`),
  summarizeMemory: (deviceId) => bearer.post(`/memories/${deviceId}/summarize`),
  memoryGraph: (deviceId) => bearer.get(`/memories/${deviceId}`),

  // ---- sessions ----
  sessions: (deviceId) => bearer.get(`/sessions/${deviceId}`),
  createSession: (deviceId) => bearer.post(`/sessions/${deviceId}`),
  sessionHistory: (deviceId, sessionId) => bearer.get(`/sessions/${deviceId}/history`, { params: { sessionId } }),
  endSession: (deviceId, sessionId) => bearer.post(`/sessions/${deviceId}/${sessionId}/end`),

  // ---- mcp ----
  mcpTools: () => bearer.get('/mcp/tools'),

  // ---- billing ----
  balance: () => bearer.get('/billing/balance'),
  usageOverview: () => bearer.get('/usage/overview'),

  // ---- knowledge base (Bearer) ----
  knowledgeBases: () => bearer.get('/knowledge-bases'),
  createKb: (d) => bearer.post('/knowledge-bases', d),
  deleteKb: (id) => bearer.delete(`/knowledge-bases/${id}`),
  kbDocs: (id) => bearer.get(`/knowledge-bases/${id}/documents`),
  uploadDoc: (id, formData) => bearer.post(`/knowledge-bases/${id}/documents`, formData, {
    headers: { 'Content-Type': 'multipart/form-data' }
  }),
  kbSearch: (id, query, topK = 5) => bearer.post(`/knowledge-bases/${id}/search`, { query, topK }),

  // ---- chat (Bearer — OpenAI compatible) ----
  chat: (body) => bearer.post('/chat/completions', body),
  chatStream: (body) => bearer.post('/chat/completions', body, { responseType: 'stream' }),

  // ---- models list for chat UI ----
  modelList: () => bearer.get('/models'),
}
