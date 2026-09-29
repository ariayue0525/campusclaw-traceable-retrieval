FROM node:22-alpine AS test
WORKDIR /app
COPY frontend/package*.json ./
RUN npm ci
COPY frontend/ .
RUN npm test && npm run build

# 前端以 /week04/ 为基址构建，产物落在 html/week04 下，由 nginx 的 /week04/ 位置提供服务。
FROM nginx:1.29-alpine
COPY --from=test /app/dist /usr/share/nginx/html/week04
COPY nginx/default.conf /etc/nginx/conf.d/default.conf
EXPOSE 8080
