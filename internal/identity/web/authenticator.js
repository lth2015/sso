"use strict";
const get = (id) => document.getElementById(id);
if (document.querySelector('[name="change-state"]').content === "ready") {
  const id = document.querySelector('[name="change-id"]').content;
  const csrf = document.querySelector('[name="change-csrf"]').content;
  let busy = false,
    stage = "pending";
  const clearSecrets = () => {
    get("new-secret").textContent = "";
    get("codes").textContent = "";
    get("password").value = "";
    get("old-code").value = "";
    get("new-code").value = "";
  };
  window.addEventListener("pagehide", clearSecrets);
  function uncertain() {
    stage = "unknown";
    clearSecrets();
    get("old-factor-form").hidden = true;
    get("new-factor-form").hidden = true;
    get("cancel").hidden = true;
    get("relogin").hidden = false;
  }
  async function post(action, body) {
    if (busy) return null;
    busy = true;
    document.querySelectorAll("button").forEach((b) => (b.disabled = true));
    get("error").textContent = "";
    get("status").textContent = "正在验证，请稍候…";
    try {
      const response = await fetch(
        "/v1/sso/authenticator/" + id + "/" + action,
        {
          method: "POST",
          redirect: "error",
          headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
          body: JSON.stringify(body),
        },
      );
      const data = response.status === 204 ? {} : await response.json();
      if (!response.ok) {
        if (action === "confirm" && response.status >= 500) {
          uncertain();
          get("error").textContent =
            "更换结果不明，请使用新认证器重新登录核对，不要重复提交。";
          return null;
        }
        get("error").textContent =
          response.status === 429
            ? "验证过于频繁，请稍后再试。"
            : "验证未通过，或此页面已失效。请核对验证码；请求过期时重新进入页面。";
        return null;
      }
      return data;
    } catch {
      if (action === "confirm") uncertain();
      get("error").textContent =
        action === "confirm"
          ? "连接中断，更换可能已完成。请使用新认证器重新登录核对；不要自动重试，新恢复码无法再次取回。"
          : "连接中断。此阶段未更换认证器，可重新打开管理页再开始；新密钥不会重复返回。";
      return null;
    } finally {
      busy = false;
      document.querySelectorAll("button").forEach((b) => (b.disabled = false));
      get("status").textContent = "";
    }
  }
  get("old-factor-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    if (stage !== "pending" || busy) return;
    const password = get("password").value,
      code = get("old-code").value;
    get("password").value = "";
    get("old-code").value = "";
    const data = await post("start", { password, code });
    if (!data) return;
    stage = "enrolling";
    get("old-factor-form").hidden = true;
    get("new-secret").textContent = data.secret;
    data.secret = "";
    get("new-factor-form").hidden = false;
    get("new-code").focus();
  });
  get("new-factor-form").addEventListener("submit", async (e) => {
    e.preventDefault();
    if (stage !== "enrolling" || busy) return;
    const code = get("new-code").value;
    get("new-code").value = "";
    const data = await post("confirm", { code });
    get("confirmed").checked = false;
    if (!data) return;
    stage = "complete";
    clearSecrets();
    get("new-factor-form").hidden = true;
    get("cancel").hidden = true;
    get("codes").textContent = data.recovery_codes.join("\n");
    data.recovery_codes = [];
    get("completed").hidden = false;
  });
  get("cancel").addEventListener("click", async () => {
    if (stage === "complete" || busy) return;
    const data = await post("cancel", {});
    if (!data) return;
    stage = "cancelled";
    clearSecrets();
    get("old-factor-form").hidden = true;
    get("new-factor-form").hidden = true;
    get("cancel").hidden = true;
    get("status").textContent = "已取消。原认证器、恢复码和会话保持有效。";
  });
  get("done").addEventListener("click", () => {
    clearSecrets();
    location.assign("http://console.localhost:3003/login?force=1");
  });
}
