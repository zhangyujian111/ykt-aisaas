import { createRouter, createWebHistory } from 'vue-router'

const guard = (to, next) => {
  if (!localStorage.getItem('token') && to.path !== '/login') next('/login')
  else next()
}

export default createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', redirect: '/devices' },
    {
      path: '/login', component: () => import('../views/Login.vue'),
      beforeEnter: (t, f, n) => (localStorage.getItem('token') ? n('/devices') : n()),
    },
    { path: '/devices', component: () => import('../views/Devices.vue'), beforeEnter: (t, f, n) => guard(t, n) },
    { path: '/orders', component: () => import('../views/Orders.vue'), beforeEnter: (t, f, n) => guard(t, n) },
  ],
})
