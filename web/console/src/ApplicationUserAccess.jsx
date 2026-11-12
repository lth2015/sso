import React, { useEffect, useRef, useState } from "react";
import { request, errorText, ApiError } from "./api.js";
import {
  accessLimit,
  emptyChoices,
  validMember,
  accessPage,
  accessDecision,
  accessWrite,
  accessMembers,
  decisionText,
} from "./user-access.js";
export default function ApplicationUserAccess({
  context,
  csrf,
  app,
  canReadMembers,
  onClose,
  onFailure,
}) {
  const tenant = context.tenant_id,
    identity = context.context,
    base = "/bff/v1/tenants/" + tenant,
    url = base + "/applications/" + app.id + "/user-access";
  const [page, setPage] = useState(null),
    [members, setMembers] = useState(emptyChoices),
    [mode, setMode] = useState(""),
    [member, setMember] = useState(""),
    [decision, setDecision] = useState(null),
    [confirmed, setConfirmed] = useState(false),
    [loading, setLoading] = useState(true),
    [writing, setWriting] = useState(false),
    [error, setError] = useState(null),
    [unavailable, setUnavailable] = useState(false),
    [notice, setNotice] = useState("");
  const dialog = useRef(null),
    alive = useRef(true),
    epoch = useRef(0),
    pending = useRef(null),
    submitted = useRef(false);
  const current = (generation, abort) =>
    alive.current &&
    generation === epoch.current &&
    !abort?.signal.aborted &&
    location.hash === "#workspace=" + identity;
  function clear() {
    setDecision(null);
    setConfirmed(false);
  }
  function fail(e) {
    setNotice("");
    clear();
    setPage(null);
    setMember("");
    setMembers(emptyChoices());
    setUnavailable(true);
    setError(e);
    if (e.status === 401) onFailure(e);
  }
  function close() {
    if (submitted.current) return;
    epoch.current++;
    pending.current?.abort();
    onClose();
  }
  async function initial(generation, abort, expected) {
    const r = await request(identity, csrf, url + "?limit=" + accessLimit, {
      signal: abort?.signal,
    });
    if (!current(generation, abort)) return;
    const next = accessPage(r, tenant, app.id);
    if (expected && expected !== next.etag)
      throw new ApiError(409, "version_conflict");
    let directory = emptyChoices();
    if (canReadMembers) {
      const response = await request(
        identity,
        csrf,
        base + "/members?limit=" + accessLimit,
        { signal: abort?.signal },
      );
      if (!current(generation, abort)) return;
      directory = accessMembers(response);
    }
    if (!current(generation, abort)) return;
    setPage(next);
    setMode(next.policy.mode);
    setMembers(directory);
    setMember("");
    clear();
  }
  useEffect(() => {
    alive.current = true;
    dialog.current.showModal();
    load();
    return () => {
      alive.current = false;
      epoch.current++;
      pending.current?.abort();
    };
  }, []);
  async function load(kind) {
    if (pending.current || submitted.current || unavailable) return;
    const abort = new AbortController(),
      generation = ++epoch.current;
    pending.current = abort;
    setLoading(true);
    setError(null);
    clear();
    try {
      if (!kind) {
        setPage(null);
        await initial(generation, abort);
      } else if (kind === "grants") {
        const r = await request(
          identity,
          csrf,
          url + "?limit=" + accessLimit + "&after=" + page.next,
          { signal: abort.signal },
        );
        if (current(generation, abort))
          setPage(accessPage(r, tenant, app.id, page));
      } else {
        const r = await request(
          identity,
          csrf,
          base + "/members?limit=" + accessLimit + "&after=" + members.next,
          { signal: abort.signal },
        );
        if (current(generation, abort)) setMembers(accessMembers(r, members));
      }
    } catch (e) {
      if (current(generation, abort)) fail(e);
    } finally {
      if (current(generation, abort)) {
        pending.current = null;
        setLoading(false);
      }
    }
  }
  async function check() {
    if (
      pending.current ||
      submitted.current ||
      unavailable ||
      !page ||
      !validMember(member) ||
      mode !== page.policy.mode
    )
      return;
    const abort = new AbortController(),
      generation = ++epoch.current;
    pending.current = abort;
    setLoading(true);
    setError(null);
    clear();
    try {
      const r = await request(identity, csrf, url + "/members/" + member, {
        signal: abort.signal,
      });
      if (current(generation, abort))
        setDecision(accessDecision(r, page.policy, member));
    } catch (e) {
      if (current(generation, abort)) fail(e);
    } finally {
      if (current(generation, abort)) {
        pending.current = null;
        setLoading(false);
      }
    }
  }
  async function change(kind) {
    const targetDecision =
      decision?.membership_id === member &&
      decision.policy.version === page?.policy.version;
    if (
      pending.current ||
      submitted.current ||
      unavailable ||
      !page ||
      !confirmed ||
      location.hash !== "#workspace=" + identity ||
      (kind === "mode"
        ? mode === page.policy.mode
        : !targetDecision || mode !== page.policy.mode)
    )
      return;
    submitted.current = true;
    setWriting(true);
    setError(null);
    clear();
    const generation = ++epoch.current;
    try {
      const r = await request(
        identity,
        csrf,
        url + (kind === "mode" ? "" : "/members/" + member),
        {
          method: kind === "revoke" ? "DELETE" : "PUT",
          body: kind === "mode" ? { mode } : {},
          etag: page.etag,
        },
      );
      if (!current(generation)) return;
      accessWrite(r, page.policy, kind === "mode" ? mode : page.policy.mode);
      await initial(generation, undefined, r.etag);
      if (current(generation))
        setNotice(
          "设置已保存。此应用原有 User 授权需重新登录；机器身份和其他应用不受影响。",
        );
    } catch (e) {
      if (current(generation)) fail(e);
    } finally {
      submitted.current = false;
      if (current(generation)) setWriting(false);
    }
  }
  const disabled = loading || writing || unavailable,
    ready = !!page && mode === page.policy.mode,
    canGrant =
      decision &&
      !decision.explicit_grant &&
      !["membership_inactive", "user_inactive"].includes(decision.reason);
  return (
    <dialog
      className="application-user-access-modal"
      ref={dialog}
      aria-labelledby="application-user-access-title"
      onCancel={(e) => {
        e.preventDefault();
        close();
      }}
    >
      <header className="dialog-header">
        <h2 id="application-user-access-title">应用成员访问</h2>
        <button
          type="button"
          className="icon-button"
          aria-label="关闭应用成员访问"
          disabled={writing}
          onClick={close}
        >
          ×
        </button>
      </header>
      <p>
        <strong>{app.name}</strong>
      </p>
      <p className="section-note">
        控制谁可以登录此应用，不授予平台管理或业务资源权限。有效所有者始终可访问；准入变化后，此应用需要重新登录。
      </p>
      {notice && (
        <p className="notice" role="status">
          {notice}
        </p>
      )}
      {loading && <p role="status">正在读取应用访问设置…</p>}
      {page && (
        <>
          <p className="muted">准入版本 {page.policy.version}</p>
          <label>
            访问模式
            <select
              aria-label="应用访问模式"
              value={mode}
              disabled={disabled}
              onChange={(e) => {
                setMode(e.target.value);
                clear();
              }}
            >
              <option value="all_members">全体有效成员</option>
              <option value="selected_members">仅指定成员和所有者</option>
            </select>
          </label>
          <button
            type="button"
            disabled={disabled || !confirmed || mode === page.policy.mode}
            onClick={() => change("mode")}
          >
            保存访问模式
          </button>
          <hr />
          {canReadMembers && (
            <>
              <label>
                选择空间成员
                <select
                  aria-label="选择应用访问成员"
                  value={
                    members.items.some((v) => v.id === member) ? member : ""
                  }
                  disabled={disabled || !ready}
                  onChange={(e) => {
                    setMember(e.target.value);
                    clear();
                  }}
                >
                  <option value="">请选择成员</option>
                  {members.items.map((v) => (
                    <option key={v.id} value={v.id}>
                      {v.email || v.user_id} ·{" "}
                      {v.status === "active" ? "有效" : "未启用"}
                    </option>
                  ))}
                </select>
              </label>
              {members.next && (
                <button
                  type="button"
                  className="secondary"
                  disabled={disabled}
                  onClick={() => load("members")}
                >
                  加载更多成员
                </button>
              )}
            </>
          )}
          {!canReadMembers && (
            <p className="muted">
              当前权限不读取成员目录。可输入成员 ID 检查和管理应用准入。
            </p>
          )}
          <label>
            成员 ID
            <input
              aria-label="应用访问成员 ID"
              maxLength="32"
              value={member}
              disabled={disabled || !ready}
              onChange={(e) => {
                setMember(e.target.value);
                clear();
              }}
              autoComplete="off"
            />
          </label>
          <button
            type="button"
            className="secondary"
            disabled={disabled || !ready || !validMember(member)}
            onClick={check}
          >
            检查成员访问
          </button>
          {decision && (
            <p className={decision.allowed ? "notice" : "error"} role="status">
              {decision.allowed ? "当前允许访问" : "当前不允许访问"}：
              {decisionText[decision.reason]}。显式授权：
              {decision.explicit_grant ? "已有" : "没有"}。
            </p>
          )}
          <div className="actions">
            <button
              type="button"
              disabled={disabled || !ready || !confirmed || !canGrant}
              onClick={() => change("grant")}
            >
              添加显式授权
            </button>
            <button
              type="button"
              className="secondary"
              disabled={
                disabled || !ready || !confirmed || !decision?.explicit_grant
              }
              onClick={() => change("revoke")}
            >
              移除显式授权
            </button>
          </div>
          <p className="section-note">
            全体成员模式下也可准备显式条目；切换模式会保留这些条目。移除显式条目不会取消所有者资格，也不会阻止全体成员模式下的访问。
          </p>
          <label className="owner-confirm">
            <input
              type="checkbox"
              checked={confirmed}
              disabled={disabled}
              onChange={(e) => setConfirmed(e.target.checked)}
            />
            我确认修改所选应用的准入设置
          </label>
          <h3>显式授权成员</h3>
          <p className="muted">已加载 {page.items.length} 个显式授权</p>
          {page.items.length ? (
            <ul className="user-access-members">
              {page.items.map((v) => (
                <li key={v.membership_id}>
                  <code>{v.membership_id}</code>{" "}
                  <button
                    type="button"
                    className="text-button"
                    disabled={disabled || !ready}
                    onClick={() => {
                      setMember(v.membership_id);
                      clear();
                    }}
                  >
                    选择此成员
                  </button>
                </li>
              ))}
            </ul>
          ) : (
            <p>暂无显式成员授权。</p>
          )}
          {page.next && (
            <button
              type="button"
              className="secondary"
              disabled={disabled}
              onClick={() => load("grants")}
            >
              加载更多显式授权
            </button>
          )}
        </>
      )}
      {error && (
        <div className="error" role="alert">
          <p>{errorText(error)}</p>
          {error.code === "recent_mfa_required" && (
            <a href="/login?force=1">重新验证身份</a>
          )}
        </div>
      )}
      {unavailable && (
        <p className="muted">
          旧选择和检查结果已清除。请关闭后核对最新设置，系统不会自动重发写入。
        </p>
      )}
      <div className="dialog-actions">
        <button
          type="button"
          className="secondary"
          disabled={writing}
          onClick={close}
        >
          关闭
        </button>
        <button
          type="button"
          className="secondary"
          disabled={disabled}
          onClick={() => load()}
        >
          重新读取设置
        </button>
      </div>
    </dialog>
  );
}
