package agent

import (
	"bytes"
	"encoding/json"
	"io"
	"llm-guard-agent/internal/config"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"llm-guard-agent/internal/multimodal"
	"llm-guard-agent/internal/rules"
)

type ChatRequest struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

var (
	//logBuffer   []string
	logMu sync.Mutex
	//logFilePath = "internal/web/templates/index.html"
	//logHeader   = "<!DOCTYPE html><html><head><title>拦截日志</title></head><body><h1>拦截日志</h1><table border='1'><tr><th>时间</th><th>护栏类型</th><th>类型</th><th>内容</th><th>关键词</th></tr>"
	logFooter   = "</table></body></html>"
	jsonLogPath = filepath.Join("logs", "guard_log_"+time.Now().Format("20060102")+".json")
	jsonLogs    []map[string]string // 内存日志
)

// 日志结构体
func addLog(t, content, keyword, desc string) {
	logMu.Lock()
	defer logMu.Unlock()
	os.MkdirAll("logs", 0755)
	row := map[string]string{
		"time":       time.Now().Format("2006-01-02 15:04:05"),
		"guard_type": desc,
		"type":       t,
		"content":    content,
		"keyword":    keyword,
	}
	jsonLogs = append(jsonLogs, row)
	f, _ := os.OpenFile(jsonLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	enc := json.NewEncoder(f)
	enc.Encode(row)
	f.Close()
}

// 启动时加载历史json日志
func LoadJsonLogs() {
	os.MkdirAll("logs", 0755) // 确保 logs 目录存在
	jsonLogs = nil
	f, err := os.Open(jsonLogPath)
	if err != nil {
		return
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	for {
		var row map[string]string
		if err := dec.Decode(&row); err != nil {
			break
		}
		jsonLogs = append(jsonLogs, row)
	}
}

// API: 查询全部日志
func LogsAPIHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	logMu.Lock()
	defer logMu.Unlock()
	json.NewEncoder(w).Encode(jsonLogs)
}

func ProxyHandler(modelURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		bodyBytes, _ := io.ReadAll(r.Body)
		r.Body.Close()

		// 解析用户请求
		var req ChatRequest
		_ = json.Unmarshal(bodyBytes, &req)

		// 拦截输入内容
		for _, msg := range req.Messages {
			if msg.Role == "user" {
				keywords, descs := rules.MatchAllSlidingWindow(msg.Content, "input", 5, 1)
				if len(keywords) > 0 {
					addLog("input", msg.Content, strings.Join(keywords, ", "), strings.Join(descs, ", "))
					log.Printf("[拦截][输入] IP:%s  内容:%s 关键词:%s", ip, msg.Content, strings.Join(keywords, ", "))
					response := map[string]string{
						"error": "您提出的问题违规：关键词为：" + strings.Join(keywords, ", ") + "；类型为：" + strings.Join(descs, ", "),
					}
					w.Header().Set("Content-Type", "application/json")
					json.NewEncoder(w).Encode(response)
					return
				}
			}
		}

		log.Printf("[代理转发] IP:%s 请求转发到模型: %s", ip, modelURL)
		client := &http.Client{}
		reqModel, _ := http.NewRequest("POST", config.Cfg.ModelURL, bytes.NewReader(bodyBytes))
		reqModel.Header.Set("Content-Type", "application/json")
		if config.Cfg.ApiKey != "" {
			reqModel.Header.Set("Authorization", "Bearer "+config.Cfg.ApiKey)
		}
		resp, err := client.Do(reqModel)
		if err != nil {
			log.Printf("[代理失败] %v", err)
			http.Error(w, "模型接口异常", 500)
			return
		}
		defer resp.Body.Close()

		outputBytes, _ := io.ReadAll(resp.Body)

		// 只检测模型 message.content 字段
		var respObj struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		_ = json.Unmarshal(outputBytes, &respObj)
		var outputText string
		for _, c := range respObj.Choices {
			outputText += c.Message.Content
		}
		keywords, descs := rules.MatchAllSlidingWindow(outputText, "output", 5, 1)
		if len(keywords) > 0 {
			addLog("output", outputText, strings.Join(keywords, ", "), strings.Join(descs, ", "))
			log.Printf("[拦截][输出] 内容:%s 关键词:%s", outputText, strings.Join(keywords, ", "))
			response := map[string]string{
				"error": "模型输出违规：关键词为：" + strings.Join(keywords, ", ") + "；类型为：" + strings.Join(descs, ", "),
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		log.Printf("[代理成功] IP:%s 返回模型响应", ip)
		w.Header().Set("Content-Type", "application/json")
		w.Write(outputBytes)
	}
}

// 文件上传接口，检测文件名和内容
func UploadHandler(w http.ResponseWriter, r *http.Request) {
	r.ParseMultipartForm(32 << 20)
	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "文件上传失败", 400)
		return
	}
	defer file.Close()
	filename := header.Filename

	// 文件名规则检测
	if keyword, ok := rules.Match(filename, "filename"); ok {
		desc := rules.GetDescription("filename", keyword)
		addLog("filename", filename, keyword, desc)
		http.Error(w, "文件名违规："+keyword, 403)
		return
	}

	// 跨平台临时目录
	tmpDir := os.TempDir()
	tmpPath := filepath.Join(tmpDir, filename)
	out, _ := os.Create(tmpPath)
	io.Copy(out, file)
	out.Close()

	// 文件内容提取（仅图片和文档示例）
	var text string
	if isImage(filename) {
		text, _ = multimodal.OCRImage(tmpPath)
	} else if isDoc(filename) {
		text, _ = multimodal.ParseFile(tmpPath)
	}

	// 文件内容规则检测
	keywords, descs := rules.MatchAllSlidingWindow(text, "input", 5, 1)
	if len(keywords) > 0 {
		os.Remove(tmpPath)
		contentWithFile := "[文件:" + filename + "] " + text
		addLog("input", contentWithFile, strings.Join(keywords, ", "), strings.Join(descs, ", "))
		http.Error(w, "文件内容违规：关键词为："+strings.Join(keywords, ", ")+"；类型为："+strings.Join(descs, ", "), 403)
		return
	}

	// 检测通过，组装OpenAI标准消息体并转发
	fileURL := "http://127.0.0.1:8888/tmp/" + filename
	openaiReq := map[string]interface{}{
		"model": config.Cfg.ModelName,
		"messages": []map[string]interface{}{
			{
				"role": "user",
				"content": []map[string]interface{}{
					{"type": "file_url", "file_url": map[string]interface{}{"url": fileURL}},
				},
			},
		},
	}
	buf, _ := json.Marshal(openaiReq)
	client := &http.Client{}
	req, _ := http.NewRequest("POST", config.Cfg.ModelURL, bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	if config.Cfg.ApiKey != "" {
		req.Header.Set("Authorization", "Bearer "+config.Cfg.ApiKey)
	}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "大模型接口异常", 500)
		return
	}
	defer resp.Body.Close()
	outputBytes, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.Write(outputBytes)
}

func isImage(filename string) bool {
	f := strings.ToLower(filename)
	return strings.HasSuffix(f, ".jpg") || strings.HasSuffix(f, ".jpeg") || strings.HasSuffix(f, ".png")
}
func isDoc(filename string) bool {
	f := strings.ToLower(filename)
	return strings.HasSuffix(f, ".pdf") || strings.HasSuffix(f, ".docx") || strings.HasSuffix(f, ".txt") || strings.HasSuffix(f, ".md")
}
func DeleteLogsAPIHandler(w http.ResponseWriter, r *http.Request) {
	var req []map[string]string
	json.NewDecoder(r.Body).Decode(&req)
	logMu.Lock()
	defer logMu.Unlock()
	// 只保留未被选中的日志
	keep := func(row map[string]string) bool {
		for _, del := range req {
			if row["time"] == del["time"] && row["content"] == del["content"] {
				return false
			}
		}
		return true
	}
	// 内存日志
	newLogs := []map[string]string{}
	for _, row := range jsonLogs {
		if keep(row) {
			newLogs = append(newLogs, row)
		}
	}
	jsonLogs = newLogs
	// 覆盖写回日志文件
	f, _ := os.OpenFile(jsonLogPath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0644)
	for _, row := range jsonLogs {
		enc := json.NewEncoder(f)
		enc.Encode(row)
	}
	f.Close()
	w.WriteHeader(http.StatusOK)
}
