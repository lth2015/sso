"use strict";
const get = (id) => document.getElementById(id);
const state = document.querySelector('meta[name="security-state"]').content;
let pending = false;
if (state === "ready") {
  const id = document.querySelector('meta[name="security-id"]').content;
  const csrf = document.querySelector('meta[name="security-csrf"]').content;
  const clearCodes = () => {
    get("codes").textContent = "";
  };
  window.addEventListener("pagehide", clearCodes);
  get("security-form").addEventListener("submit", async (event) => {
    event.preventDefault();
    if (pending) return;
    pending = true;
    const button = event.currentTarget.querySelector("button");
    button.disabled = true;
    get("error").textContent = "";
    get("status").textContent = "正在验证身份…";
    const password = get("password").value,
      code = get("code").value;
    get("password").value = "";
    get("code").value = "";
    try {
      const response = await fetch("/v1/sso/recovery/regenerate/" + id, {
        method: "POST",
        redirect: "error",
        headers: { "Content-Type": "application/json", "X-CSRF-Token": csrf },
        body: JSON.stringify({ password, code }),
      });
      const body = await response.json();
      if (!response.ok) {
        get("error").textContent =
          response.status === 429
            ? "验证过于频繁，请稍后再试。"
            : response.status === 503
              ? "服务暂时不可用，请先核对操作结果。"
              : "验证未通过，或此页面已过期。请核对密码并使用新的动态验证码；必要时重新登录并进入本页。";
        return;
      }
      if (
        !Array.isArray(body.recovery_codes) ||
        body.recovery_codes.length !== 10
      )
        throw new Error();
      get("security-form").hidden = true;
      get("codes").textContent = body.recovery_codes.join("\n");
      body.recovery_codes = [];
      get("saved").hidden = false;
      get("status").textContent = "恢复码已更新，旧恢复码和全部会话已失效。";
    } catch {
      get("error").textContent =
        "连接中断，操作可能已完成。不要盲目重试；可用认证器重新登录核对，再重新生成。新码无法重复查看。";
    } finally {
      pending = false;
      button.disabled = false;
      get("confirmed").checked = false;
      if (!get("security-form").hidden) get("status").textContent = "";
    }
  });
  get("done").addEventListener("click", () => {
    clearCodes();
    location.assign("http://console.localhost:3003/login?force=1");
  });
}
