package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.Println("Airforce Filtration Proxy 启动中...")

	// 优先使用可执行文件所在目录的 .env
	envFile := ".env"
	if exe, err := os.Executable(); err == nil {
		altEnvFile := filepath.Join(filepath.Dir(exe), ".env")
		if _, err := os.Stat(altEnvFile); err == nil {
			envFile = altEnvFile
		}
	}
	if err := godotenv.Load(envFile); err != nil {
		log.Printf("[配置] 未找到 .env 文件，使用环境变量: %v", err)
	} else {
		log.Printf("[配置] 已加载 .env 文件: %s", envFile)
	}

	cfg := LoadConfig()
	handler := NewHandler(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", handler.HandleChatCompletions)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status": "ok", "service": "airforce-filtration-proxy"}`))
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"service": "airforce-filtration-proxy", "version": "1.0.0", "endpoints": ["/v1/chat/completions", "/health"]}`))
	})

	wrappedMux := corsMiddleware(loggingMiddleware(mux))

	addr := "0.0.0.0:" + cfg.ListenPort
	log.Printf("[启动] 服务监听 %s | 模式: 透传 Key", addr)

	if err := http.ListenAndServe(addr, wrappedMux); err != nil {
		log.Fatalf("[致命] 服务启动失败: %v", err)
	}
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("[访问] %s %s | 来源: %s", r.Method, r.URL.Path, r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
