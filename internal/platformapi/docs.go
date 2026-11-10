package platformapi

import (
	"encoding/json"
	contract "github.com/lth2015/sso/api"
	"html/template"
	"net/http"
	"sort"
	"strings"
)

func docs(w http.ResponseWriter, r *http.Request) {
	var spec struct {
		Paths map[string]map[string]struct {
			Summary string `json:"summary"`
		} `json:"paths"`
	}
	if json.Unmarshal(contract.PublicJSON, &spec) != nil {
		problem(w, 503, "temporarily_unavailable")
		return
	}
	type route struct{ Method, Path, Summary string }
	routes := []route{}
	paths := []string{}
	for path := range spec.Paths {
		if strings.HasPrefix(path, "/v1/") {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	for _, path := range paths {
		for _, method := range []string{"get", "post", "put", "delete"} {
			if op, ok := spec.Paths[path][method]; ok {
				routes = append(routes, route{strings.ToUpper(method), path, op.Summary})
			}
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	reference.Execute(w, routes)
}

var reference = template.Must(template.New("reference").Parse(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Identity · 公共 API 参考</title><link rel="stylesheet" href="/docs.css"></head><body><header>◈ Identity <span>开发者文档 · v1</span></header><main><p class="eyebrow">PLATFORM API</p><h1>将身份与空间管理<br>接入你的应用</h1><p class="intro">通过标准 OAuth 授权，读取身份资料，管理获授权的空间、成员、组织和会话。</p><nav><a href="/openapi.yaml">下载 OpenAPI 3.1 YAML</a><a href="/openapi.json">下载 JSON</a><a href="#routes">查看接口</a></nav><section class="cards"><article><h2>标准接入</h2><p>服务端与原生客户端使用 Authorization Code + S256 PKCE。请求携带 <code>Authorization: Bearer</code>；浏览器应用通过 BFF 接入。</p></article><article><h2>明确权限</h2><p>读取需要 <code>platform:read</code>，写入需要 <code>platform:write</code> 与最近五分钟的 MFA。租户管理当前仅向 active owner 开放。</p></article><article><h2>可预测的合同</h2><p>列表支持 <code>limit / after</code> 分页；并发修改使用 <code>If-Match</code>；错误采用 RFC 9457 和请求关联 ID。</p></article></section><section id="routes"><h2>接口参考</h2><p>切换空间需要新的租户授权。权限版本变化会使原授权失效；刷新令牌不会更新 MFA 认证时间。</p><div class="table"><table><thead><tr><th>方法</th><th>路径</th><th>用途</th></tr></thead><tbody>{{range .}}<tr><td><code>{{.Method}}</code></td><td><code>{{.Path}}</code></td><td>{{.Summary}}</td></tr>{{end}}</tbody></table></div></section><footer>本地参考实现 · 生产部署由 SRE 负责 TLS、正式域名和运行配置。此页面不收集或保存访问令牌。</footer></main></body></html>`))

func docsCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Write([]byte(`*{box-sizing:border-box}body{margin:0;background:#f6f8fc;color:#182338;font:16px/1.65 system-ui,sans-serif}header{padding:22px max(6vw,24px);background:#172d52;color:white;font-size:23px;font-weight:700}header span{float:right;font-size:14px;font-weight:400;padding-top:7px}main{max-width:1200px;margin:auto;padding:58px 24px}.eyebrow{color:#3158ba;letter-spacing:.16em;font-size:13px;font-weight:700}h1{font-size:clamp(32px,5vw,54px);line-height:1.25;letter-spacing:-.025em;margin:15px 0 22px}.intro{color:#4c5b72;font-size:19px;max-width:680px}nav{display:flex;flex-wrap:wrap;gap:12px;margin:30px 0 48px}nav a{background:#2450d4;color:white;padding:11px 18px;border-radius:7px;text-decoration:none}nav a:focus-visible{outline:3px solid #829fff;outline-offset:3px}nav a:last-child{background:white;color:#2450d4;border:1px solid #c7d2ea}.cards{display:grid;grid-template-columns:repeat(3,1fr);gap:20px;margin-bottom:54px}article{background:white;border:1px solid #dae2ef;border-radius:12px;padding:24px}h2{font-size:21px;margin:0 0 14px}article p,section>p{color:#526079}code{font-size:13px;overflow-wrap:anywhere}table{width:100%;border-collapse:collapse;background:white;font-size:14px}th,td{text-align:left;border-bottom:1px solid #e2e7f0;padding:15px 18px}th{background:#eaf0fa;color:#334760}td:first-child{color:#2450b4;font-weight:700}.table{border:1px solid #dae2ef;border-radius:10px;overflow:auto}footer{font-size:13px;color:#65748b;margin-top:40px}@media(max-width:700px){header{font-size:20px}header span{font-size:11px}.cards{grid-template-columns:1fr}main{padding-top:32px}table,tbody,tr,td{display:block;width:100%}thead{display:none}tr{padding:18px;border-bottom:1px solid #e2e7f0}td{padding:4px 0;border:0;min-width:0}td:first-child{display:inline-block;width:auto;background:#edf2ff;border-radius:4px;padding:3px 8px;margin-bottom:7px}td:nth-child(2){min-width:0}td:last-child{color:#526079;font-size:13px}code{font-size:12px}tr:last-child{border:0}}`))
}
