FROM golang:1.24-alpine AS deps
# 模块代理可通过 --build-arg 覆盖；默认留空即使用镜像自带的官方代理。
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=$GOPROXY
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download

FROM deps AS test
COPY . .
RUN go test ./...

FROM deps AS build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/campusclaw ./cmd/server

FROM alpine:3.22 AS runtime
RUN addgroup -S app && adduser -S -G app app && mkdir -p /data/uploads && chown -R app:app /data
USER app
COPY --from=build /out/campusclaw /usr/local/bin/campusclaw
EXPOSE 8081
ENTRYPOINT ["campusclaw"]
