import React, { useEffect, useRef, useState } from "react";
import { request, errorText } from "./api.js";
import { permissionLabels } from "./RoleManagement.jsx";
import { validateRolePolicyHistory } from "./role-policy-history.js";
const actions = {
  "role.created": "创建角色",
  "role.updated": "修改角色策略",
  "role.deleted": "删除角色",
};
const effects = (policies) =>
  policies.map((p) => (p.effect === "deny" ? "拒绝" : "允许")).join("、") ||
  "未配置";
export default function RolePolicyHistory({
  context,
  csrf,
  tenant,
  role,
  onClose,
  onFailure,
}) {
  const [data, setData] = useState(null),
    [cursor, setCursor] = useState(""),
    [loading, setLoading] = useState(true),
    [error, setError] = useState(null);
  const dialog = useRef(null),
    alive = useRef(true),
    epoch = useRef(0),
    pending = useRef(null);
  const current = (generation, abort) =>
    alive.current &&
    generation === epoch.current &&
    !abort.signal.aborted &&
    location.hash === "#workspace=" + context;
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
  function close() {
    epoch.current++;
    pending.current?.abort();
    onClose();
  }
  async function load(after) {
    if (pending.current) return;
    const abort = new AbortController(),
      generation = ++epoch.current;
    pending.current = abort;
    setLoading(true);
    setData(null);
    setError(null);
    setCursor(after);
    try {
      const r = await request(
        context,
        csrf,
        "/bff/v1/tenants/" +
          tenant +
          "/roles/" +
          role +
          "/policy-history?limit=20" +
          (after ? "&after=" + after : ""),
        { signal: abort.signal },
      );
      if (current(generation, abort))
        setData(validateRolePolicyHistory(r.data, tenant, role, after));
    } catch (e) {
      if (current(generation, abort)) {
        setError(e);
        if (e.status === 401) onFailure(e);
      }
    } finally {
      if (current(generation, abort)) {
        pending.current = null;
        setLoading(false);
      }
    }
  }
  return (
    <dialog
      className="policy-history-modal"
      ref={dialog}
      aria-labelledby="role-policy-history-title"
      onCancel={(e) => {
        e.preventDefault();
        close();
      }}
    >
      <header className="dialog-header">
        <h2 id="role-policy-history-title">角色策略历史</h2>
        <button
          type="button"
          className="icon-button"
          aria-label="关闭策略历史"
          onClick={close}
        >
          ×
        </button>
      </header>
      <p className="section-note">
        仅包含启用采集后实际捕获的策略变化；空列表不证明从未改动。角色绑定另记审计，这里不会恢复或改写权限。
      </p>
      <p className="muted">
        角色 ID：<code>{role}</code>
      </p>
      {loading && <p role="status">正在读取策略历史…</p>}
      {error && (
        <div className="error" role="alert">
          {errorText(error)}
        </div>
      )}
      {data && (
        <section aria-label="策略变化记录">
          {!data.items.length && <p role="status">没有已捕获的策略变化。</p>}
          {data.items.map((item) => (
            <article key={item.id} className="panel">
              <h3>
                {actions[item.action]} · {item.role_code}
              </h3>
              <p className="muted">
                {new Date(item.occurred_at).toLocaleString("zh-CN")} · 授权版本{" "}
                {item.authz_version}
              </p>
              <p className="muted">
                操作者成员 ID：<code>{item.actor_membership_id}</code>
              </p>
              <div className="table-scroll">
                <table>
                  <thead>
                    <tr>
                      <th>能力</th>
                      <th>变更前</th>
                      <th>变更后</th>
                    </tr>
                  </thead>
                  <tbody>
                    {Object.entries(permissionLabels).map(
                      ([permission, label]) => (
                        <tr key={permission}>
                          <td>{label}</td>
                          <td>
                            {effects(
                              item.before.filter(
                                (p) => p.permission === permission,
                              ),
                            )}
                          </td>
                          <td>
                            {effects(
                              item.after.filter(
                                (p) => p.permission === permission,
                              ),
                            )}
                          </td>
                        </tr>
                      ),
                    )}
                  </tbody>
                </table>
              </div>
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
        {data?.next_cursor && (
          <button
            type="button"
            disabled={loading}
            onClick={() => load(data.next_cursor)}
          >
            下一批策略变化
          </button>
        )}
        <button type="button" className="secondary" onClick={close}>
          关闭
        </button>
      </div>
    </dialog>
  );
}
