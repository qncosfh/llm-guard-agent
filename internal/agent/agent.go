package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	logMu       sync.Mutex
	jsonLogPath = filepath.Join("logs", "guard_log_"+time.Now().Format("20060102")+".json")
	jsonLogs    []map[string]string // 内存日志
)

// 日志结构体
func addLog(t, content, keyword, desc, ip string) {
	logMu.Lock()
	defer logMu.Unlock()
	os.MkdirAll("logs", 0755)
	row := map[string]string{
		"time":       time.Now().Format("2006-01-02 15:04:05"),
		"ip":         ip,
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

// 递归提取所有 http/https URL
func extractAllUrls(data interface{}) []string {
	var urls []string
	switch v := data.(type) {
	case map[string]interface{}:
		for _, val := range v {
			urls = append(urls, extractAllUrls(val)...)
		}
	case []interface{}:
		for _, item := range v {
			urls = append(urls, extractAllUrls(item)...)
		}
	case string:
		if strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://") {
			urls = append(urls, v)
		}
	}
	return urls
}

// 拉取 URL 内容并检测
func fetchAndDetect(url string) (bool, string, string) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false, "", "文件拉取失败"
	}
	defer resp.Body.Close()
	content, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB限制
	if err != nil {
		return false, "", "文件读取失败"
	}
	// 判断类型并提取文本
	var text string
	ct := resp.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "image/") {
		tmpFile := filepath.Join(os.TempDir(), "tmpimg")
		os.WriteFile(tmpFile, content, 0644)
		text, _ = multimodal.OCRImage(tmpFile)
		os.Remove(tmpFile)
	} else if strings.HasSuffix(url, ".pdf") || strings.HasSuffix(url, ".docx") || strings.HasSuffix(url, ".txt") {
		tmpFile := filepath.Join(os.TempDir(), "tmpdoc")
		os.WriteFile(tmpFile, content, 0644)
		text, _ = multimodal.ParseFile(tmpFile)
		os.Remove(tmpFile)
	} else {
		text = string(content)
	}
	keywords, descs := rules.MatchAllSlidingWindow(text, "input", 5, 1)
	if len(keywords) > 0 {
		return false, strings.Join(keywords, ", "), strings.Join(descs, ", ")
	}
	return true, "", ""
}

// 判断内容是否为系统 prompt
func isSystemPrompt(content string) bool {
	sysKeywords := []string{
		"### Task:", "### Guidelines:", "### Output:", "### Chat History:",
		"Generate a concise, 3-5 word title", "categorizing the main themes of the chat history",
	}
	for _, kw := range sysKeywords {
		if strings.Contains(content, kw) {
			return true
		}
	}
	return false
}

// 判断是否为 Dify 请求
func isDifyRequest(r *http.Request, reqMap map[string]interface{}) bool {
	ua := r.Header.Get("User-Agent")
	if strings.Contains(strings.ToLower(ua), "dify") {
		log.Printf("[DEBUG] isDifyRequest: User-Agent命中dify: %s", ua)
		return true
	}
	if reqMap != nil {
		if _, ok := reqMap["model_mode"]; ok {
			log.Printf("[DEBUG] isDifyRequest: model_mode命中")
			return true
		}
		if _, ok := reqMap["prompts"]; ok {
			log.Printf("[DEBUG] isDifyRequest: prompts命中")
			return true
		}
		if _, ok := reqMap["sys.query"]; ok {
			log.Printf("[DEBUG] isDifyRequest: sys.query命中")
			return true
		}
	}
	return false
}

// 模拟Dify流式SSE回复，多片分片+日志
func difyStreamReply(w http.ResponseWriter, tip string) {
	w.Header().Set("Content-Type", "text/event-stream")
	id := fmt.Sprintf("chatcmpl-%d", time.Now().UnixNano()%1000)
	model := config.Cfg.ModelName
	fingerprint := "fp_guard"
	created := time.Now().Unix()
	// 只发一片详细提示内容
	resp := map[string]interface{}{
		"id":                 id,
		"object":             "chat.completion.chunk",
		"created":            created,
		"model":              model,
		"system_fingerprint": fingerprint,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"delta":         map[string]interface{}{"role": "assistant", "content": tip},
				"finish_reason": nil,
			},
		},
	}
	b, _ := json.Marshal(resp)
	fmt.Fprintf(w, "data: %s\n\n", b)
	log.Printf("[模拟大模型回复] \033[31m%s\033[0m", tip)
	// 最后一片，finish_reason为stop
	final := map[string]interface{}{
		"id":                 id,
		"object":             "chat.completion.chunk",
		"created":            created,
		"model":              model,
		"system_fingerprint": fingerprint,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"delta":         map[string]interface{}{"role": "assistant", "content": ""},
				"finish_reason": "stop",
			},
		},
	}
	b, _ = json.Marshal(final)
	fmt.Fprintf(w, "data: %s\n\n", b)
	fmt.Fprintf(w, "data: [DONE]\n\n")
}

func ProxyHandler(modelURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := r.RemoteAddr
		bodyBytes, _ := io.ReadAll(r.Body)
		r.Body.Close()

		// 解析用户请求
		var req ChatRequest
		_ = json.Unmarshal(bodyBytes, &req)

		// 解析为 map 以便 URL 检测和 Dify 检测
		var reqMap map[string]interface{}
		json.Unmarshal(bodyBytes, &reqMap)
		isDify := isDifyRequest(r, reqMap)

		// 判断是否为流式请求
		var isStream bool
		if v, ok := reqMap["stream"]; ok {
			isStream, _ = v.(bool)
		}

		// 检测最后一条 user 或 system 消息（优先 user）
		var lastInputMsg *struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		for i := len(req.Messages) - 1; i >= 0; i-- {
			if req.Messages[i].Role == "user" {
				lastInputMsg = &req.Messages[i]
				break
			}
		}
		if lastInputMsg == nil {
			for i := len(req.Messages) - 1; i >= 0; i-- {
				if req.Messages[i].Role == "system" {
					lastInputMsg = &req.Messages[i]
					break
				}
			}
		}
		if lastInputMsg != nil {
			// 1. 先用Python ONNX服务检测
			reqBody, _ := json.Marshal(map[string]string{"text": lastInputMsg.Content})
			client := &http.Client{Timeout: 3 * time.Second}
			resp, err := client.Post(config.Cfg.OnnxURL, "application/json", bytes.NewBuffer(reqBody))
			blocked := false
			labels := []string{}
			if err == nil {
				defer resp.Body.Close()
				type OnnxResp struct {
					Blocked bool      `json:"blocked"`
					Labels  []string  `json:"labels"`
					Probs   []float32 `json:"probs"`
				}
				var result OnnxResp
				if err := json.NewDecoder(resp.Body).Decode(&result); err == nil {
					blocked = result.Blocked
					labels = result.Labels
				}
			}
			if blocked {
				descs := strings.Join(labels, ", ")
				logContent := strings.TrimSpace(lastInputMsg.Content)
				logContent = strings.TrimSuffix(logContent, "[]")
				logContent = strings.TrimRight(logContent, "\r\n")
				logContent = strings.TrimSpace(logContent)
				logContent = cleanForLog(lastInputMsg.Content)
				addLog("input", logContent, "onnx", descs, ip)
				cleanInput := logContent
				tip := fmt.Sprintf("⚠️[此消息为llm-guard回复]\r\n您的输入\"%s\"被内容安全护栏拦截，拦截类型为\"%s\"，请规范您的提问。", cleanInput, descs)
				if config.Cfg.Platform == "dify" {
					difyStreamReply(w, tip)
					return
				}
				response := map[string]interface{}{
					"id":      "chatcmpl-guarded",
					"object":  "chat.completion",
					"created": time.Now().Unix(),
					"model":   config.Cfg.ModelName,
					"choices": []map[string]interface{}{
						{
							"index": 0,
							"message": map[string]interface{}{
								"role":    "assistant",
								"content": tip,
							},
							"finish_reason": "stop",
						},
					},
					"text": tip,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(response)
				return
			}
			// 2. 再用rules.yaml兜底
			keywords, descs := rules.MatchAllSlidingWindow(lastInputMsg.Content, "input", 5, 1)
			if len(keywords) > 0 {
				if (lastInputMsg.Role == "user" || lastInputMsg.Role == "system") && !isSystemPrompt(lastInputMsg.Content) {
					// 日志内容也做清洗，防止日志中出现[]
					logContent := strings.TrimSpace(lastInputMsg.Content)
					logContent = strings.TrimSuffix(logContent, "[]")
					logContent = strings.TrimRight(logContent, "\r\n")
					logContent = strings.TrimSpace(logContent)
					addLog("input", logContent, strings.Join(keywords, ", "), strings.Join(descs, ", "), ip)
				}
				cleanInput := strings.TrimSpace(lastInputMsg.Content)
				cleanInput = strings.TrimSuffix(cleanInput, "[]")
				cleanInput = strings.TrimRight(cleanInput, "\r\n")
				cleanInput = strings.TrimSpace(cleanInput)
				tip := fmt.Sprintf("⚠️[此消息为llm-guard回复]\r\n您的输入\"%s\"被内容安全护栏拦截，拦截类型为\"%s\"，请规范您的提问。", cleanInput, strings.Join(descs, ", "))
				if config.Cfg.Platform == "dify" {
					difyStreamReply(w, tip)
					return
				}
				response := map[string]interface{}{
					"id":      "chatcmpl-guarded",
					"object":  "chat.completion",
					"created": time.Now().Unix(),
					"model":   config.Cfg.ModelName,
					"choices": []map[string]interface{}{
						{
							"index": 0,
							"message": map[string]interface{}{
								"role":    "assistant",
								"content": tip,
							},
							"finish_reason": "stop",
						},
					},
					"text": tip,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(response)
				return
			}
		}

		// 递归检测所有 URL 指向的内容（每次用本次请求内容）
		urls := extractAllUrls(reqMap)
		for _, url := range urls {
			ok, keywords, descs := fetchAndDetect(url)
			if !ok {
				addLog("input", url, keywords, descs, ip)
				tip := fmt.Sprintf("⚠️[此消息为llm-guard回复]\r\n您的输入文件\"%s\"被内容安全护栏拦截，拦截类型为\"%s\"，请规范您的提问。", url, descs)
				if config.Cfg.Platform == "dify" {
					log.Printf("[模拟大模型回复] \033[31m%s\033[0m", tip)
					difyStreamReply(w, tip)
					return
				}
				response := map[string]interface{}{
					"id":      "chatcmpl-guarded",
					"object":  "chat.completion",
					"created": time.Now().Unix(),
					"model":   config.Cfg.ModelName,
					"choices": []map[string]interface{}{
						{
							"index": 0,
							"message": map[string]interface{}{
								"role":    "assistant",
								"content": tip,
							},
							"finish_reason": "stop",
						},
					},
					"text": tip,
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(response)
				return
			}
		}

		log.Printf("[代理转发] \033[32mIP:%s 请求转发到模型: %s\033[0m", ip, modelURL)
		client := &http.Client{}

		var reqModel *http.Request
		if isDify && !isStream {
			// Dify 非流式请求，移除 stream 字段，强制大模型返回非流式 JSON
			delete(reqMap, "stream")
			newBody, _ := json.Marshal(reqMap)
			reqModel, _ = http.NewRequest("POST", config.Cfg.ModelURL, bytes.NewReader(newBody))
		} else {
			reqModel, _ = http.NewRequest("POST", config.Cfg.ModelURL, bytes.NewReader(bodyBytes))
		}
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

		// 打印大模型返回的完整原始数据，便于调试
		//log.Printf("[大模型原始返回] %s", string(outputBytes))

		if isStream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Write(outputBytes)
			return
		}

		// 只检测模型 message.content 字段（每次用本次响应内容）
		var respObj struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		raw := string(outputBytes)
		if strings.HasPrefix(raw, "data:") {
			// 兼容 stream: true
			lines := strings.Split(raw, "\n")
			var lastData string
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "data:") {
					data := strings.TrimPrefix(line, "data:")
					data = strings.TrimSpace(data)
					if data != "[DONE]" && data != "" {
						lastData = data
					}
				}
			}
			if lastData != "" {
				raw = lastData
			}
		}
		err = json.Unmarshal([]byte(raw), &respObj)
		if err != nil {
			// 不是标准 JSON，封装成标准格式
			tip := fmt.Sprintf("⚠️[此消息为llm-guard回复]\r\n后端返回内容格式异常：%s", string(outputBytes))
			if config.Cfg.Platform == "dify" {
				log.Printf("[模拟大模型回复] \033[31m%s\033[0m", tip)
				difyStreamReply(w, tip)
				return
			}
			response := map[string]interface{}{
				"id":      "chatcmpl-guarded",
				"object":  "chat.completion",
				"created": time.Now().Unix(),
				"model":   config.Cfg.ModelName,
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": tip,
						},
						"finish_reason": "stop",
					},
				},
				"text": tip,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}
		var outputText string
		for _, c := range respObj.Choices {
			outputText += c.Message.Content
		}

		keywords, descs := rules.MatchAllSlidingWindow(outputText, "output", 5, 1)

		if len(keywords) > 0 {
			addLog("output", outputText, strings.Join(keywords, ", "), strings.Join(descs, ", "), ip)
			tip := fmt.Sprintf("⚠️[此消息为llm-guard回复]\r\n模型输出内容\"%s\"被内容安全护栏拦截，拦截类型为\"%s\"，请规范您的提问。", outputText, strings.Join(descs, ", "))
			if config.Cfg.Platform == "dify" {
				log.Printf("[模拟大模型回复] \033[31m%s\033[0m", tip)
				difyStreamReply(w, tip)
				return
			}
			response := map[string]interface{}{
				"id":      "chatcmpl-guarded",
				"object":  "chat.completion",
				"created": time.Now().Unix(),
				"model":   config.Cfg.ModelName,
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"message": map[string]interface{}{
							"role":    "assistant",
							"content": tip,
						},
						"finish_reason": "stop",
					},
				},
				"text": tip,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(response)
			return
		}

		log.Printf("[代理成功] \033[32mIP:%s 返回模型响应\033[0m", ip)
		w.Header().Set("Content-Type", "application/json")
		w.Write(outputBytes)
	}
}

// 文件上传接口，检测文件名和内容---✨✨✨✨✨✨✨只是为了方便测试用！实际对接dify、n8n、openwebui时 并不是通过/v1/upload接口
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
		addLog("filename", filename, keyword, desc, r.RemoteAddr)
		tip := "⚠️[此消息为llm-guard回复]\r\n您的文件名被内容安全护栏拦截，拦截类型为：" + desc + "，请规范文件命名。"
		if config.Cfg.Platform == "dify" {
			log.Printf("[模拟大模型回复] \033[31m%s\033[0m", tip)
			difyStreamReply(w, tip)
			return
		}
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
		addLog("input", contentWithFile, strings.Join(keywords, ", "), strings.Join(descs, ", "), r.RemoteAddr)
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
func TransparentProxyHandler(modelURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// 拼接目标 URL
		targetURL := modelURL + r.URL.Path
		if r.URL.RawQuery != "" {
			targetURL += "?" + r.URL.RawQuery
		}
		// 复制请求体
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		req, _ := http.NewRequest(r.Method, targetURL, bytes.NewReader(body))
		for k, v := range r.Header {
			for _, vv := range v {
				req.Header.Add(k, vv)
			}
		}
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			http.Error(w, "代理失败", 500)
			return
		}
		defer resp.Body.Close()
		for k, v := range resp.Header {
			for _, vv := range v {
				w.Header().Add(k, vv)
			}
		}
		w.WriteHeader(resp.StatusCode)
		io.Copy(w, resp.Body)
	}
}
func cleanForLog(input string) string {

	if strings.HasPrefix(input, "### Task: Generate a concise") {
		return "[openwebui自动摘要prompt已过滤]"
	}
	// 其它自定义清洗逻辑
	return input
}
