package main

import (
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
)


type Config struct {
	Proxies      []*url.URL
	UpstreamURL  string
	ListenPort   string
	AdKeywords   []string
	AdBufferSize int
}

var defaultAdKeywords = []string{
	"Need proxies cheaper than the market?",
	"op.wtf",
	"Upgrade your plan to remove this message",
	"discord.gg/airforce",
	"api.airforce",
}

func LoadConfig() *Config {
	cfg := &Config{}

	cfg.Proxies = loadProxies()
	cfg.UpstreamURL = getEnv("UPSTREAM_URL", "https://api.airforce")
	cfg.ListenPort = getEnv("LISTEN_PORT", "6777")



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

	log.Printf("[配置] 上游: %s | 端口: %s | 代理池数量: %d",
		cfg.UpstreamURL, cfg.ListenPort, len(cfg.Proxies))
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

func loadProxies() []*url.URL {
	var proxyList []*url.URL
	
	// 支持兼容老配置
	envProxy := getEnv("PROXIES", getEnv("PROXY_URL", ""))
	if envProxy == "" {
		return proxyList
	}

	rawProxies := strings.Split(envProxy, ",")
	for _, rp := range rawProxies {
		rp = strings.TrimSpace(rp)
		if rp != "" {
			u, err := url.Parse(rp)
			if err != nil {
				log.Printf("[警告] 代理地址解析失败: %s - %v", rp, err)
			} else {
				proxyList = append(proxyList, u)
			}
		}
	}
	return proxyList
}
