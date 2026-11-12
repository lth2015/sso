// Verify the actual exported archive with no credentials or external requests.
const { chromium } = require("playwright"),
  assert = require("node:assert/strict"),
  http = require("node:http"),
  fs = require("node:fs"),
  path = require("node:path"),
  os = require("node:os"),
  { execFileSync } = require("node:child_process");
let phase = "archive";
(async () => {
  const root = path.resolve(__dirname, "../.."),
    version = JSON.parse(fs.readFileSync(path.join(root, "api/openapi.json")))
      .info.version;
  const directory = fs.mkdtempSync(
    path.join(os.tmpdir(), "sso-offline-doc-test-"),
  );
  let browser, server;
  try {
    execFileSync(
      "python3",
      [
        "-c",
        `import hashlib,json,sys,zipfile
from pathlib import Path
archive,destination,version=sys.argv[1:]
with zipfile.ZipFile(archive) as z:
 m=json.loads(z.read('MANIFEST.json'))
 assert m['version']==version
 assert all(hashlib.sha256(z.read(n)).hexdigest()==h for n,h in m['files'].items())
 html=z.read('docs/dist/index.html')
 assert b'default-src' in html
 Path(destination).write_bytes(html)
`,
        path.join(
          root,
          ".local/exports/sso-documentation-v" + version + ".zip",
        ),
        path.join(directory, "index.html"),
        version,
      ],
      { stdio: ["pipe", "pipe", "pipe"] },
    );
    const html = fs.readFileSync(path.join(directory, "index.html"));
    server = http.createServer((q, r) => {
      if (q.url !== "/") {
        r.writeHead(404).end();
        return;
      }
      r.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
      r.end(html);
    });
    await new Promise((r) => server.listen(0, "127.0.0.1", r));
    browser = await chromium.launch({
      executablePath: "/usr/bin/chromium",
      headless: true,
      args: ["--no-sandbox"],
    });
    const p = await browser.newPage(),
      origin = "http://127.0.0.1:" + server.address().port,
      outbound = [];
    p.on("request", (r) => {
      if (/^https?:/.test(r.url()) && !r.url().startsWith(origin + "/"))
        outbound.push("external request");
    });
    phase = "content";
    await p.goto(origin + "/");
    const body = await p.locator("main").innerText();
    for (const marker of [
      "文档版本：" + version,
      "API Playground",
      "外部业务服务：Go SDK",
      "受控审计批次导出",
      "邀请凭证更新",
      "协议登记只读核对",
      "外部 User 审计客户端与 CLI",
      "角色绑定选择器分页",
      "组织成员关联独立分页",
      "创建与移动组织：上级选择和移动预览",
      "自有应用的成员准入",
      "应用准入：本地演示手卡",
      "自定义角色策略历史",
    ])
      assert(body.includes(marker));
    for (const width of [1365, 768, 390, 320]) {
      phase = "viewport-" + width;
      await p.setViewportSize({ width, height: 900 });
      assert.equal(
        await p.evaluate(
          () => document.documentElement.scrollWidth > innerWidth + 1,
        ),
        false,
      );
      assert((await p.locator("table").count()) > 0);
    }
    assert.equal(outbound.length, 0);
    console.log(
      "Offline documentation verified: version " +
        version +
        ", ZIP SHA-256 manifest, complete sections, 1365/768/390/320px without document overflow, no external requests",
    );
  } finally {
    if (browser) await browser.close();
    if (server) await new Promise((r) => server.close(r));
    fs.rmSync(directory, { recursive: true, force: true });
  }
})().catch((e) => {
  console.error(
    "Offline documentation failed: phase=" +
      phase +
      ", error=" +
      e.name +
      " (private details withheld)",
  );
  process.exitCode = 1;
});
