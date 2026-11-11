import React, { useEffect, useRef, useState } from "react";
import { request, errorText } from "./api.js";
import ManagedRegistration from "./ManagedRegistration.jsx";
function Dialog({ title, busy, close, children }) {
  const ref = useRef(null);
  useEffect(() => {
    ref.current.showModal();
  }, []);
  return (
    <dialog
      ref={ref}
      aria-labelledby="application-dialog-title"
      onCancel={(e) => {
        e.preventDefault();
        if (!busy) close();
      }}
    >
      <header className="dialog-header">
        <h2 id="application-dialog-title">{title}</h2>
        <button
          type="button"
          className="icon-button"
          aria-label="关闭应用对话框"
          disabled={busy}
          onClick={close}
        >
          ×
        </button>
      </header>
      {children}
    </dialog>
  );
}
export default function ApplicationManagement({
  context,
  csrf,
  onFailure,
  revision,
}) {
  const [items, setItems] = useState(null),
    [next, setNext] = useState(null),
    [after, setAfter] = useState(""),
    [reload, setReload] = useState(0),
    [dialog, setDialog] = useState(null),
    [busy, setBusy] = useState(false),
    [error, setError] = useState(null),
    [notice, setNotice] = useState(""),
    [confirmed, setConfirmed] = useState(false);
  const [registration, setRegistration] = useState(null);
  const alive = useRef(true),
    generation = useRef(0),
    dialogAbort = useRef(null);
  const base = "/bff/v1/tenants/" + context.tenant_id + "/applications";
  const identity = context.context;
  function fail(e) {
    setError(e);
    if (e.status === 401 || e.code === "recent_mfa_required") onFailure(e);
  }
  useEffect(
    () => () => {
      alive.current = false;
      generation.current++;
      dialogAbort.current?.abort();
    },
    [],
  );
  useEffect(() => {
    const abort = new AbortController();
    setItems(null);
    setNext(null);
    request(
      identity,
      csrf,
      base + "?limit=50" + (after ? "&after=" + after : ""),
      { signal: abort.signal },
    )
      .then((r) => {
        if (!abort.signal.aborted) {
          setItems(r.data.items);
          setNext(r.data.next_cursor);
        }
      })
      .catch((e) => {
        if (!abort.signal.aborted) fail(e);
      });
    return () => abort.abort();
  }, [identity, after, reload, revision]);
  function close() {
    generation.current++;
    dialogAbort.current?.abort();
    setDialog(null);
    setError(null);
    setConfirmed(false);
  }
  async function choose(app, kind, cursor = "") {
    const epoch = ++generation.current;
    dialogAbort.current?.abort();
    const abort = new AbortController();
    dialogAbort.current = abort;
    setError(null);
    setConfirmed(false);
    setDialog({ kind, app, loading: true });
    try {
      const url =
        base +
        "/" +
        app.id +
        (kind === "clients"
          ? "/clients?limit=20" +
            (cursor ? "&after=" + encodeURIComponent(cursor) : "")
          : "");
      const response = await request(identity, csrf, url, {
        signal: abort.signal,
      });
      if (
        !alive.current ||
        abort.signal.aborted ||
        epoch !== generation.current
      )
        return;
      setDialog(
        kind === "clients"
          ? { kind, app, data: response.data }
          : { kind, app: response.data, etag: response.etag },
      );
    } catch (e) {
      if (
        alive.current &&
        !abort.signal.aborted &&
        epoch === generation.current
      )
        fail(e);
    }
  }
  async function change(e) {
    e.preventDefault();
    if (busy || !confirmed || dialog.loading) return;
    const epoch = generation.current;
    setBusy(true);
    setError(null);
    try {
      const enabled = !dialog.app.enabled;
      await request(
        identity,
        csrf,
        base + "/" + dialog.app.id + "/" + (enabled ? "enable" : "disable"),
        { method: "POST", body: {}, etag: dialog.etag },
      );
      if (!alive.current || epoch !== generation.current) return;
      setDialog(null);
      setConfirmed(false);
      setNotice(
        enabled
          ? "应用已恢复，请重新授权登录；旧会话不会恢复。"
          : "当前空间的应用已停用，旧授权已撤销；其他应用和空间保持可用。",
      );
      setReload((n) => n + 1);
    } catch (e) {
      if (alive.current && epoch === generation.current) fail(e);
    } finally {
      if (alive.current) setBusy(false);
    }
  }
  return (
    <section className="panel" aria-label="应用接入管理">
      <div className="panel-heading">
        <h2>已接入应用</h2>
        <button onClick={() => setRegistration({ kind: "create-app" })}>
          创建应用
        </button>
        <button
          className="secondary"
          disabled={busy}
          onClick={() => {
            setAfter("");
            setReload((n) => n + 1);
            setError(null);
            setNotice("");
          }}
        >
          刷新应用
        </button>
      </div>
      <p className="section-note">
        查看当前空间的应用和登记配置。自有应用支持公开 PKCE
        客户端登记；停用只影响本空间中目标应用，恢复后需要重新登录。平台管理入口保持启用。
      </p>
      {notice && (
        <p role="status" className="notice">
          {notice}
        </p>
      )}
      {error && !dialog && <p role="alert">{errorText(error)}</p>}
      {!items ? (
        <p role="status">正在读取应用…</p>
      ) : items.length === 0 ? (
        <p role="status">当前空间暂无已接入应用。</p>
      ) : (
        <div className="table-scroll application-directory">
          <table>
            <thead>
              <tr>
                <th>应用</th>
                <th>状态</th>
                <th>客户端</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {items.map((app) => (
                <tr key={app.id}>
                  <td>
                    <strong>{app.name}</strong>
                    <small className="event-code">{app.code}</small>
                  </td>
                  <td data-label="状态">
                    {app.enabled ? "已启用" : "已停用"}
                    {!app.available && (
                      <small className="event-code">平台已暂停此应用</small>
                    )}
                    {app.protected && (
                      <small className="event-code">平台管理入口</small>
                    )}
                  </td>
                  <td data-label="客户端数量">{app.client_count}</td>
                  <td>
                    <div className="actions">
                      {app.managed && (
                        <button
                          className="secondary"
                          disabled={busy || !app.enabled || !app.available}
                          onClick={() =>
                            setRegistration({ kind: "create-client", app })
                          }
                        >
                          登记公开客户端
                        </button>
                      )}
                      <button
                        className="secondary"
                        disabled={busy}
                        onClick={() => choose(app, "clients")}
                      >
                        查看登记配置
                      </button>
                      <button
                        className={app.enabled ? "danger-link" : "secondary"}
                        disabled={
                          busy ||
                          app.protected ||
                          (!app.enabled && !app.available)
                        }
                        onClick={() => choose(app, "change")}
                      >
                        {app.enabled ? "停用应用" : "恢复应用"}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {next && (
        <button
          className="secondary"
          disabled={busy}
          onClick={() => setAfter(next)}
        >
          下一页应用
        </button>
      )}
      {dialog && (
        <Dialog
          title={
            dialog.kind === "clients"
              ? "客户端登记配置"
              : dialog.app.enabled
                ? "停用应用"
                : "恢复应用"
          }
          busy={busy}
          close={close}
        >
          <p>
            <strong>{dialog.app.name}</strong>
          </p>
          {error && (
            <p role="alert">
              {errorText(error)} 请关闭对话框，刷新状态后再决定是否操作。
            </p>
          )}
          {dialog.loading ? (
            <p role="status">正在读取最新登记状态…</p>
          ) : dialog.kind === "clients" ? (
            <>
              <p>
                OIDC issuer：<code>{dialog.data.issuer}</code>
              </p>
              <p className="section-note">
                共享客户端只显示登记元数据。自有公开客户端可查看状态、完成登记或撤销；密钥类凭证由后续流程提供。
              </p>
              {dialog.data.items.map((client) => (
                <article className="summary-card" key={client.client_id}>
                  <h3>{client.client_id}</h3>
                  <dl>
                    <dt>回调 URI</dt>
                    <dd className="integration-value">{client.redirect_uri}</dd>
                    <dt>Scope</dt>
                    <dd className="integration-value">
                      {client.scopes.join(" ")}
                    </dd>
                    <dt>Audience</dt>
                    <dd className="integration-value">{client.audience}</dd>
                    <dt>客户端状态</dt>
                    <dd>
                      {client.managed
                        ? {
                            pending: "待完成登记",
                            active: "有效",
                            revoked: "已撤销",
                          }[client.state]
                        : client.active
                          ? "有效"
                          : "已暂停"}
                    </dd>
                  </dl>
                  {client.managed && (
                    <button
                      className="secondary"
                      onClick={() => {
                        setDialog(null);
                        setRegistration({
                          kind: "inspect",
                          app: dialog.app,
                          client: client.client_id,
                        });
                      }}
                    >
                      管理登记
                    </button>
                  )}
                </article>
              ))}
              {dialog.data.items.length === 0 && <p>暂无已登记客户端。</p>}
              {dialog.data.next_cursor && (
                <button
                  className="secondary"
                  onClick={() =>
                    choose(dialog.app, "clients", dialog.data.next_cursor)
                  }
                >
                  下一页客户端
                </button>
              )}
            </>
          ) : (
            <form onSubmit={change}>
              <p>
                {dialog.app.enabled
                  ? "将撤销当前空间所有成员在这个应用的旧授权和应用会话。其他应用、其他空间和中央登录保留。"
                  : "恢复当前空间的应用访问。旧会话不会重新生效，用户需要重新授权登录。"}
              </p>
              <label>
                <input
                  type="checkbox"
                  checked={confirmed}
                  onChange={(e) => setConfirmed(e.target.checked)}
                  required
                />
                我确认对当前空间执行此操作
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
                <button
                  type="submit"
                  disabled={busy || !confirmed || Boolean(error)}
                >
                  {busy ? "正在保存…" : "确认操作"}
                </button>
              </div>
            </form>
          )}
        </Dialog>
      )}
      {registration && (
        <ManagedRegistration
          key={
            identity +
            registration.kind +
            (registration.client || registration.app?.id || "")
          }
          action={registration}
          context={context}
          csrf={csrf}
          close={() => setRegistration(null)}
          onFailure={onFailure}
          onSaved={(message) => {
            setNotice(message);
            setReload((n) => n + 1);
          }}
        />
      )}
    </section>
  );
}
