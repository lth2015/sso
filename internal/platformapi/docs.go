package platformapi

import (
	"encoding/json"
	contract "github.com/lth2015/sso/api"
	"html/template"
	"net/http"
	"sort"
	"strings"
)

// expandSchema produces a self-contained display; it does not execute schemas.
func expandSchema(value any, spec map[string]any, depth int) any {
	if depth > 24 {
		return value
	}
	switch item := value.(type) {
	case map[string]any:
		if reference, ok := item["$ref"].(string); ok && strings.HasPrefix(reference, "#/") {
			var target any = spec
			for _, part := range strings.Split(reference[2:], "/") {
				object, ok := target.(map[string]any)
				if !ok {
					return value
				}
				target = object[part]
			}
			return expandSchema(target, spec, depth+1)
		}
		result := map[string]any{}
		for key, child := range item {
			result[key] = expandSchema(child, spec, depth+1)
		}
		return result
	case []any:
		result := make([]any, len(item))
		for i, child := range item {
			result[i] = expandSchema(child, spec, depth+1)
		}
		return result
	default:
		return value
	}
}
func docs(w http.ResponseWriter, r *http.Request) {
	var spec map[string]any
	if json.Unmarshal(contract.PublicJSON, &spec) != nil {
		problem(w, 503, "temporarily_unavailable")
		return
	}
	type route struct{ Method, Path, Summary, ID, Description, Authorization, Parameters, Request, Responses string }
	routes := []route{}
	paths := spec["paths"].(map[string]any)
	names := []string{}
	for path := range paths {
		if strings.HasPrefix(path, "/v1/") {
			names = append(names, path)
		}
	}
	sort.Strings(names)
	display := func(value any) string {
		if value == nil {
			return "无"
		}
		data, _ := json.MarshalIndent(expandSchema(value, spec, 0), "", "  ")
		return string(data)
	}
	for _, path := range names {
		methods := paths[path].(map[string]any)
		for _, method := range []string{"get", "post", "put", "delete"} {
			if value, ok := methods[method]; ok {
				op := value.(map[string]any)
				responses := map[string]any{}
				for code, value := range op["responses"].(map[string]any) {
					if strings.HasPrefix(code, "2") {
						responses[code] = value
					} else {
						responses[code] = map[string]string{"schema": "Problem (公共错误合同)"}
					}
				}
				authorization, _ := op["x-authorization"].(string)
				routes = append(routes, route{strings.ToUpper(method), path, op["summary"].(string), op["operationId"].(string), op["description"].(string), authorization, display(op["parameters"]), display(op["requestBody"]), display(responses)})
			}
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	reference.Execute(w, routes)
}

var reference = template.Must(template.New("reference").Parse(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Identity · 公共 API 参考</title><link rel="stylesheet" href="/docs.css"></head><body><header>◈ Identity <span>开发者文档 · v1</span></header><main><p class="eyebrow">PLATFORM API</p><h1>将身份与空间管理<br>接入你的应用</h1><p class="intro">通过标准 OAuth 授权，读取身份资料，管理获授权的空间、成员、组织和会话。</p><nav><a href="/openapi.yaml">下载 OpenAPI 3.1 YAML</a><a href="/openapi.json">下载 JSON</a><a href="/machine-openapi.yaml">机器 API OpenAPI YAML</a><a href="/machine-openapi.json">机器 API JSON</a><a href="#routes">查看管理接口</a><a href="http://console.localhost:3003">控制台 / API Playground</a></nav><section class="cards"><article><h2>标准接入</h2><p>服务端与原生客户端使用 Authorization Code + S256 PKCE。请求携带 <code>Authorization: Bearer</code>；浏览器应用通过 BFF 接入。</p></article><article><h2>明确权限</h2><p>读取需要 <code>platform:read</code>，写入需要 <code>platform:write</code> 与最近五分钟的 MFA。租户角色分别授权成员、组织、邀请和审计，显式拒绝优先；角色管理仅限 owner。</p></article><article><h2>可预测的合同</h2><p>列表支持 <code>limit / after</code> 分页；并发修改使用 <code>If-Match</code>；错误采用 RFC 9457 和请求关联 ID。</p></article></section><section><h2>机器身份与凭证</h2><p>自有应用的所有者可在控制台管理机器身份和 AK/SK。SK 仅在专用创建页显示一次；通用 Playground 只展示该创建合同，其余管理操作可执行。机器协议与业务资源使用单独合同和应用 audience，不经 User BFF。机器 token 不能管理平台，User token 不能调用机器资源。</p></section><section id="routes"><h2>接口参考</h2><p>切换空间需要新的租户授权。权限版本变化会使原授权失效；刷新令牌不会更新 MFA 认证时间。</p><div class="table"><table><thead><tr><th>方法</th><th>路径</th><th>用途</th></tr></thead><tbody>{{range .}}<tr><td><code>{{.Method}}</code></td><td><code>{{.Path}}</code></td><td><a href="#{{.ID}}">{{.Summary}}</a></td></tr>{{end}}</tbody></table></div></section><section id="api-contract"><h2>参数、请求与响应</h2><p>展开接口可查看逐字段 schema、请求示例、所需权限和成功响应。schema 直接来自同一份 OpenAPI；示例 ID/邀请码只作格式演示，须换成实际数据。</p>{{range .}}<details id="{{.ID}}"><summary><code>{{.Method}} {{.Path}}</code> · {{.Summary}}</summary><p>{{.Description}}</p><p>授权：<code>{{.Authorization}}</code>；写入均需最近 300 秒 MFA。</p><h3>参数</h3><pre>{{.Parameters}}</pre><h3>请求体与示例</h3><pre>{{.Request}}</pre><h3>响应与状态码</h3><pre>{{.Responses}}</pre></details>{{end}}</section><section><h2>认证、并发与错误处理</h2><p>issuer 为 http://auth.localhost:4464/，业务 audience 为 https://api.sso.local。使用 code + S256 PKCE、state/nonce；原生客户端回调 http://127.0.0.1:9876/callback。ID token 不能作为 access token；不要把实际令牌粘贴到文档页。</p><p>列表 limit 为 1–100，after 使用 next_cursor。If-Match 缺失返回 428，格式错误 400，旧版本 412。先读当前版本，再明确提交一次；请求中断后先核对状态。变更权限版本后需重新授权，refresh 不更新权限或 MFA 时间。</p><p>错误使用 application/problem+json，包含 type/title/status/code/request_id。401 为令牌无效或授权已失效；403 为权限/scope 不足或 recent_mfa_required；404 隐藏非当前空间资源；409 为冲突（含 last_active_owner、reserved_role、role_in_use）；429 查看 Retry-After；503 表示当前无法证明权威状态。浏览器 BFF 的会话错误另使用受控 JSON。</p><pre>{"type":"urn:sso:problem:forbidden","title":"forbidden","status":403,"code":"forbidden","request_id":"000000000000000000000000"}</pre></section><footer>本地参考实现 · 生产部署由 SRE 负责 TLS、正式域名和运行配置。此页面不收集或保存访问令牌。</footer></main></body></html>`))

func docsCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Write([]byte(`*{box-sizing:border-box}body{margin:0;background:#f6f8fc;color:#182338;font:16px/1.65 system-ui,sans-serif}header{padding:22px max(6vw,24px);background:#172d52;color:white;font-size:23px;font-weight:700}header span{float:right;font-size:14px;font-weight:400;padding-top:7px}main{max-width:1200px;margin:auto;padding:58px 24px}.eyebrow{color:#3158ba;letter-spacing:.16em;font-size:13px;font-weight:700}h1{font-size:clamp(32px,5vw,54px);line-height:1.25;letter-spacing:-.025em;margin:15px 0 22px}.intro{color:#4c5b72;font-size:19px;max-width:680px}nav{display:flex;flex-wrap:wrap;gap:12px;margin:30px 0 48px}nav a{background:#2450d4;color:white;padding:11px 18px;border-radius:7px;text-decoration:none}nav a:focus-visible{outline:3px solid #829fff;outline-offset:3px}nav a:last-child{background:white;color:#2450d4;border:1px solid #c7d2ea}details{background:white;border:1px solid #dae2ef;border-radius:8px;padding:18px;margin:14px 0;scroll-margin-top:18px}summary{cursor:pointer;overflow-wrap:anywhere}pre{white-space:pre-wrap;overflow-wrap:anywhere;max-height:520px;overflow:auto;background:#f3f6fb;padding:16px;font-size:12px}#api-contract{margin-top:44px}.cards{display:grid;grid-template-columns:repeat(3,1fr);gap:20px;margin-bottom:54px}article{background:white;border:1px solid #dae2ef;border-radius:12px;padding:24px}h2{font-size:21px;margin:0 0 14px}article p,section>p{color:#526079}code{font-size:13px;overflow-wrap:anywhere}table{width:100%;border-collapse:collapse;background:white;font-size:14px}th,td{text-align:left;border-bottom:1px solid #e2e7f0;padding:15px 18px}th{background:#eaf0fa;color:#334760}td:first-child{color:#2450b4;font-weight:700}.table{border:1px solid #dae2ef;border-radius:10px;overflow:auto}footer{font-size:13px;color:#65748b;margin-top:40px}@media(max-width:700px){header{font-size:20px}header span{font-size:11px}.cards{grid-template-columns:1fr}main{padding-top:32px}table,tbody,tr,td{display:block;width:100%}thead{display:none}tr{padding:18px;border-bottom:1px solid #e2e7f0}td{padding:4px 0;border:0;min-width:0}td:first-child{display:inline-block;width:auto;background:#edf2ff;border-radius:4px;padding:3px 8px;margin-bottom:7px}td:nth-child(2){min-width:0}td:last-child{color:#526079;font-size:13px}code{font-size:12px}tr:last-child{border:0}}`))
}
