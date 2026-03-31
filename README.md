# Airforce Filtration Proxy

API 请求过滤代理&负载均衡。


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

### 2. Docker Compose 部署

```bash
docker compose up -d
```

### 3. 直接运行

```bash
go build -o airforce-filtration .
./airforce-filtration
```

## 使用方式

在 NewAPI 中将渠道的上游地址设置为：

```
http://your-server-ip:6777
```

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

MIT License - see the [LICENSE](LICENSE) file for details.
