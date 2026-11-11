import React, { useEffect, useRef, useState } from "react";
import {
  operations,
  bodyExample,
  buildCall,
  execute,
  curlTemplate,
  redact,
} from "./playground.js";

export default function Playground({
  context,
  csrf,
  me,
  tenant,
  onUnauthorized,
}) {
  const [spec, setSpec] = useState(null),
    [selected, setSelected] = useState("getMe"),
    [values, setValues] = useState({}),
    [body, setBody] = useState(""),
    [etag, setETag] = useState(""),
    [review, setReview] = useState(null),
    [lastCall, setLastCall] = useState(null),
    [confirmed, setConfirmed] = useState(false),
    [result, setResult] = useState(null),
    [error, setError] = useState(""),
    [busy, setBusy] = useState(false);
  const alive = useRef(true),
    pending = useRef(false),
    abort = useRef(null);
  useEffect(() => {
    const controller = new AbortController();
    fetch("/platform-openapi.json", {
      signal: controller.signal,
      redirect: "error",
    })
      .then((r) => {
        if (!r.ok) throw new Error();
        return r.json();
      })
      .then((data) => {
        if (alive.current) setSpec(data);
      })
      .catch((e) => {
        if (e.name !== "AbortError" && alive.current)
          setError("API 合同暂时不可用，请重新进入页面。");
      });
    return () => {
      alive.current = false;
      controller.abort();
      abort.current?.abort();
    };
  }, []);
  const choices = spec ? operations(spec) : [],
    operation = choices.find((o) => o.operationId === selected);
  function clear() {
    setReview(null);
    setLastCall(null);
    setConfirmed(false);
    setResult(null);
    setError("");
  }
  function choose(value) {
    clear();
    setSelected(value);
    setValues({});
    setETag("");
    const next = choices.find((o) => o.operationId === value);
    setBody(bodyExample(spec, next, tenant));
  }
  async function send(call, versionOnly = false) {
    if (pending.current) return;
    pending.current = true;
    setBusy(true);
    setError("");
    setResult(null);
    abort.current = new AbortController();
    try {
      const response = await execute(context, csrf, call, abort.current.signal);
      if (!alive.current) return;
      if (response.status === 401) {
        onUnauthorized();
        return;
      }
      if (versionOnly && response.status === 200 && response.etag)
        setETag(response.etag);
      setResult({
        ...response,
        request: call.method + " " + call.path,
        versionOnly,
      });
      setReview(null);
      setConfirmed(false);
    } catch (e) {
      if (alive.current && e.name !== "AbortError")
        setError("请求未完成，结果可能已经保存。请核对后再决定是否重试。");
    } finally {
      pending.current = false;
      if (alive.current) setBusy(false);
    }
  }
  function prepare(event) {
    event.preventDefault();
    clear();
    try {
      const call = buildCall(operation, values, body, etag, me.tenant_id);
      setLastCall({ ...call, body: redact(call.body) });
      if (call.method === "GET") send(call);
      else {
        setReview(call);
        setConfirmed(false);
      }
    } catch (e) {
      setError(e.message);
    }
  }
  if (!operation)
    return (
      <section className="panel">
        <p role="status">{error || "正在读取 OpenAPI 合同…"}</p>
      </section>
    );
  const match = operation.parameters.find((p) => p.name === "If-Match");
  const scope = operation.security[0].OAuth2.join(" ");
  return (
    <section className="panel playground">
      <div className="panel-heading">
        <h2>真实 API 调试</h2>
        <a href="/platform-openapi.json" download="sso-openapi.json">
          下载 OpenAPI {spec.info.version}
        </a>
      </div>
      <p className="section-note">
        请求使用当前空间授权。OAuth 令牌保存在服务端；写入需最近五分钟
        MFA，且只发送一次。响应中的凭证字段会被隐藏。
      </p>
      <form onSubmit={prepare}>
        <fieldset disabled={busy}>
          <label>
            选择 API
            <select
              aria-label="选择 API"
              value={selected}
              onChange={(e) => choose(e.target.value)}
            >
              {choices.map((o) => (
                <option key={o.operationId} value={o.operationId}>
                  {o.method} {o.path} · {o.summary}
                </option>
              ))}
            </select>
          </label>
          <p className="section-note">{operation.description}</p>
          <div className="playground-meta">
            <span className="pill">{scope}</span>
            <span className="pill neutral">
              {operation["x-authorization"] || "当前有效成员"}
            </span>
          </div>
          {operation.parameters
            .filter((p) => p.in === "path" || p.in === "query")
            .map((p) => (
              <label key={p.name}>
                {p.name}
                {p.required ? "（必填）" : "（可选）"}
                <input
                  aria-label={p.name}
                  required={p.required}
                  maxLength={128}
                  readOnly={p.name === "tenant"}
                  value={
                    p.name === "tenant" ? me.tenant_id : values[p.name] || ""
                  }
                  placeholder={
                    p.name === "limit"
                      ? "默认 50，最多 100"
                      : p.name === "after"
                        ? "上页 next_cursor"
                        : "32 位资源 ID"
                  }
                  onChange={(e) => {
                    clear();
                    setValues({ ...values, [p.name]: e.target.value });
                  }}
                />
              </label>
            ))}
          {match && (
            <div>
              <label>
                If-Match
                <input
                  aria-label="If-Match"
                  value={etag}
                  maxLength={128}
                  placeholder={'例如 "authz-2" 或 "org-3"'}
                  onChange={(e) => {
                    clear();
                    setETag(e.target.value);
                  }}
                />
              </label>
              <button
                className="secondary"
                type="button"
                onClick={() => {
                  clear();
                  const clientVersion =
                    match.schema.pattern.includes("client-");
                  if (
                    clientVersion &&
                    (!/^[a-f0-9]{32}$/.test(values.application || "") ||
                      !/^[a-f0-9]{32}$/.test(values.client || ""))
                  ) {
                    setError(
                      "请先填写目标 application 和 client ID，再读取最新版本。",
                    );
                    return;
                  }
                  const applicationVersion =
                    match.schema.pattern.includes("app-");
                  if (
                    applicationVersion &&
                    !/^[a-f0-9]{32}$/.test(values.application || "")
                  ) {
                    setError(
                      "请先填写目标 application ID，再读取它的最新版本。",
                    );
                    return;
                  }
                  send(
                    {
                      method: "GET",
                      path:
                        "/v1/tenants/" +
                        me.tenant_id +
                        (clientVersion
                          ? "/applications/" +
                            values.application +
                            "/clients/" +
                            values.client
                          : applicationVersion
                            ? "/applications/" + values.application
                            : match.schema.pattern.includes("org-")
                              ? "/organizations?limit=1"
                              : ""),
                    },
                    true,
                  );
                }}
              >
                读取最新 ETag
              </button>
            </div>
          )}
          {operation.requestBody && (
            <label>
              请求 JSON
              <textarea
                aria-label="请求 JSON"
                rows={9}
                value={body}
                spellCheck={false}
                maxLength={4096}
                onChange={(e) => {
                  clear();
                  setBody(e.target.value);
                }}
              />
            </label>
          )}
          <div className="actions playground-actions">
            <button type="submit">
              {operation.method === "GET" ? "发送读取请求" : "检查写入请求"}
            </button>
            <button
              className="secondary"
              type="button"
              onClick={() => {
                clear();
                setValues({});
                setETag("");
                setBody(bodyExample(spec, operation, tenant));
              }}
            >
              重置输入
            </button>
          </div>
        </fieldset>
      </form>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      {review && (
        <section className="playground-review" aria-label="请求确认">
          <h3>确认后发送一次</h3>
          <code>
            {review.method} {review.path}
          </code>
          <pre>{JSON.stringify(review.body, null, 2)}</pre>
          <label className="owner-confirm">
            <input
              type="checkbox"
              checked={confirmed}
              onChange={(e) => setConfirmed(e.target.checked)}
            />
            我确认向当前空间发送这项写入请求
          </label>
          <button disabled={!confirmed || busy} onClick={() => send(review)}>
            确认发送一次
          </button>
        </section>
      )}
      {result && (
        <section
          aria-label="API 响应"
          className="playground-result"
          aria-live="polite"
        >
          <h3>HTTP {result.status}</h3>
          <p>
            <code>{result.request}</code>
          </p>
          <dl>
            <dt>ETag</dt>
            <dd>{result.etag || "—"}</dd>
            <dt>Request ID</dt>
            <dd>{result.requestID || "—"}</dd>
            <dt>Retry-After</dt>
            <dd>{result.retryAfter || "—"}</dd>
          </dl>
          <pre data-testid="playground-response">
            {JSON.stringify(result.body, null, 2)?.slice(0, 24000) ||
              "无响应体"}
          </pre>
          <p className="section-note">
            最多显示 24,000
            字符；不保存调用历史。一次性凭证字段已隐藏，邀请凭证请通过邀请页面获取。
          </p>
          {result.body?.code === "recent_mfa_required" && (
            <a className="button" href="/login?force=1">
              重新验证身份
            </a>
          )}
          {!result.versionOnly && operation.method !== "GET" && (
            <p>
              若空间权限版本已变化，请<a href="/login">重新确认空间授权</a>
              。重新授权不会重发此请求。
            </p>
          )}
        </section>
      )}
      {lastCall && (
        <details>
          <summary>外部客户端 cURL 模板</summary>
          <p className="section-note">
            ACCESS_TOKEN 是占位符；敏感请求字段已隐藏。外部客户端须自行完成
            OAuth 授权。
          </p>
          <pre>{curlTemplate(lastCall)}</pre>
        </details>
      )}
    </section>
  );
}
