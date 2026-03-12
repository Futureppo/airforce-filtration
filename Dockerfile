FROM golang:1.25-alpine AS builder

WORKDIR /app

# 复制 go.mod 和 go.sum
COPY go.mod go.sum ./
RUN go mod download

# 复制源码
COPY *.go ./

# 编译
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /app/airforce-filtration .

# ==================== 运行阶段 ====================
FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# 从构建阶段复制二进制
COPY --from=builder /app/airforce-filtration .

# 暴露端口
EXPOSE 6777

# 启动
CMD ["./airforce-filtration"]
