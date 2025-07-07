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

	// 注册代理路由，兼容 OpenAI 接口标准
	http.HandleFunc("/v1/chat/completions", agent.ProxyHandler(config.Cfg.ModelURL))
	http.HandleFunc("/v1/upload", agent.UploadHandler)

	// 其它 /v1/* 路径全部透明转发
	http.HandleFunc("/v1/", agent.TransparentProxyHandler(config.Cfg.ModelURL))
	agent.LoadJsonLogs()
	http.HandleFunc("/api/delete_logs", agent.DeleteLogsAPIHandler)

	http.HandleFunc("/api/logs", agent.LogsAPIHandler)

	// 读取 Listen 和 WebListen 配置
	go func() {
		log.Printf("Web日志页面监听: http://0.0.0.0%s/index.html", config.Cfg.WebListen)
		http.Handle("/", http.FileServer(http.Dir("internal/web/templates")))
		if err := http.ListenAndServe(config.Cfg.WebListen, nil); err != nil {
			log.Fatal("Web服务启动失败:", err)
		}
	}()

	log.Printf("大模型护栏 Agent 运行中，监听地址 0.0.0.0%s，代理目标: %s，已加载规则数: %d", config.Cfg.Listen, config.Cfg.ModelURL, len(rules.AllRules()))
	if err := http.ListenAndServe(config.Cfg.Listen, nil); err != nil {
		log.Fatal("服务启动失败:", err)
	}
}
