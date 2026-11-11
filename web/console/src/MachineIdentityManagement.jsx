import React, { useEffect, useRef, useState } from "react";
import { request, errorText } from "./api.js";
import ProtocolProfile from "./ProtocolProfile.jsx";
const stateLabel = {
  pending: "待处理",
  active: "有效",
  revoked: "已撤销",
  expired: "已过期",
};
const date = (seconds) => new Date(seconds * 1000).toLocaleString("zh-CN");
export default function MachineIdentityManagement({
  app,
  context,
  csrf,
  close,
  onFailure,
}) {
  const [view, setView] = useState({ kind: "principals" }),
    [data, setData] = useState(null),
    [error, setError] = useState(null),
    [notice, setNotice] = useState(""),
    [confirmed, setConfirmed] = useState(false),
    [busy, setBusy] = useState(false),
    [revision, setRevision] = useState(0),
    [after, setAfter] = useState("");
  const dialog = useRef(null),
    alive = useRef(true),
    abort = useRef(null),
    generation = useRef(0),
    writing = useRef(false),
    secret = useRef(null);
  const base =
    "/bff/v1/tenants/" +
    context.tenant_id +
    "/applications/" +
    app.id +
    "/service-principals";
  function fail(e) {
    setError(e);
    if (e.status === 401 || e.code === "recent_mfa_required") onFailure(e);
  }
  function leave() {
    secret.current = null;
    generation.current++;
    abort.current?.abort();
    close();
  }
  function choose(next) {
    if (writing.current) return;
    secret.current = null;
    generation.current++;
    abort.current?.abort();
    setView(next);
    setData(null);
    setAfter("");
    setError(null);
    setConfirmed(false);
  }
  useEffect(() => {
    dialog.current.showModal();
    return () => {
      alive.current = false;
      secret.current = null;
      generation.current++;
      abort.current?.abort();
    };
  }, []);
  useEffect(() => {
    if (!["principals", "credentials"].includes(view.kind)) return;
    const controller = new AbortController();
    abort.current = controller;
    const epoch = ++generation.current;
    setData(null);
    (async () => {
      if (view.kind === "principals")
        return request(
          context.context,
          csrf,
          base + "?limit=20" + (after ? "&after=" + after : ""),
          { signal: controller.signal },
        );
      const principal = await request(
        context.context,
        csrf,
        base + "/" + view.principal.id,
        { signal: controller.signal },
      );
      const keys = await request(
        context.context,
        csrf,
        base +
          "/" +
          view.principal.id +
          "/credentials?limit=20" +
          (after ? "&after=" + after : ""),
        { signal: controller.signal },
      );
      return { ...keys, principal };
    })()
      .then((r) => {
        if (
          alive.current &&
          !controller.signal.aborted &&
          epoch === generation.current
        )
          setData(r);
      })
      .catch((e) => {
        if (
          alive.current &&
          !controller.signal.aborted &&
          epoch === generation.current
        )
          fail(e);
      });
    return () => controller.abort();
  }, [view.kind, view.principal?.id, after, revision]);
  async function sensitive(kind, row) {
    const epoch = ++generation.current;
    abort.current?.abort();
    const controller = new AbortController();
    abort.current = controller;
    setError(null);
    setConfirmed(false);
    setData(null);
    setView({ kind: "loading", principal: view.principal });
    try {
      const path =
        base +
        "/" +
        view.principal.id +
        (kind === "principal-change" ? "" : "/credentials/" + row.id);
      const value = await request(context.context, csrf, path, {
        signal: controller.signal,
      });
      if (
        alive.current &&
        !controller.signal.aborted &&
        epoch === generation.current
      )
        setView({ kind, principal: view.principal, value });
    } catch (e) {
      if (
        alive.current &&
        !controller.signal.aborted &&
        epoch === generation.current
      )
        fail(e);
    }
  }
  async function save(e) {
    e.preventDefault();
    if (!confirmed || error || writing.current) return;
    writing.current = true;
    setBusy(true);
    setError(null);
    setNotice("");
    const epoch = generation.current;
    try {
      let path = base,
        body = {},
        etag;
      const fields =
        e.currentTarget.tagName === "FORM"
          ? new FormData(e.currentTarget)
          : null;
      if (view.kind === "create-principal")
        body = { name: fields.get("name").trim() };
      else if (view.kind === "create-key") {
        path += "/" + view.principal.id + "/credentials";
        body = {
          name: fields.get("name").trim(),
          expires_in_days: Number(fields.get("days")),
        };
      } else if (view.kind === "principal-change") {
        path +=
          "/" +
          view.principal.id +
          "/" +
          (view.value.data.enabled ? "disable" : "enable");
        etag = view.value.etag;
      } else {
        path +=
          "/" +
          view.principal.id +
          "/credentials/" +
          view.value.data.id +
          "/" +
          (view.kind === "revoke-key" ? "revoke" : "cleanup");
        etag = view.value.etag;
      }
      const r = await request(context.context, csrf, path, {
        method: "POST",
        body,
        etag,
      });
      if (!alive.current || epoch !== generation.current) return;
      setConfirmed(false);
      if (view.kind === "create-key") {
        secret.current = r.data;
        setView({ kind: "secret", principal: view.principal });
        setNotice("新凭证已激活。请立即安全保存 SK，关闭后无法再次查看。");
        return;
      }
      if (view.kind === "create-principal") {
        setView({ kind: "credentials", principal: r.data });
        setNotice("机器身份已创建。现在可创建独立凭证。");
      } else {
        setView({ kind: "credentials", principal: view.principal });
        setNotice(
          view.kind === "principal-change"
            ? "机器身份状态已更新；旧 token 不会随恢复重新生效。"
            : view.kind === "revoke-key"
              ? "凭证已撤销，旧 token 和后续换取资格已失效。"
              : "本次协议清理核对完成，撤销记录保留。",
        );
      }
      setData(null);
      setAfter("");
      setRevision((n) => n + 1);
    } catch (e) {
      if (alive.current && epoch === generation.current) fail(e);
    } finally {
      writing.current = false;
      if (alive.current) setBusy(false);
    }
  }
  const current = data?.principal?.data || view.principal;
  const returnToKeys = () => {
    secret.current = null;
    choose({ kind: "credentials", principal: view.principal });
    setNotice("只显示凭证元数据。未保存或丢失的 SK 需撤销后新建，不能恢复。");
    setRevision((n) => n + 1);
  };
  return (
    <dialog
      ref={dialog}
      className="machine-dialog"
      aria-labelledby="machine-dialog-title"
      onCancel={(e) => {
        e.preventDefault();
        if (!busy) leave();
      }}
    >
      <header className="dialog-header">
        <h2 id="machine-dialog-title">机器身份与凭证</h2>
        <button
          type="button"
          className="icon-button"
          aria-label="关闭机器身份对话框"
          disabled={busy}
          onClick={leave}
        >
          ×
        </button>
      </header>
      <p>
        <strong>{app.name}</strong>
        {current && <> / {current.name}</>}
      </p>
      {notice && (
        <p role="status" className="notice">
          {notice}
        </p>
      )}
      {error && (
        <p role="alert">
          {errorText(error)}{" "}
          操作不会自动重发。创建结果不明时先返回列表，撤销未保存的凭证，再新建。
          {error.code === "recent_mfa_required" && (
            <a href="/login?force=1">重新验证身份</a>
          )}
        </p>
      )}
      {view.kind === "principals" ? (
        <>
          <p className="section-note">
            机器身份属于当前空间的自有应用，使用独立 AK/SK 换取应用范围的短期
            token。空间所有者可管理，不借用任何人的登录会话。
          </p>
          <div className="actions">
            <button
              disabled={busy || !app.enabled || !app.available}
              onClick={() => choose({ kind: "create-principal" })}
            >
              创建机器身份
            </button>
            <button
              className="secondary"
              onClick={() => {
                setError(null);
                setRevision((n) => n + 1);
              }}
            >
              刷新机器身份
            </button>
          </div>
          {!data ? (
            <p role="status">正在读取机器身份…</p>
          ) : data.data.items.length === 0 ? (
            <p role="status">暂无机器身份。</p>
          ) : (
            <div className="machine-records">
              {data.data.items.map((sp) => (
                <article className="summary-card" key={sp.id}>
                  <h3>{sp.name}</h3>
                  <p>
                    {sp.enabled ? "已启用" : "已停用"} · 凭证总数{" "}
                    {sp.credential_count}
                  </p>
                  <code>{sp.id}</code>
                  <div className="actions">
                    <button
                      className="secondary"
                      onClick={() =>
                        choose({ kind: "credentials", principal: sp })
                      }
                    >
                      管理机器凭证
                    </button>
                  </div>
                </article>
              ))}
            </div>
          )}
          {data?.data.next_cursor && (
            <button
              className="secondary"
              onClick={() => setAfter(data.data.next_cursor)}
            >
              下一页机器身份
            </button>
          )}
        </>
      ) : view.kind === "credentials" ? (
        <>
          {!data ? (
            <p role="status">正在读取机器凭证…</p>
          ) : (
            <>
              <p>机器身份：{current.enabled ? "已启用" : "已停用"}</p>
              <p className="section-note">
                最多两份未过期的有效或待处理凭证。新凭证激活成功后，旧有效凭证最多保留
                24 小时重叠期。待处理或未保存 SK 的凭证请先撤销。
              </p>
              <div className="actions">
                <button
                  disabled={!current.enabled || !app.enabled || !app.available}
                  onClick={() =>
                    choose({ kind: "create-key", principal: current })
                  }
                >
                  创建机器凭证
                </button>
                <button
                  className="secondary"
                  onClick={() => sensitive("principal-change")}
                >
                  {current.enabled ? "停用机器身份" : "恢复机器身份"}
                </button>
                <button
                  className="secondary"
                  onClick={() => {
                    setError(null);
                    setRevision((n) => n + 1);
                  }}
                >
                  刷新机器凭证
                </button>
                <button
                  className="text-button"
                  onClick={() => choose({ kind: "principals" })}
                >
                  返回机器身份列表
                </button>
              </div>
              {data.data.items.length === 0 ? (
                <p role="status">暂无机器凭证。</p>
              ) : (
                <div className="machine-records">
                  {data.data.items.map((key) => (
                    <article className="summary-card" key={key.id}>
                      <h3>{key.name}</h3>
                      <dl>
                        <dt>AK</dt>
                        <dd className="integration-value">{key.access_key}</dd>
                        <dt>状态</dt>
                        <dd>{stateLabel[key.state]}</dd>
                        <dt>到期</dt>
                        <dd>{date(key.expires_at)}</dd>
                        <dt>Audience</dt>
                        <dd className="integration-value">{key.audience}</dd>
                        <dt>Scope</dt>
                        <dd>{key.scope}</dd>
                      </dl>
                      <ProtocolProfile
                        key={key.id + ":" + key.version + ":" + revision}
                        context={context.context}
                        csrf={csrf}
                        tenant={context.tenant_id}
                        application={app.id}
                        principal={view.principal.id}
                        resource={key.id}
                        revision={key.version}
                        kind="machine_credential"
                        disabled={busy}
                        onFailure={onFailure}
                      />
                      <div className="actions">
                        {key.state !== "revoked" ? (
                          <button
                            className="danger-link"
                            onClick={() => sensitive("revoke-key", key)}
                          >
                            撤销机器凭证
                          </button>
                        ) : (
                          <button
                            className="secondary"
                            onClick={() => sensitive("cleanup-key", key)}
                          >
                            清理机器协议登记
                          </button>
                        )}
                      </div>
                    </article>
                  ))}
                </div>
              )}
              {data.data.next_cursor && (
                <button
                  className="secondary"
                  onClick={() => setAfter(data.data.next_cursor)}
                >
                  下一页机器凭证
                </button>
              )}
            </>
          )}
        </>
      ) : view.kind === "secret" && secret.current ? (
        <>
          <p className="section-note">
            SK
            仅在这里显示一次。离开页面、切换空间或关闭后无法找回；如未保存，请返回列表撤销并新建。不要把它放入前端应用、日志或文档。
          </p>
          <dl>
            <dt>AK</dt>
            <dd className="integration-value">
              {secret.current.credential.access_key}
            </dd>
            <dt>一次性 SK</dt>
            <dd>
              <pre className="secret" data-testid="credential-secret">
                {secret.current.secret_key}
              </pre>
            </dd>
            <dt>到期</dt>
            <dd>{date(secret.current.credential.expires_at)}</dd>
          </dl>
          <div className="dialog-actions">
            <button
              className="secondary"
              onClick={async () => {
                try {
                  await navigator.clipboard.writeText(
                    secret.current.secret_key,
                  );
                  setNotice("SK 已复制，请安全保存。");
                } catch {
                  setNotice("无法访问剪贴板，请手动选择并安全保存。");
                }
              }}
            >
              复制一次性 SK
            </button>
            <button onClick={returnToKeys}>我已安全保存 SK</button>
            <button className="secondary" onClick={returnToKeys}>
              关闭并返回凭证列表
            </button>
          </div>
        </>
      ) : view.kind === "loading" ? (
        <p role="status">正在读取最新状态…</p>
      ) : (
        <form onSubmit={save}>
          {view.kind === "create-principal" ? (
            <label>
              机器身份名称
              <input name="name" required maxLength={128} autoFocus />
            </label>
          ) : view.kind === "create-key" ? (
            <>
              <label>
                机器凭证名称
                <input name="name" required maxLength={128} autoFocus />
              </label>
              <label>
                有效天数
                <input
                  name="days"
                  type="number"
                  required
                  min={1}
                  max={90}
                  defaultValue={90}
                />
              </label>
              <p>
                仅提供此应用的 machine:invoke 权限。新 SK
                只在创建成功后显示一次，请准备好安全保存；没有恢复接口。
              </p>
            </>
          ) : (
            <p>
              {view.kind === "principal-change"
                ? view.value.data.enabled
                  ? "将停用此机器身份，所有相关旧 token 与换取资格将失效。恢复后需用尚有效的凭证重新换取 token。"
                  : "将恢复机器身份。旧 token 不会复活，尚有效的凭证可以重新换取 token。"
                : view.kind === "revoke-key"
                  ? "将不可逆撤销此凭证，旧 token 与后续换取资格失效。其他凭证和人的登录保持可用。"
                  : "仅核对并清理匹配的已撤销协议对象，不恢复凭证或 SK。"}
            </p>
          )}
          <label className="confirmation">
            <input
              type="checkbox"
              required
              checked={confirmed}
              onChange={(e) => setConfirmed(e.target.checked)}
            />
            我确认执行当前机器身份或凭证操作
          </label>
          <div className="dialog-actions">
            <button
              type="button"
              className="secondary"
              disabled={busy}
              onClick={() =>
                view.kind === "create-principal"
                  ? choose({ kind: "principals" })
                  : choose({ kind: "credentials", principal: view.principal })
              }
            >
              返回列表核对
            </button>
            <button type="submit" disabled={busy || !confirmed || !!error}>
              {busy
                ? "正在保存…"
                : view.kind === "create-principal"
                  ? "保存机器身份"
                  : view.kind === "create-key"
                    ? "创建并显示一次性 SK"
                    : "确认机器操作"}
            </button>
          </div>
        </form>
      )}
    </dialog>
  );
}
