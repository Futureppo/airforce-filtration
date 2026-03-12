package main

import (
	"log"
	"os"
	"strconv"
	"strings"
)

// Config 全局配置结构
type Config struct {
	ProxyURL     string   // HTTP 代理地址，例如 http://ip:port
	UpstreamURL  string   // 上游 API 地址
	ListenPort   string   // 监听端口
	MaxRetries   int      // 429 错误时最大重试次数
	AdKeywords   []string // 广告过滤关键词列表
	AdBufferSize int      // 流式响应尾部缓冲区大小（字符数）
}

// 默认广告关键词
var defaultAdKeywords = []string{
	"Need proxies cheaper than the market?",
	"op.wtf",
	"Upgrade your plan to remove this message",
	"discord.gg/airforce",
}

// LoadConfig 从环境变量加载配置
func LoadConfig() *Config {
	cfg := &Config{}

	cfg.ProxyURL = getEnv("PROXY_URL", "")
	cfg.UpstreamURL = getEnv("UPSTREAM_URL", "https://api.airforce")
	cfg.ListenPort = getEnv("LISTEN_PORT", "6777")

	maxRetries, err := strconv.Atoi(getEnv("MAX_RETRIES", "3"))
	if err != nil {
		maxRetries = 3
	}
	cfg.MaxRetries = maxRetries

	// 广告关键词（可自定义，用 | 分隔，留空则使用默认）
	adStr := getEnv("AD_KEYWORDS", "")
	if adStr != "" {
		for _, kw := range strings.Split(adStr, "|") {
			kw = strings.TrimSpace(kw)
			if kw != "" {
				cfg.AdKeywords = append(cfg.AdKeywords, kw)
			}
		}
	} else {
		cfg.AdKeywords = defaultAdKeywords
	}

	bufSize, err := strconv.Atoi(getEnv("AD_BUFFER_SIZE", "400"))
	if err != nil {
		bufSize = 400
	}
	cfg.AdBufferSize = bufSize

	log.Printf("[配置] 上游地址: %s", cfg.UpstreamURL)
	log.Printf("[配置] 监听端口: %s", cfg.ListenPort)
	log.Printf("[配置] 最大重试次数: %d", cfg.MaxRetries)
	if cfg.ProxyURL != "" {
		log.Printf("[配置] HTTP 代理: %s", cfg.ProxyURL)
	} else {
		log.Printf("[配置] HTTP 代理: 未配置（直连）")
	}
	log.Printf("[配置] 广告关键词: %v", cfg.AdKeywords)

	return cfg
}

// getEnv 获取环境变量，不存在则返回默认值
func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
