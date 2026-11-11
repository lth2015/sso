export class ApiError extends Error {
  constructor(status, code, requestID = "") {
    super(code);
    this.status = status;
    this.code = code;
    this.requestID = requestID;
  }
}
export async function request(
  context,
  csrf,
  path,
  { method = "GET", body, etag, signal } = {},
) {
  const headers = { "X-Console-Context": context };
  if (method !== "GET") {
    headers["X-CSRF-Token"] = csrf;
    headers["Content-Type"] = "application/json";
  }
  if (etag) headers["If-Match"] = etag;
  let response;
  try {
    response = await fetch(path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal,
      redirect: "error",
    });
  } catch (error) {
    if (error.name === "AbortError") throw error;
    throw new ApiError(0, "network_unavailable");
  }
  let data = null;
  if (response.status !== 204) {
    try {
      data = await response.json();
    } catch {
      throw new ApiError(502, "temporarily_unavailable");
    }
  }
  if (!response.ok)
    throw new ApiError(
      response.status,
      data?.code || "temporarily_unavailable",
      data?.request_id || response.headers.get("X-Request-ID") || "",
    );
  return { data, etag: response.headers.get("ETag") };
}
export const errorText = (error) =>
  ({
    network_unavailable:
      "连接中断，操作结果可能已保存。请刷新核对后再决定是否重试。",
    recent_mfa_required: "请重新验证身份，再发起这项操作。",
    reserved_role: "内置所有者角色不能编辑或删除。",
    protected_application: "平台管理入口必须保持启用。",
    application_unavailable: "应用已被平台暂停，当前空间无法恢复。",
    role_in_use: "请先移除该角色的所有成员绑定。",
    last_active_owner: "至少需要保留一位有效的空间所有者。",
    version_conflict: "资料已被其他操作更新，请刷新后重新确认。",
    already_exists: "相同记录已存在，请核对后再操作。",
    root_cannot_move: "空间根组织不能移动。",
    organization_cycle: "不能把组织移动到它自身或下级组织中。",
    forbidden: "你没有执行这项管理操作的权限。",
    insufficient_scope: "当前授权不包含这项操作，请重新登录。",
    invalid_request: "请检查输入。邀请码需有效、未过期且尚未使用。",
    not_found: "找不到这项记录，或它不属于当前空间。",
    rate_limited: "操作过于频繁，请稍后再试。",
    temporarily_unavailable: "服务暂时不可用，请稍后刷新。",
  })[error?.code] || "操作未完成，请核对资料或稍后重试。";
