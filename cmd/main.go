package main

import (
	"log"
	"net/http"

	"llm-guard-agent/internal/agent"
	"llm-guard-agent/internal/config"
	"llm-guard-agent/internal/rules"
)

func main() {
	// 加载配置文件
	if err := config.LoadConfig("config/config.yaml"); err != nil {
		log.Fatal("配置加载失败:", err)
	}

	// 加载规则文件
	if err := rules.LoadFromYAML("config/rules.yaml"); err != nil {
		log.Fatal("加载规则文件失败:", err)
	}

	log.Printf("大模型护栏 Agent 运行中，监听地址 localhost%s，代理目标: %s，已加载规则数: %d", config.Cfg.Listen, config.Cfg.ModelURL, len(rules.AllRules()))

	// 注册代理路由，兼容 OpenAI 接口标准
	http.HandleFunc("/v1/chat/completions", agent.ProxyHandler(config.Cfg.ModelURL))
	http.HandleFunc("/v1/upload", agent.UploadHandler)
	agent.LoadJsonLogs()
	http.HandleFunc("/api/delete_logs", agent.DeleteLogsAPIHandler)

	http.HandleFunc("/api/logs", agent.LogsAPIHandler)

	go func() {
		log.Println("Web日志页面监听: http://127.0.0.1:8888/index.html")
		http.Handle("/", http.FileServer(http.Dir("internal/web/templates")))
		http.ListenAndServe(":8888", nil)
	}()

	if err := http.ListenAndServe(config.Cfg.Listen, nil); err != nil {
		log.Fatal("服务启动失败:", err)
	}
}
