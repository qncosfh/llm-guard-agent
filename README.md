# LLM Guard Agent

大模型护栏（LLM Guard Agent）是一个用于大语言模型（如 OpenAI、DeepSeek 等）API 接口的内容安全代理，支持多模态输入（文本、图片、文件），可灵活配置敏感词规则，具备现代化日志大屏和二次开发友好性。

:high_brightness:<mark>这里只是给了一个思路。</mark>

---

## 设计理念

- **安全合规**：拦截和过滤用户输入、模型输出、文件上传等多种内容，防止敏感信息泄露或违规内容生成。
- **多模态支持**：支持文本、~~图片（OCR）~~、文档（PDF、TXT、MD、~~DOCX~~）等多种输入类型。
- **高可扩展性**：规则可通过 YAML~~/JSON/数据库~~灵活配置，易于二次开发和集成。
- **现代化运维**：内置大屏风格 Web 日志分析页面，支持日志导入、导出、删除、分页、统计等。

---

## 主要功能

- **OpenAI/DeepSeek API 兼容代理**：/v1/chat/completions 代理，支持多模态消息体。
- **文件上传检测**：/v1/upload 支持文件名和内容检测，自动转发到大模型。
- **敏感词规则系统**：支持关键词、正则表达式，规则热加载。
- **日志持久化与分析**：JSON 日志文件，支持 Web 大屏分析、导入导出、批量删除。
- **现代化前端**：ECharts+Bootstrap 大屏，支持多选、分页、趋势分析、类型分布等。

---

## 代码结构

```
llm-guard-agent/
├── cmd/                # 启动入口 main.go
├── config/             # 配置文件（config.yaml、rules.yaml）
├── internal/
│   ├── agent/          # 主要业务逻辑（代理、上传、日志、API）
│   ├── config/         # 配置加载
│   ├── multimodal/     # 多模态内容解析（OCR、文件解析）
│   ├── rules/          # 规则系统（关键词、正则、描述）
│   └── web/templates/  # 前端页面（index.html）
├── logs/               # JSON 日志文件
├── go.mod/go.sum       # Go 依赖
```

---

## 二次开发建议

1. **扩展规则系统**

- 支持更多规则类型（如 IP、用户ID、上下文等）。
- 支持规则热加载、数据库存储。

2. **多模态能力增强**

- 集成更强的 OCR、音频转文本、图片内容识别等。
- 支持更多文件格式。

3. **API 扩展**

- 增加更多管理接口（如规则管理、日志检索、批量导入导出等）。
- 支持多模型路由、负载均衡。

4. **前端大屏扩展**

- 增加多天日志分析、更多可视化图表、权限管理等。
- 支持自定义看板、告警推送。

5. **高可用与安全**

- 支持 HTTPS、认证、限流、审计等。
- 支持分布式部署。

---

## 快速启动

1. 安装依赖

```bash
go mod tidy
```

2. 配置规则和参数

- 编辑 `config/config.yaml`、`config/rules.yaml`

  ![image-20250701163919618](/assert/image-20250701163919618.png)

  ![image-20250701163950284](/assert/image-20250701163950284.png)

  

3. 启动服务

```bash
go run cmd/main.go
```

![image-20250701164037368](/assert/image-20250701164037368.png)



4. 访问 Web 日志大屏

- http://127.0.0.1:8888/index.html

  ![image-20250701164146473](/assert/image-20250701164146473.png)

  

---

## 接口说明

- **/v1/chat/completions**：OpenAI 兼容多模态代理

  ![image-20250701164215894](/assert/image-20250701164215894.png)

  ![image-20250701164230296](/assert/image-20250701164230296.png)
  ![image-20250701164230296](/assert/WechatIMG8057.jpg)

  

- **/v1/upload**：文件上传检测接口，检测通过后封装标准的openai数据结构发送至本地模型

  ![image-20250701164358804](/assert/image-20250701164358804.png)

  

- **/api/logs**：获取全部拦截日志（JSON）

  ![image-20250701164421309](/assert/image-20250701164421309.png)

  ![image-20250701164456792](/assert/image-20250701164456792.png)

  

- **/api/delete_logs**：批量删除日志

  ![image-20250701164721916](/assert/image-20250701164721916.png)

  

---



# LLM Guard Agent 后端实现逻辑说明

---

## 1. 主要流程

### 1.1 启动流程

1. 加载配置文件（config.yaml），包括监听端口、模型地址、API Key、规则文件路径等。
2. 加载敏感词规则（rules.yaml），支持关键词、正则、描述等。
3. 加载历史 JSON 日志文件到内存。
4. 注册 API 路由，包括：
   - `/v1/chat/completions`：OpenAI 兼容代理
   - `/v1/upload`：文件上传检测
   - `/api/logs`：日志查询
   - `/api/delete_logs`：日志删除
   - 静态文件服务（前端页面、大屏）

### 1.2 用户请求拦截与检测

- **文本输入检测**：
  1. 用户通过 `/v1/chat/completions` 提交消息。
  2. 后端对每条 user 消息内容进行敏感词检测（滑动窗口，支持多关键词、正则）。
  3. 命中规则则拦截，记录日志并返回标准错误响应。
  4. 未命中则转发到大模型 API，返回模型响应。
  5. 对模型输出内容再次检测，命中则拦截并记录。

- **文件上传检测**：
  1. 用户通过 `/v1/upload` 上传文件。
  2. 检查文件名是否违规。
  3. 提取文件内容（支持文本、图片 OCR、音频 ASR、文档解析等）。
  4. 对提取出的文本内容进行敏感词检测。
  5. 命中规则则拦截，记录日志并返回错误。
  6. 检测通过则自动组装 OpenAI 标准消息体，POST 到本地大模型 `/v1/chat/completions`，返回模型响应。

---

## 2. 关键模块说明

### 2.1 agent.go

- 主要业务逻辑，包括 API 路由、代理、上传、日志、规则检测等。
- 日志采用 JSON 文件持久化，支持内存与文件同步。
- 敏感词检测采用滑动窗口算法，支持多关键词、正则表达式。
- 文件上传自动识别类型，调用 multimodal 进行内容提取。

### 2.2 rules.go

- 规则系统，支持关键词、正则、描述。
- 支持 YAML 配置，便于扩展和热加载。
- 提供 Match、MatchAllSlidingWindow 等检测方法。

### 2.3 multimodal/

- 多模态内容解析，包括图片 OCR、音频转文本、文档解析等。
- 可扩展对更多文件类型和识别方式的支持。

### 2.4 config.go

- 配置加载，支持监听端口、模型地址、API Key、规则文件路径等。

### 2.5 日志系统

- 日志以 JSON 行格式写入 logs/guard_log_日期.json。
- 支持 Web API 查询、批量删除、导入导出。
- 前端大屏通过 /api/logs 获取数据，支持多选、分页、趋势分析等。

---

## 3. 典型接口调用流程

- **文本代理**：
  - 用户 → `/v1/chat/completions` → [输入检测] → [大模型] → [输出检测] → 用户
- **文件上传**：
  - 用户 → `/v1/upload` → [文件名/内容检测] → [组装消息体] → `/v1/chat/completions` → 用户
- **日志管理**：
  - 前端 → `/api/logs` 获取全部日志
  - 前端 → `/api/delete_logs` 批量删除日志

---

## 4. 二次开发建议

- 可扩展更多规则类型、检测算法、文件格式。
- 可对接更多大模型 API、支持多模型路由。
- 可扩展前端大屏，支持多天日志、更多可视化、权限管理等。
- 支持分布式部署、认证、限流、审计等企业级能力。

---

