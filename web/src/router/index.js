import { createRouter, createWebHistory } from 'vue-router'

const guard = (to, next) => {
  if (!localStorage.getItem('token') && to.path !== '/login') next('/login')
  else next()
}

const router = createRouter({
  history: createWebHistory('/portal/'),
  routes: [
    { path: '/', redirect: '/dashboard' },
    {
      path: '/login', component: () => import('../views/Login.vue'),
      beforeEnter: (t, f, n) => (localStorage.getItem('token') ? n('/dashboard') : n()),
    },
    {
      path: '/',
      component: () => import('../views/Layout.vue'),
      beforeEnter: (t, f, n) => guard(t, n),
      children: [
        { path: 'dashboard', component: () => import('../views/Dashboard.vue'), meta: { title: '总览' } },
        { path: 'billing', component: () => import('../views/Billing.vue'), meta: { title: '账单' } },
        { path: 'models', component: () => import('../views/Models.vue'), meta: { title: '模型' } },
        { path: 'apikeys', component: () => import('../views/APIKeys.vue'), meta: { title: 'API Key' } },
        { path: 'personas', component: () => import('../views/Personas.vue'), meta: { title: '人设' } },
        { path: 'memory', component: () => import('../views/Memory.vue'), meta: { title: '记忆' } },
        { path: 'sessions', component: () => import('../views/Sessions.vue'), meta: { title: '会话' } },
        { path: 'mcp', component: () => import('../views/MCP.vue'), meta: { title: 'MCP 工具' } },
        { path: 'kb', component: () => import('../views/KB.vue'), meta: { title: '知识库' } },
        { path: 'anomaly', component: () => import('../views/Anomaly.vue'), meta: { title: '异常规则' } },
        { path: 'devices', component: () => import('../views/Devices.vue'), meta: { title: '设备' } },
        { path: 'orders', component: () => import('../views/Orders.vue'), meta: { title: '订单' } },
      ]
    },
    { path: '/:pathMatch(.*)*', redirect: '/dashboard' },
  ],
})

router.beforeEach((to, _from, next) => guard(to, next))

export default router