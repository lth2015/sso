import React, { useEffect, useRef, useState } from "react";
import { request, errorText } from "./api.js";
const stateText = {
  pending: "待完成登记",
  active: "已生效",
  revoked: "已撤销",
};
export default function ManagedRegistration({
  action,
  context,
  csrf,
  close,
  onSaved,
  onFailure,
}) {
  const [value, setValue] = useState(null),
    [inspecting, setInspecting] = useState(action.kind === "inspect"),
    [error, setError] = useState(null),
    [busy, setBusy] = useState(false),
    [confirmed, setConfirmed] = useState(false),
    [notice, setNotice] = useState("");
  const ref = useRef(null),
    alive = useRef(true),
    abort = useRef(null),
    writing = useRef(false);
  const base = "/bff/v1/tenants/" + context.tenant_id + "/applications";
  const clientPath = value
    ? base +
      "/" +
      value.data.application_id +
      "/clients/" +
      value.data.client_id
    : base + "/" + action.app?.id + "/clients/" + action.client;
  function fail(e) {
    setError(e);
    if (e.status === 401 || e.code === "recent_mfa_required") onFailure(e);
  }
  async function read() {
    setInspecting(true);
    abort.current?.abort();
    const controller = new AbortController();
    abort.current = controller;
    setError(null);
    setConfirmed(false);
    setValue(null);
    try {
      const r = await request(context.context, csrf, clientPath, {
        signal: controller.signal,
      });
      if (alive.current && !controller.signal.aborted) setValue(r);
    } catch (e) {
      if (alive.current && !controller.signal.aborted) fail(e);
    }
  }
  useEffect(() => {
    ref.current.showModal();
    if (action.kind === "inspect") read();
    return () => {
      alive.current = false;
      abort.current?.abort();
    };
  }, []);
  async function save(e, command) {
    e.preventDefault();
    if (!confirmed || error || writing.current) return;
    writing.current = true;
    setBusy(true);
    setError(null);
    setNotice("");
    try {
      const form =
        e.currentTarget.tagName === "FORM"
          ? new FormData(e.currentTarget)
          : null;
      const creatingApp = action.kind === "create-app";
      const body = value
        ? {}
        : creatingApp
          ? { name: form.get("name").trim() }
          : {
              name: form.get("name").trim(),
              redirect_uri: form.get("redirect_uri"),
            };
      const r = await request(
        context.context,
        csrf,
        value
          ? clientPath + "/" + command
          : creatingApp
            ? base
            : base + "/" + action.app.id + "/clients",
        { method: "POST", body, etag: value?.etag },
      );
      if (!alive.current) return;
      setConfirmed(false);
      if (creatingApp) {
        onSaved("应用已创建。现在可登记公开客户端。");
        close();
        return;
      }
      const updated =
        command === "reconcile" ? { ...r, data: r.data.client } : r;
      setInspecting(true);
      setValue(updated);
      const message = !command
        ? "登记已保存。确认回调配置后，核对并完成协议登记。"
        : command === "revoke"
          ? "客户端已撤销，旧授权已失效。可继续清理协议登记。"
          : r.data.protocol_observation === "absent"
            ? "本次核对确认协议登记已不存在；本地撤销记录保留。"
            : "协议配置核对完成，客户端可用于登录。";
      setNotice(message);
      onSaved(message);
    } catch (e) {
      if (alive.current) fail(e);
    } finally {
      writing.current = false;
      if (alive.current) setBusy(false);
    }
  }
  return (
    <dialog
      ref={ref}
      aria-labelledby="managed-registration-title"
      onCancel={(e) => {
        e.preventDefault();
        if (!busy) close();
      }}
    >
      <header className="dialog-header">
        <h2 id="managed-registration-title">
          {action.kind === "create-app"
            ? "创建应用"
            : inspecting
              ? "管理客户端登记"
              : "登记公开客户端"}
        </h2>
        <button
          type="button"
          className="icon-button"
          aria-label="关闭登记对话框"
          disabled={busy}
          onClick={close}
        >
          ×
        </button>
      </header>
      {action.app && (
        <p>
          <strong>{action.app.name}</strong>
        </p>
      )}
      {error && (
        <p role="alert">
          {errorText(error)} 操作不会自动重发；请核对记录，再重新确认。
        </p>
      )}
      {notice && (
        <p role="status" className="notice">
          {notice}
        </p>
      )}
      {value ? (
        <>
          <h3>{value.data.name}</h3>
          <dl>
            <dt>客户端 ID</dt>
            <dd className="integration-value">{value.data.client_id}</dd>
            <dt>状态</dt>
            <dd>{stateText[value.data.state]}</dd>
            <dt>精确回调 URI</dt>
            <dd className="integration-value">{value.data.redirect_uri}</dd>
            <dt>Scope</dt>
            <dd className="integration-value">{value.data.scopes.join(" ")}</dd>
            <dt>Audience</dt>
            <dd className="integration-value">{value.data.audience}</dd>
          </dl>
          <p className="section-note">
            公开客户端使用 code + S256
            PKCE，无需客户端密钥。待完成登记时不能登录；撤销会使此客户端旧授权失效，其他客户端与中央登录保持可用。撤销不可恢复。
          </p>
          <label className="confirmation">
            <input
              type="checkbox"
              checked={confirmed}
              onChange={(e) => setConfirmed(e.target.checked)}
            />
            我已核对配置并确认本次登记操作
          </label>
          <div className="dialog-actions">
            <button className="secondary" disabled={busy} onClick={read}>
              刷新登记
            </button>
            {value.data.state !== "revoked" && (
              <button
                className="danger-link"
                disabled={busy || !confirmed || !!error}
                onClick={(e) => save(e, "revoke")}
              >
                撤销客户端
              </button>
            )}
            <button
              disabled={busy || !confirmed || !!error}
              onClick={(e) => save(e, "reconcile")}
            >
              {value.data.state === "pending"
                ? "核对并完成登记"
                : value.data.state === "revoked"
                  ? "清理已撤销登记"
                  : "核对协议登记"}
            </button>
          </div>
        </>
      ) : inspecting ? (
        <p role="status">
          {error ? "请关闭后重新读取登记。" : "正在读取登记…"}
        </p>
      ) : (
        <form onSubmit={(e) => save(e)}>
          <label>
            {action.kind === "create-app" ? "应用名称" : "客户端名称"}
            <input name="name" required maxLength={128} autoFocus />
          </label>
          {action.kind === "create-app" ? (
            <p className="section-note">
              应用归当前空间所有，只有空间所有者可以管理客户端。系统生成唯一标识；创建应用不会改变其他应用的授权。
            </p>
          ) : (
            <>
              <label>
                精确回调 URI
                <input
                  name="redirect_uri"
                  required
                  type="url"
                  maxLength={255}
                  placeholder="https://app.example.com/oidc/callback"
                />
              </label>
              <p className="section-note">
                适用于桌面或原生公开客户端。回调使用带路径的 HTTPS，或固定端口的
                IP 回环 HTTP。当前配置提供平台 API
                只读权限，登记后需核对协议配置才生效。
              </p>
            </>
          )}
          <label className="confirmation">
            <input
              type="checkbox"
              required
              checked={confirmed}
              onChange={(e) => setConfirmed(e.target.checked)}
            />
            我确认在当前空间创建此登记
          </label>
          <div className="dialog-actions">
            <button
              type="button"
              className="secondary"
              disabled={busy}
              onClick={close}
            >
              取消
            </button>
            <button type="submit" disabled={busy || !confirmed || !!error}>
              {busy ? "正在保存…" : "保存登记"}
            </button>
          </div>
        </form>
      )}
    </dialog>
  );
}
