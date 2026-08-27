import axios from 'axios'

const http = axios.create({ baseURL: '/portal/api/v1', timeout: 15000 })
http.interceptors.request.use((c) => {
  const t = localStorage.getItem('token')
  if (t) c.headers.Authorization = 'Bearer ' + t
  return c
})
http.interceptors.response.use(
  (r) => (r.data.code === 0 ? r.data.data : Promise.reject(r.data)),
  (e) => Promise.reject(e.response?.data || { message: e.message })
)

export default {
  register: (d) => http.post('/auth/register', d),
  login: (d) => http.post('/auth/login', d),
  devices: () => http.get('/devices'),
  bind: (d) => http.post('/devices/bind', d),
  plans: () => http.get('/plans'),
  subscribe: (tid, planId) => http.post(`/devices/${tid}/subscribe`, { planId }),
  recharge: (tid, amountCents) => http.post(`/devices/${tid}/recharge`, { amountCents }),
  payMock: (id) => http.post(`/orders/${id}/pay-mock`),
  orders: () => http.get('/orders'),
}
