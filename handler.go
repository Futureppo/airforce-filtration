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
	"sync/atomic"
	"time"
)

type Handler struct {
	config *Config
	client *http.Client
}

func NewHandler(cfg *Config) *Handler {
	transport := &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: false},
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 100,
		IdleConnTimeout:     90 * time.Second,
	}

	if len(cfg.Proxies) > 0 {
		var proxyIndex uint64
		transport.Proxy = func(req *http.Request) (*url.URL, error) {
			idx := atomic.AddUint64(&proxyIndex, 1)
			proxyURL := cfg.Proxies[(idx-1)%uint64(len(cfg.Proxies))]
			return proxyURL, nil
		}
		log.Printf("[代理] 已启用 HTTP 代理池，共 %d 个节点", len(cfg.Proxies))
	} else {
		log.Printf("[代理] 未配置代理，将直连")
	}

	return &Handler{
		config: cfg,
		client: &http.Client{
			Transport: transport,
			Timeout:   5 * time.Minute,
		},
	}
}

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

	upstreamURL := fmt.Sprintf("%s/v1/chat/completions", strings.TrimRight(h.config.UpstreamURL, "/"))
	log.Printf("[请求] 转发到上游 %s | Key: %s | 流式: %v | Body大小: %d",
		upstreamURL, keyDisplay, isStream, len(body))
	upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		log.Printf("[错误] 构造上游请求失败: %v", err)
		http.Error(w, `{"error": {"message": "Failed to create upstream request", "type": "server_error"}}`, http.StatusInternalServerError)
		return
	}

	upReq.Header.Set("Content-Type", "application/json")
	upReq.Header.Set("Authorization", authHeader)
	upReq.Header.Set("Accept", "*/*")

	reqStart := time.Now()
	resp, err := h.client.Do(upReq)
	reqDuration := time.Since(reqStart)
	if err != nil {
		log.Printf("[错误] 上游请求失败 (耗时 %v): %v", reqDuration, err)
		http.Error(w, fmt.Sprintf(`{"error": {"message": "Upstream request failed: %s", "type": "upstream_error"}}`, err), http.StatusBadGateway)
		return
	}
	log.Printf("[响应] 上游返回 | 状态码: %d | 耗时: %v | Content-Type: %s",
		resp.StatusCode, reqDuration, resp.Header.Get("Content-Type"))

	// 429 直接透传
	if resp.StatusCode == http.StatusTooManyRequests {
		log.Printf("[限流] Key %s 被上游限流 429", keyDisplay)
		h.proxyRawResponse(w, resp)
		return
	}

	// 非200直接透传
	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Printf("[上游错误] 状态码: %d | 响应体: %s", resp.StatusCode, truncateForLog(string(errBody), 500))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		w.Write(errBody)
		return
	}

	// 读取响应体检测软限流
	respBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		log.Printf("[错误] 读取上游响应体失败: %v", err)
		http.Error(w, `{"error": {"message": "Failed to read upstream response", "type": "server_error"}}`, http.StatusBadGateway)
		return
	}

	if isRateLimitBody(string(respBody)) {
		log.Printf("[限流] Key %s 内容级限流，返回429 | 原始响应: %s", keyDisplay, truncateForLog(string(respBody), 200))
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		fmt.Fprintf(w, `{"error": {"message": "Rate limit exceeded (upstream soft limit)", "type": "rate_limit_error"}}`)
		return
	}

	// 重建 resp.Body 供后续处理
	resp.Body = io.NopCloser(bytes.NewReader(respBody))

	if isStream {
		h.handleStreamResponse(w, resp)
	} else {
		h.handleNonStreamResponse(w, resp)
	}
}

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

type SSEChunk struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []SSEChoice `json:"choices"`
	Usage   *SSEUsage   `json:"usage,omitempty"`
}

type SSEChoice struct {
	Index        int      `json:"index"`
	Delta        SSEDelta `json:"delta"`
	FinishReason *string  `json:"finish_reason"`
}

type SSEDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type SSEUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type bufferedChunk struct {
	rawLine    string
	content    string
	contentLen int
	isStop     bool
	isUsage    bool
}

// handleStreamResponse 使用尾部缓冲策略过滤流式响应中的广告
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
		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

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
	} else {
		log.Printf("[警告] 流式响应异常结束，未收到 [DONE] 标记，缓冲区剩余 %d 个chunk", len(buffer))
	}

	if len(buffer) > 0 {
		log.Printf("[清理] 刷新剩余缓冲区: %d 个chunk", len(buffer))
		h.flushFilteredBuffer(w, flusher, buffer)
	}
}

func parseSSEChunk(rawLine, data string) bufferedChunk {
	chunk := bufferedChunk{rawLine: rawLine}

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

	for _, chunk := range buffer {
		if chunk.isStop || chunk.isUsage {
			fmt.Fprintf(w, "%s\n\n", chunk.rawLine)
			flusher.Flush()
		}
	}
}

func (h *Handler) proxyRawResponse(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[错误] 读取上游原始响应失败: %v", err)
		http.Error(w, `{"error": {"message": "Failed to read upstream response", "type": "server_error"}}`, http.StatusBadGateway)
		return
	}
	log.Printf("[转发] 原始响应 | 状态码: %d | 大小: %d | 内容: %s",
		resp.StatusCode, len(body), truncateForLog(string(body), 500))
	for key, values := range resp.Header {
		for _, v := range values {
			w.Header().Add(key, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	w.Write(body)
}

func maskKey(authHeader string) string {
	key := strings.TrimPrefix(authHeader, "Bearer ")
	if len(key) <= 14 {
		return "***"
	}
	return key[:10] + "..." + key[len(key)-4:]
}

func truncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...(truncated)"
}

// 软限流关键词，命中任一则判定为限流响应
var rateLimitKeywords = []string{
	"Ratelimit Exceeded",
	"rate limit exceeded",
	"Too many requests",
}

func isRateLimitBody(body string) bool {
	lower := strings.ToLower(body)
	for _, kw := range rateLimitKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}
