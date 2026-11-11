// Local, single-use machine client reference. Read private AK/SK JSON on stdin;
// fixed loopback destinations, no credential arguments, cache, files or raw logs.
const http = require("node:http");
let phase = "input",
  safeStatus = 0;
const id = /^[a-f0-9]{32}$/;
function request(port, host, method, path, body, headers) {
  return new Promise((resolve, reject) => {
    const req = http.request(
      {
        hostname: "127.0.0.1",
        port,
        method,
        path,
        maxHeaderSize: 16384,
        headers: {
          Host: host,
          ...headers,
          ...(body === undefined
            ? {}
            : { "Content-Length": Buffer.byteLength(body) }),
        },
      },
      (res) => {
        const chunks = [];
        let bytes = 0;
        res.on("data", (chunk) => {
          bytes += chunk.length;
          if (bytes > 8192) {
            res.destroy();
            reject(new Error("response limit"));
            return;
          }
          chunks.push(chunk);
        });
        res.on("error", () => reject(new Error("response unavailable")));
        res.on("end", () => {
          safeStatus = res.statusCode;
          if (
            res.statusCode !== 200 ||
            res.headers["content-type"]?.split(";")[0] !== "application/json"
          ) {
            reject(new Error("response refused"));
            return;
          }
          try {
            resolve(JSON.parse(Buffer.concat(chunks).toString("utf8")));
          } catch {
            reject(new Error("response invalid"));
          }
        });
      },
    );
    req.on("error", () => reject(new Error("request unavailable")));
    const deadline = setTimeout(() => req.destroy(), 3000);
    req.once("close", () => clearTimeout(deadline));
    if (body !== undefined) req.write(body);
    req.end();
  });
}
(async () => {
  const chunks = [];
  let bytes = 0;
  for await (const chunk of process.stdin) {
    bytes += chunk.length;
    if (bytes > 4096) throw new Error("input limit");
    chunks.push(chunk);
  }
  const input = JSON.parse(Buffer.concat(chunks).toString("utf8"));
  if (
    !input ||
    Array.isArray(input) ||
    Object.keys(input).length !== 3 ||
    ["access_key", "secret_key", "application_id"].some(
      (key) => typeof input[key] !== "string",
    ) ||
    !/^ak_[a-f0-9]{32}$/.test(input.access_key || "") ||
    !/^[A-Za-z0-9_-]{43}$/.test(input.secret_key || "") ||
    !id.test(input.application_id || "")
  )
    throw new Error("input invalid");
  phase = "token_exchange";
  const token = await request(
    4464,
    "auth.localhost:4464",
    "POST",
    "/oauth2/token",
    new URLSearchParams({
      grant_type: "client_credentials",
      scope: "machine:invoke",
      audience: "urn:sso:application:" + input.application_id,
    }).toString(),
    {
      "Content-Type": "application/x-www-form-urlencoded",
      Authorization:
        "Basic " +
        Buffer.from(input.access_key + ":" + input.secret_key).toString(
          "base64",
        ),
    },
  );
  if (
    typeof token.access_token !== "string" ||
    token.access_token.length > 16384 ||
    token.scope !== "machine:invoke" ||
    token.token_type?.toLowerCase() !== "bearer" ||
    !Number.isInteger(token.expires_in) ||
    token.expires_in < 1 ||
    token.expires_in > 300 ||
    token.refresh_token ||
    token.id_token
  )
    throw new Error("machine token response invalid");
  phase = "business_resource";
  safeStatus = 0;
  const identity = await request(
    8092,
    "127.0.0.1:8092",
    "GET",
    "/whoami",
    undefined,
    {
      Authorization: "Bearer " + token.access_token,
      Accept: "application/json",
      "Cache-Control": "no-store",
    },
  );
  if (
    identity.principal_type !== "service_principal" ||
    identity.application_id !== input.application_id ||
    identity.scope !== "machine:invoke" ||
    identity.audience !== "urn:sso:application:" + input.application_id ||
    !id.test(identity.tenant_id || "") ||
    !id.test(identity.service_principal_id || "") ||
    !id.test(identity.credential_id || "")
  )
    throw new Error("business identity response invalid");
  const metadata = Object.fromEntries(
    [
      "principal_type",
      "tenant_id",
      "application_id",
      "service_principal_id",
      "credential_id",
      "scope",
      "audience",
    ].map((key) => [key, identity[key]]),
  );
  console.log(JSON.stringify(metadata));
})().catch(() => {
  console.error(
    "Machine client reference failed: " +
      phase +
      (safeStatus ? " HTTP " + safeStatus : "") +
      " (private details withheld)",
  );
  process.exitCode = 1;
});
