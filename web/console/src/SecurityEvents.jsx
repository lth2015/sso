import React, { useEffect, useState } from "react";
import { request, errorText } from "./api.js";
const labels = {
  "account.registered": "账户注册完成",
  "account.reset_requested": "收到密码重置请求",
  "account.password_reset": "密码已重置",
  "mfa.enrolled": "认证器已启用",
  "session.authenticated": "通过统一身份验证",
  "session.logout_all": "退出所有设备",
  "authorization.rejected": "拒绝应用授权",
  "session.revoked": "撤销一个登录会话",
  "session.others_revoked": "撤销其他登录会话",
  "mfa.recovery_regenerated": "重新生成恢复码",
  "mfa.replacement_started": "开始更换认证器",
  "mfa.replacement_cancelled": "取消更换认证器",
  "mfa.authenticator_replaced": "认证器更换完成",
};
export default function SecurityEvents({ context, csrf, onFailure }) {
  const [cursor, setCursor] = useState(""),
    [revision, setRevision] = useState(0),
    [result, setResult] = useState(null);
  const current =
    result?.context === context &&
    result?.cursor === cursor &&
    result?.revision === revision
      ? result
      : null;
  useEffect(() => {
    const abort = new AbortController();
    const binding = { context, cursor, revision };
    request(
      context,
      csrf,
      "/bff/v1/me/security-events?limit=20" +
        (cursor ? "&after=" + cursor : ""),
      { signal: abort.signal },
    )
      .then((response) => {
        if (!abort.signal.aborted)
          setResult({ ...binding, data: response.data });
      })
      .catch((error) => {
        if (abort.signal.aborted) return;
        if (error.status === 401) onFailure(error);
        else setResult({ ...binding, error });
      });
    return () => abort.abort();
  }, [context, cursor, revision]);
  return (
    <section className="security-overview" aria-label="个人安全记录">
      <div className="panel-heading">
        <h2>个人安全记录</h2>
        <button
          className="secondary"
          disabled={!current}
          onClick={() => {
            setCursor("");
            setRevision((n) => n + 1);
          }}
        >
          刷新最新记录
        </button>
      </div>
      <p className="section-note">
        仅显示你自己的安全记录，按时间从新到旧排列。密码重置请求表示系统收到请求，不代表由你本人发起。此列表不包含登录失败、设备或
        IP 监测。
      </p>
      {!current ? (
        <p role="status">正在读取安全记录…</p>
      ) : current.error ? (
        <p role="alert">{errorText(current.error)}</p>
      ) : current.data.items.length === 0 ? (
        <p role="status">暂无可显示的安全记录。</p>
      ) : (
        <>
          <div className="table-scroll">
            <table>
              <thead>
                <tr>
                  <th>时间</th>
                  <th>安全事件</th>
                </tr>
              </thead>
              <tbody>
                {current.data.items.map((item) => (
                  <tr key={item.id}>
                    <td>
                      <time dateTime={item.occurred_at}>
                        {new Date(item.occurred_at).toLocaleString("zh-CN")}
                      </time>
                    </td>
                    <td>
                      {labels[item.action] || "其他安全事件"}
                      <small className="event-code">{item.action}</small>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {current.data.next_cursor && (
            <button
              className="secondary"
              onClick={() => setCursor(current.data.next_cursor)}
            >
              下一页（较早记录）
            </button>
          )}
        </>
      )}
    </section>
  );
}
