package main

import (
	"log"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	ProxyURL     string
	UpstreamURL  string
	ListenPort   string
	MaxRetries   int
	AdKeywords   []string
	AdBufferSize int
}

var defaultAdKeywords = []string{
	"Need proxies cheaper than the market?",
	"op.wtf",
	"Upgrade your plan to remove this message",
	"discord.gg/airforce",
}

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

	// 广告关键词可通过 AD_KEYWORDS 环境变量自定义，用 | 分隔
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

	log.Printf("[配置] 上游: %s | 端口: %s | 重试: %d | 代理: %s",
		cfg.UpstreamURL, cfg.ListenPort, cfg.MaxRetries, orDefault(cfg.ProxyURL, "直连"))
	log.Printf("[配置] 广告关键词: %v", cfg.AdKeywords)

	return cfg
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func orDefault(val, def string) string {
	if val == "" {
		return def
	}
	return val
}
