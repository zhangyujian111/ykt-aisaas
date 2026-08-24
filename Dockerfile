FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN GOPROXY=https://goproxy.cn,direct go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/aisaas ./cmd/aisaas

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/aisaas /usr/local/bin/aisaas
EXPOSE 8190
ENTRYPOINT ["aisaas", "-config", "config.yaml"]
