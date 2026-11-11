import React, { useEffect, useState } from "react";
import { request, errorText } from "./api.js";
export default function AccountSecurity({
  context,
  csrf,
  onRevoke,
  onFailure,
}) {
  const [value, setValue] = useState(null),
    [error, setError] = useState(null);
  useEffect(() => {
    const abort = new AbortController();
    request(context, csrf, "/bff/v1/me/security", { signal: abort.signal })
      .then((response) => {
        if (!abort.signal.aborted) setValue(response.data);
      })
      .catch((e) => {
        if (!abort.signal.aborted) {
          if (e.status === 401) onFailure(e);
          else setError(e);
        }
      });
    return () => abort.abort();
  }, [context]);
  return (
    <section aria-label="账户安全概览" className="security-overview">
      <h2>账户安全概览</h2>
      {error ? (
        <p role="alert">{errorText(error)}</p>
      ) : !value ? (
        <p role="status">正在读取安全状态…</p>
      ) : (
        <div className="summary-grid">
          <article className="summary-card">
            <p>双重验证</p>
            <h3>{value.mfa_enabled ? "已启用认证器" : "未启用"}</h3>
          </article>
          <article className="summary-card">
            <p>可用恢复码</p>
            <h3>{value.recovery_codes_remaining} 个</h3>
          </article>
          <article className="summary-card">
            <p>有效统一登录会话</p>
            <h3>{value.active_sessions} 个</h3>
          </article>
        </div>
      )}
      <p className="section-note">
        恢复码重新生成需要在统一认证域验证当前密码和新的认证器验证码；完成后旧恢复码和全部旧会话失效。
      </p>
      <div className="actions">
        <a
          className="button secondary"
          href="http://auth.localhost:8080/sso/security"
        >
          重新生成恢复码
        </a>
        <a
          className="button secondary"
          href="http://auth.localhost:8080/sso/security/authenticator"
        >
          更换认证器
        </a>
        <button className="secondary" onClick={onRevoke}>
          撤销其他会话
        </button>
      </div>
    </section>
  );
}
