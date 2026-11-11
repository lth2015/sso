import React, { useEffect, useRef, useState } from "react";
import { request, errorText } from "./api.js";

export default function OrganizationActions({
  action,
  context,
  tenant,
  csrf,
  etag,
  canWrite,
  onClose,
  onFailure,
  onChanged,
}) {
  const [page, setPage] = useState(null),
    [cursor, setCursor] = useState(""),
    [selected, setSelected] = useState(null),
    [error, setError] = useState(null),
    [busy, setBusy] = useState(false),
    [confirmed, setConfirmed] = useState(false);
  const dialog = useRef(null),
    alive = useRef(true);
  const url = "/bff/v1/tenants/" + tenant + "/organizations/" + action.org.id;
  useEffect(() => {
    dialog.current.showModal();
    return () => {
      alive.current = false;
    };
  }, []);
  useEffect(() => {
    if (action.kind !== "members") return;
    const abort = new AbortController();
    setPage(null);
    setError(null);
    request(
      context,
      csrf,
      url + "/members?limit=20" + (cursor ? "&after=" + cursor : ""),
      { signal: abort.signal },
    )
      .then((r) => {
        if (!abort.signal.aborted) setPage(r);
      })
      .catch((e) => {
        if (!abort.signal.aborted) fail(e);
      });
    return () => abort.abort();
  }, [cursor]);
  function fail(e) {
    setError(e);
    if (e.status === 401 || e.code === "recent_mfa_required") onFailure(e);
  }
  async function submit(e) {
    e.preventDefault();
    if (busy || !confirmed || error) return;
    const rename = action.kind === "rename";
    const body = rename
      ? { name: new FormData(e.currentTarget).get("name").trim() }
      : {};
    setBusy(true);
    try {
      await request(
        context,
        csrf,
        url + (rename ? "/rename" : selected ? "/members/" + selected.id : ""),
        {
          method: rename ? "POST" : "DELETE",
          body,
          etag: selected ? page.etag : etag,
        },
      );
      if (!alive.current) return;
      onChanged({
        invalidates: !rename,
        message: rename
          ? "组织名称已更新。"
          : selected
            ? "组织归属已移除，空间成员资格和角色保留。请重新确认空间授权。"
            : "空组织已删除。请重新确认空间授权。",
      });
    } catch (e) {
      if (alive.current) fail(e);
    } finally {
      if (alive.current) setBusy(false);
    }
  }
  return (
    <dialog
      ref={dialog}
      aria-labelledby="organization-action-title"
      onCancel={(e) => {
        e.preventDefault();
        if (!busy) onClose();
      }}
    >
      <header className="dialog-header">
        <h2 id="organization-action-title">
          {action.kind === "rename"
            ? "重命名组织"
            : action.kind === "delete"
              ? "删除空组织"
              : selected
                ? "移除组织归属"
                : "组织直属成员"}
        </h2>
        <button
          type="button"
          className="icon-button"
          aria-label="关闭组织对话框"
          disabled={busy}
          onClick={onClose}
        >
          ×
        </button>
      </header>
      <p>
        <strong>{action.org.name}</strong>
      </p>
      {error && (
        <p role="alert">
          {errorText(error)} 请关闭对话框并刷新核对；本次操作不会自动重发。
        </p>
      )}
      {action.kind === "members" && !selected ? (
        <>
          <p className="section-note">
            仅显示直接关联，不含下级组织。移除归属会保留空间成员资格及角色；已停用成员的归属也需显式移除。
          </p>
          {!page ? (
            <p role="status">正在读取直属成员…</p>
          ) : page.data.items.length === 0 ? (
            <p role="status">没有直接关联的成员。</p>
          ) : (
            <div className="table-scroll">
              <table>
                <thead>
                  <tr>
                    <th>成员</th>
                    <th>状态</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {page.data.items.map((member) => (
                    <tr key={member.id}>
                      <td>
                        {member.email || member.user_id}
                        <small className="event-code">
                          {member.owner ? "空间所有者" : "空间成员"}
                        </small>
                      </td>
                      <td>
                        {
                          {
                            active: "有效",
                            suspended: "已停用",
                            invited: "受邀",
                            left: "已离开",
                          }[member.status]
                        }
                      </td>
                      <td>
                        {canWrite && (
                          <button
                            className="danger-link"
                            disabled={!!error}
                            onClick={() => {
                              setSelected(member);
                              setConfirmed(false);
                            }}
                          >
                            移除归属
                          </button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <div className="dialog-actions">
            {page?.data.next_cursor && (
              <button
                className="secondary"
                onClick={() => setCursor(page.data.next_cursor)}
              >
                下一页直属成员
              </button>
            )}
            {cursor && (
              <button className="secondary" onClick={() => setCursor("")}>
                返回直属成员首页
              </button>
            )}
            <button type="button" onClick={onClose}>
              关闭
            </button>
          </div>
        </>
      ) : (
        <form onSubmit={submit}>
          {action.kind === "rename" ? (
            <label>
              新组织名称
              <input
                name="name"
                required
                maxLength={128}
                defaultValue={action.org.name}
                autoFocus
              />
            </label>
          ) : selected ? (
            <p>
              将移除 {selected.email || selected.user_id}{" "}
              与当前组织的直接关联。此操作不退出空间、不撤销角色；当前空间的旧授权需要重新确认。
            </p>
          ) : (
            <p>
              仅可删除没有子组织、没有任何成员直接关联的节点。不会级联删除下级、成员或角色；根组织保留。删除后无法恢复，当前空间需重新授权。
            </p>
          )}
          <label className="confirmation">
            <input
              type="checkbox"
              required
              checked={confirmed}
              onChange={(e) => setConfirmed(e.target.checked)}
            />
            我确认对当前组织执行此操作
          </label>
          <div className="dialog-actions">
            {selected && !error && (
              <button
                type="button"
                className="secondary"
                disabled={busy}
                onClick={() => {
                  setSelected(null);
                  setConfirmed(false);
                }}
              >
                返回成员列表
              </button>
            )}
            <button
              type="button"
              className="secondary"
              disabled={busy}
              onClick={onClose}
            >
              取消
            </button>
            <button type="submit" disabled={busy || !confirmed || !!error}>
              {busy ? "正在保存…" : "确认操作"}
            </button>
          </div>
        </form>
      )}
    </dialog>
  );
}
