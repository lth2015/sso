import React, { useEffect, useState } from "react";
import { request, errorText } from "./api.js";
export default function MachineEvents({
  context,
  csrf,
  tenant,
  revision,
  onFailure,
}) {
  const [items, setItems] = useState(null),
    [after, setAfter] = useState(""),
    [next, setNext] = useState(null),
    [error, setError] = useState(null),
    [reload, setReload] = useState(0);
  useEffect(() => {
    const abort = new AbortController();
    setItems(null);
    setNext(null);
    setError(null);
    request(
      context,
      csrf,
      "/bff/v1/tenants/" +
        tenant +
        "/machine-events?limit=20" +
        (after ? "&after=" + after : ""),
      { signal: abort.signal },
    )
      .then((r) => {
        if (!abort.signal.aborted) {
          setItems(r.data.items);
          setNext(r.data.next_cursor);
        }
      })
      .catch((e) => {
        if (!abort.signal.aborted) {
          setError(e);
          if (e.status === 401) onFailure(e);
        }
      });
    return () => abort.abort();
  }, [context, tenant, after, revision, reload]);
  return (
    <section className="panel" aria-label="机器签发审计">
      <div className="panel-heading">
        <h2>机器签发记录</h2>
        <button
          className="secondary"
          onClick={() => {
            setAfter("");
            setReload((n) => n + 1);
          }}
        >
          刷新机器签发记录
        </button>
      </div>
      <p className="section-note">
        记录机器身份和凭证的签发动作；不把创建者当作机器调用人，不包含 token 或
        SK。
      </p>
      {error ? (
        <p role="alert">{errorText(error)}</p>
      ) : !items ? (
        <p role="status">正在读取机器签发记录…</p>
      ) : items.length === 0 ? (
        <p role="status">暂无机器签发记录。</p>
      ) : (
        <div className="table-scroll">
          <table>
            <thead>
              <tr>
                <th>时间</th>
                <th>机器身份</th>
                <th>凭证</th>
                <th>应用</th>
              </tr>
            </thead>
            <tbody>
              {items.map((e) => (
                <tr key={e.id}>
                  <td>{new Date(e.occurred_at).toLocaleString("zh-CN")}</td>
                  <td>
                    <code>{e.service_principal_id}</code>
                  </td>
                  <td>
                    <code>{e.credential_id}</code>
                  </td>
                  <td>
                    <code>{e.application_id}</code>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
      {next && (
        <button className="secondary" onClick={() => setAfter(next)}>
          下一页机器签发
        </button>
      )}
      {after && (
        <button className="text-button" onClick={() => setAfter("")}>
          返回机器签发首页
        </button>
      )}
    </section>
  );
}
