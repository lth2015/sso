const id = /^[a-f0-9]{32}$/;
export function operations(spec) {
  return Object.entries(spec.paths).flatMap(([path, methods]) =>
    path.startsWith("/v1/")
      ? Object.entries(methods).map(([method, operation]) => ({
          ...operation,
          method: method.toUpperCase(),
          path,
          parameters: (operation.parameters || []).map((p) =>
            p.$ref ? spec.components.parameters[p.$ref.split("/").at(-1)] : p,
          ),
        }))
      : [],
  );
}
export function bodyExample(spec, operation, tenant) {
  const content = operation.requestBody?.content["application/json"];
  if (!content) return "";
  const value = structuredClone(content.example || {});
  if ("parent_id" in value) value.parent_id = tenant.root_id;
  return JSON.stringify(value, null, 2);
}
export function buildCall(operation, values, text, etag, tenant) {
  let path = operation.path;
  for (const name of path.matchAll(/\{(\w+)\}/g)) {
    const value = name[1] === "tenant" ? tenant : values[name[1]];
    if (!id.test(value || ""))
      throw new Error("请填写有效的 32 位小写十六进制资源 ID。");
    path = path.replace(name[0], value);
  }
  const query = new URLSearchParams();
  for (const p of operation.parameters.filter((p) => p.in === "query")) {
    const value = values[p.name];
    if (!value) continue;
    if (p.name === "limit" && !/^(?:[1-9][0-9]?|100)$/.test(value))
      throw new Error("limit 需要是 1–100 的整数。");
    if (p.name === "after" && !id.test(value))
      throw new Error("after 需要使用上页返回的有效 cursor。");
    query.set(p.name, value);
  }
  if (query.size) path += "?" + query;
  let body;
  if (operation.requestBody) {
    if (new TextEncoder().encode(text).length > 4096)
      throw new Error("请求 JSON 不得超过 4096 字节。");
    try {
      body = JSON.parse(text);
    } catch {
      throw new Error("请求体需要是有效 JSON。");
    }
    if (!body || Array.isArray(body) || typeof body !== "object")
      throw new Error("请求体需要是 JSON 对象。");
  }
  const match = operation.parameters.find((p) => p.name === "If-Match");
  if (match) {
    if (!new RegExp(match.schema.pattern).test(etag))
      throw new Error(
        '请先读取版本，或填写带双引号的 If-Match，如 "authz-2"。',
      );
  } else etag = "";
  return { path, method: operation.method, body, etag };
}
export function redact(value) {
  if (Array.isArray(value)) return value.map(redact);
  if (value && typeof value === "object")
    return Object.fromEntries(
      Object.entries(value).map(([key, child]) => [
        key,
        /token|password|secret|csrf|recovery|verifier/i.test(key)
          ? "[已隐藏]"
          : redact(child),
      ]),
    );
  if (typeof value === "string" && /eyJ[\w-]+\.[\w-]+\.[\w-]+/.test(value))
    return "[已隐藏]";
  return value;
}
export async function execute(context, csrf, call, signal) {
  const headers = { "X-Console-Context": context };
  if (call.method !== "GET") {
    headers["X-CSRF-Token"] = csrf;
    headers["Content-Type"] = "application/json";
  }
  if (call.etag) headers["If-Match"] = call.etag;
  const response = await fetch("/bff" + call.path, {
    method: call.method,
    headers,
    signal,
    redirect: "error",
    credentials: "same-origin",
    body: call.body === undefined ? undefined : JSON.stringify(call.body),
  });
  let data = null;
  if (response.status !== 204) {
    try {
      data = await response.json();
    } catch {
      throw new Error("服务未返回有效 JSON，请核对操作结果。");
    }
  }
  return {
    status: response.status,
    body: redact(data),
    etag: response.headers.get("ETag"),
    requestID: response.headers.get("X-Request-ID"),
    retryAfter: response.headers.get("Retry-After"),
  };
}
const quote = (value) => "'" + value.replaceAll("'", "'\"'\"'") + "'";
export function curlTemplate(call) {
  const parts = [
    "curl --request " + call.method,
    "  --url " + quote("http://api.localhost:8083" + call.path),
    "  --header 'Authorization: Bearer <ACCESS_TOKEN>'",
  ];
  if (call.etag) parts.push("  --header " + quote("If-Match: " + call.etag));
  if (call.body !== undefined) {
    parts.push("  --header 'Content-Type: application/json'");
    parts.push("  --data " + quote(JSON.stringify(redact(call.body))));
  }
  return parts.join(" \\\n");
}
