# Airforce Filtration Proxy

API 请求过滤代理，用于过滤 [api.airforce](https://api.airforce) 响应中的广告内容。

## 功能

- **透传 Key** — 直接透传来自 NewAPI 的 Authorization header，不管理任何 Key
- **HTTP 代理池** — 支持配置 HTTP 代理发送上游请求
- **429 自动重试** — 遇到限流/网络错误自动重试，支持自定义重试次数
- **广告过滤** — 自动过滤上游响应中的广告内容（支持流式和非流式）
- **Docker 部署** — 支持 Docker / Docker Compose 一键部署

## 架构

```
用户 → NewAPI（管理Key + 轮询） → 本代理(:6777) → [HTTP代理] → api.airforce
                                       ↑
                                 429重试 + 广告过滤
```

- Key 全部由 NewAPI 管理，本代理仅透传请求中的 Authorization header
- 本代理不存储、不管理任何 Key

## 快速开始

### 1. 配置

复制配置文件模板：

```bash
cp .env.example .env
```

编辑 `.env` 文件：

```env
# HTTP 代理（可选）
PROXY_URL=http://ip:port

# 其他配置保持默认即可
LISTEN_PORT=6777
MAX_RETRIES=3
```

### 2. Docker Compose 部署（推荐）

```bash
docker compose up -d
```

### 3. 直接运行

```bash
# 编译
go build -o airforce-filtration .

# 运行
./airforce-filtration
```

## 使用方式

在 NewAPI 中将渠道的上游地址设置为：

```
http://your-server-ip:6777
```

### 接口

| 路径                   | 方法 | 说明                            |
| ---------------------- | ---- | ------------------------------- |
| `/v1/chat/completions` | POST | Chat Completions（OpenAI 兼容） |
| `/health`              | GET  | 健康检查                        |

## 配置说明

所有配置均为可选，通过 `.env` 文件设置：

| 环境变量         | 默认值                 | 说明                                 |
| ---------------- | ---------------------- | ------------------------------------ |
| `PROXY_URL`      | 空（直连）             | HTTP 代理地址，格式 `http://ip:port` |
| `UPSTREAM_URL`   | `https://api.airforce` | 上游 API 地址                        |
| `LISTEN_PORT`    | `6777`                 | 监听端口                             |
| `MAX_RETRIES`    | `3`                    | 429/网络错误最大重试次数             |
| `AD_KEYWORDS`    | 内置关键词             | 自定义广告关键词，`\|` 分隔          |
| `AD_BUFFER_SIZE` | `400`                  | 流式响应尾部缓冲区大小（字符）       |

## License

MIT
