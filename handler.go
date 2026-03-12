package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Handler 请求处理器
type Handler struct {
	config *Config
	client *http.Client
}

// NewHandler 创建请求处理器，初始化 HTTP 客户端及代理配置
func NewHandler(cfg *Config) *Handler {
	transport := &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: false},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}

	if cfg.ProxyURL != "" {
		proxyURL, err := url.Parse(cfg.ProxyURL)
		if err != nil {
			log.Printf("[警告] 代理地址解析失败: %v，将直连", err)
		} else {
			transport.Proxy = http.ProxyURL(proxyURL)
			log.Printf("[代理] 已配置 HTTP 代理: %s", cfg.ProxyURL)
		}
	}

	return &Handler{
		config: cfg,
		client: &http.Client{
			Transport: transport,
			Timeout:   5 * time.Minute,
		},
	}
}

// HandleChatCompletions 处理 /v1/chat/completions 请求
func (h *Handler) HandleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error": {"message": "Method not allowed", "type": "invalid_request_error"}}`, http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("[错误] 读取请求体失败: %v", err)
		http.Error(w, `{"error": {"message": "Failed to read request body", "type": "server_error"}}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// 直接透传来自 NewAPI 的 Authorization header
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, `{"error": {"message": "Missing Authorization header", "type": "invalid_request_error"}}`, http.StatusUnauthorized)
		return
	}

	isStream := false
	var reqMap map[string]interface{}
	if err := json.Unmarshal(body, &reqMap); err == nil {
		if stream, ok := reqMap["stream"].(bool); ok && stream {
			isStream = true
		}
	}

	keyDisplay := maskKey(authHeader)
	var lastErr error

	// 重试循环（429 时使用同一个 key 重试）
	for attempt := 0; attempt <= h.config.MaxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("[重试] 第 %d 次重试 | Key: %s", attempt, keyDisplay)
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
		}

		log.Printf("[请求] 转发到上游 | Key: %s | 流式: %v | 尝试: %d/%d",
			keyDisplay, isStream, attempt+1, h.config.MaxRetries+1)

		upstreamURL := fmt.Sprintf("%s/v1/chat/completions", strings.TrimRight(h.config.UpstreamURL, "/"))
		upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(body))
		if err != nil {
			lastErr = fmt.Errorf("构造上游请求失败: %v", err)
			log.Printf("[错误] %v", lastErr)
			continue
		}

		upReq.Header.Set("Content-Type", "application/json")
		upReq.Header.Set("Authorization", authHeader)
		upReq.Header.Set("Accept", "*/*")

		resp, err := h.client.Do(upReq)
		if err != nil {
			lastErr = fmt.Errorf("上游请求失败: %v", err)
			log.Printf("[错误] %v", lastErr)
			continue
		}

		// 429 限流需要重试
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			lastErr = fmt.Errorf("上游返回 429 Too Many Requests")
			log.Printf("[限流] Key %s 被限流，准备重试", keyDisplay)
			continue
		}

		// 非 200 状态码直接透传
		if resp.StatusCode != http.StatusOK {
			log.Printf("[上游] 返回状态码: %d", resp.StatusCode)
			h.proxyRawResponse(w, resp)
			return
		}

		if isStream {
			h.handleStreamResponse(w, resp)
		} else {
			h.handleNonStreamResponse(w, resp)
		}
		return
	}

	// 所有重试均失败
	log.Printf("[错误] 所有重试均失败: %v", lastErr)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	fmt.Fprintf(w, `{"error": {"message": "All retries failed: %s", "type": "upstream_error"}}`, lastErr)
}

// handleNonStreamResponse 处理非流式响应
func (h *Handler) handleNonStreamResponse(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[错误] 读取上游响应失败: %v", err)
		http.Error(w, `{"error": {"message": "Failed to read upstream response", "type": "server_error"}}`, http.StatusBadGateway)
		return
	}

	filtered, err := FilterNonStreamResponse(body, h.config.AdKeywords)
	if err != nil {
		log.Printf("[警告] 广告过滤失败，返回原始响应: %v", err)
		filtered = body
	}

	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	w.Write(filtered)

	log.Printf("[完成] 非流式响应已返回，原始大小: %d，过滤后: %d", len(body), len(filtered))
}

// SSEChunk SSE 数据块的 JSON 结构
type SSEChunk struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []SSEChoice `json:"choices"`
	Usage   *SSEUsage   `json:"usage,omitempty"`
}

// SSEChoice SSE 数据块中的选项
type SSEChoice struct {
	Index        int      `json:"index"`
	Delta        SSEDelta `json:"delta"`
	FinishReason *string  `json:"finish_reason"`
}

// SSEDelta SSE 数据块中的增量内容
type SSEDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

// SSEUsage Token 用量信息
type SSEUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// bufferedChunk 缓冲区中的数据块记录
type bufferedChunk struct {
	rawLine    string // 原始 SSE 行（含 "data: " 前缀）
	content    string // delta.content 内容
	contentLen int    // content 的字符长度
	isStop     bool   // 是否为 finish_reason=stop
	isUsage    bool   // 是否为 usage 块（空 choices）
}

// handleStreamResponse 处理流式响应，使用尾部缓冲策略过滤广告
func (h *Handler) handleStreamResponse(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		log.Printf("[错误] ResponseWriter 不支持 Flusher")
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 256*1024), 256*1024)

	var buffer []bufferedChunk
	var bufferContentLen int

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		// 流结束，处理缓冲区并发送 [DONE]
		if data == "[DONE]" {
			h.flushFilteredBuffer(w, flusher, buffer)
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			log.Printf("[完成] 流式响应结束")
			return
		}

		chunk := parseSSEChunk(line, data)
		buffer = append(buffer, chunk)
		bufferContentLen += chunk.contentLen

		// 缓冲区超过阈值时，释放前面已安全的 chunk
		for bufferContentLen > h.config.AdBufferSize && len(buffer) > 1 {
			front := buffer[0]
			buffer = buffer[1:]
			bufferContentLen -= front.contentLen

			fmt.Fprintf(w, "%s\n\n", front.rawLine)
			flusher.Flush()
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[错误] 读取上游流失败: %v", err)
	}

	// 流异常结束时也释放缓冲区
	if len(buffer) > 0 {
		h.flushFilteredBuffer(w, flusher, buffer)
	}
}

// parseSSEChunk 解析 SSE 数据行为 bufferedChunk
func parseSSEChunk(rawLine, data string) bufferedChunk {
	chunk := bufferedChunk{
		rawLine: rawLine,
	}

	var parsed SSEChunk
	if err := json.Unmarshal([]byte(data), &parsed); err != nil {
		return chunk
	}

	if len(parsed.Choices) == 0 {
		chunk.isUsage = true
		return chunk
	}

	chunk.content = parsed.Choices[0].Delta.Content
	chunk.contentLen = len([]rune(chunk.content))
	if parsed.Choices[0].FinishReason != nil && *parsed.Choices[0].FinishReason == "stop" {
		chunk.isStop = true
	}

	return chunk
}

// flushFilteredBuffer 过滤缓冲区中的广告内容后发送给客户端
func (h *Handler) flushFilteredBuffer(w http.ResponseWriter, flusher http.Flusher, buffer []bufferedChunk) {
	if len(buffer) == 0 {
		return
	}

	var contentBuilder strings.Builder
	for _, chunk := range buffer {
		if chunk.content != "" {
			contentBuilder.WriteString(chunk.content)
		}
	}

	bufferedContent := contentBuilder.String()
	filteredContent := FilterAdContent(bufferedContent, h.config.AdKeywords)

	if filteredContent != bufferedContent {
		log.Printf("[过滤] 检测到广告内容，已过滤 %d 字符", len(bufferedContent)-len(filteredContent))
	}

	// 查找模板 chunk 用于构造新的 SSE 事件
	var templateData string
	for _, chunk := range buffer {
		if chunk.content != "" && !chunk.isStop && !chunk.isUsage {
			templateData = strings.TrimPrefix(chunk.rawLine, "data: ")
			break
		}
	}

	if filteredContent != "" && templateData != "" {
		var parsed SSEChunk
		if err := json.Unmarshal([]byte(templateData), &parsed); err == nil {
			if len(parsed.Choices) > 0 {
				parsed.Choices[0].Delta.Content = filteredContent
				parsed.Choices[0].FinishReason = nil
				newData, err := json.Marshal(parsed)
				if err == nil {
					fmt.Fprintf(w, "data: %s\n\n", string(newData))
					flusher.Flush()
				}
			}
		}
	}

	// 发送 stop 和 usage 块
	for _, chunk := range buffer {
		if chunk.isStop || chunk.isUsage {
			fmt.Fprintf(w, "%s\n\n", chunk.rawLine)
			flusher.Flush()
		}
	}
}

// proxyRawResponse 直接透传上游响应
func (h *Handler) proxyRawResponse(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()

	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// maskKey 对 Authorization header 脱敏用于日志输出
func maskKey(authHeader string) string {
	key := strings.TrimPrefix(authHeader, "Bearer ")
	if len(key) <= 14 {
		return "***"
	}
	return key[:10] + "..." + key[len(key)-4:]
}
