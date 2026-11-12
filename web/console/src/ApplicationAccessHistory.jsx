import React, { useEffect, useRef, useState } from "react";
import { request, errorText } from "./api.js";
import {
  accessHistory,
  historyActions,
  historyLimit,
} from "./application-access-history.js";

export default function ApplicationAccessHistory({
  context,
  csrf,
  app,
  onClose,
  onFailure,
}) {
  const identity = context.context,
    tenant = context.tenant_id;
  const [page, setPage] = useState(null),
    [cursor, setCursor] = useState(""),
    [loading, setLoading] = useState(true),
    [error, setError] = useState(null);
  const dialog = useRef(null),
    alive = useRef(true),
    epoch = useRef(0),
    pending = useRef(null);
  const current = (generation, controller) =>
    alive.current &&
    generation === epoch.current &&
    !controller.signal.aborted &&
    location.hash === "#workspace=" + identity;
  function close() {
    epoch.current++;
    pending.current?.abort();
    onClose();
  }
  async function load(after) {
    if (pending.current) return;
    const controller = new AbortController(),
      generation = ++epoch.current;
    pending.current = controller;
    setLoading(true);
    setError(null);
    setPage(null);
    setCursor(after);
    try {
      const r = await request(
        identity,
        csrf,
        "/bff/v1/tenants/" +
          tenant +
          "/applications/" +
          app.id +
          "/user-access/history?limit=" +
          historyLimit +
          (after ? "&after=" + after : ""),
        { signal: controller.signal },
      );
      if (current(generation, controller))
        setPage(accessHistory(r, tenant, app.id, after));
    } catch (e) {
      if (current(generation, controller)) {
        setError(e);
        if (e.status === 401) onFailure(e);
      }
    } finally {
      if (current(generation, controller)) {
        pending.current = null;
        setLoading(false);
      }
    }
  }
  useEffect(() => {
    alive.current = true;
    dialog.current.showModal();
    load("");
    return () => {
      alive.current = false;
      epoch.current++;
      pending.current?.abort();
    };
  }, []);
  return (
    <dialog
      className="application-access-history-modal"
      ref={dialog}
      aria-labelledby="application-access-history-title"
      onCancel={(e) => {
        e.preventDefault();
        close();
      }}
    >
      <header className="dialog-header">
        <h2 id="application-access-history-title">应用访问历史</h2>
        <button
          type="button"
          className="icon-button"
          aria-label="关闭应用访问历史"
          onClick={close}
        >
          ×
        </button>
      </header>
      <p>
        <strong>{app.name}</strong>
      </p>
      <p className="section-note">
        仅展示已捕获的实际准入变化。记录中的成员 ID
        属于事件发生时的身份，不代表当前仍可访问；这里不会恢复或修改授权。
      </p>
      {loading && <p role="status">正在读取应用访问历史…</p>}
      {error && (
        <div className="error" role="alert">
          {errorText(error)}
        </div>
      )}
      {page && (
        <section aria-label="应用准入变更记录">
          {!page.items.length && (
            <p role="status">没有已捕获的应用准入变化。</p>
          )}
          {page.items.map((v) => (
            <article className="panel" key={v.id}>
              <h3>{historyActions[v.action]}</h3>
              <p className="muted">
                {new Date(v.occurred_at).toLocaleString("zh-CN")} · 准入版本{" "}
                {v.version}
              </p>
              <p>
                操作者成员 ID：<code>{v.actor_membership_id}</code>
              </p>
              <p>
                目标成员 ID：
                {v.membership_id ? (
                  <code>{v.membership_id}</code>
                ) : (
                  "空间内应用访问模式"
                )}
              </p>
              <p>
                当时模式：
                {v.mode === "all_members"
                  ? "全体有效成员"
                  : "仅指定成员和所有者"}
              </p>
              <p className="muted">
                事件 ID：<code>{v.id}</code>
              </p>
            </article>
          ))}
        </section>
      )}
      <div className="dialog-actions">
        <button
          type="button"
          className="secondary"
          disabled={loading}
          onClick={() => load(cursor)}
        >
          重新读取
        </button>
        {cursor && (
          <button
            type="button"
            className="secondary"
            disabled={loading}
            onClick={() => load("")}
          >
            返回首批
          </button>
        )}
        {page?.next && (
          <button
            type="button"
            disabled={loading}
            onClick={() => load(page.next)}
          >
            下一批访问变化
          </button>
        )}
        <button type="button" className="secondary" onClick={close}>
          关闭
        </button>
      </div>
    </dialog>
  );
}
