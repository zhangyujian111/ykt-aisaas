# ykt-aisaas-portal — 租户门户前端
#
# 构建产物由 nginx 提供，API 转发规则与 vite.config.js 的 dev proxy 保持一致。

FROM node:20-alpine AS build

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY . .
RUN npm run build

FROM nginx:alpine

COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
