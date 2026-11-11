import { createRequire } from "node:module";
import { readFileSync, writeFileSync, mkdirSync, existsSync } from "node:fs";
import { dirname, resolve, relative } from "node:path";
import { fileURLToPath } from "node:url";
const root = resolve(dirname(fileURLToPath(import.meta.url)), ".."),
  docs = resolve(root, "docs");
const require = createRequire(resolve(root, "web/console/package.json"));
const MarkdownIt = require("markdown-it");
const md = new MarkdownIt({ html: false, linkify: false, typographer: false });
const escape = md.utils.escapeHtml;
const headings = [],
  used = new Map();
md.renderer.rules.heading_open = (tokens, index) => {
  const text = tokens[index + 1].content;
  const base = text
    .toLowerCase()
    .replace(/[^\p{L}\p{N}\s_-]/gu, "")
    .trim()
    .replace(/\s+/g, "-");
  const count = used.get(base) || 0;
  used.set(base, count + 1);
  const id = base + (count ? "-" + count : "");
  if (tokens[index].tag === "h2") headings.push({ text, id });
  return `<${tokens[index].tag} id="${escape(id)}">`;
};
const originalLink =
  md.renderer.rules.link_open ||
  ((tokens, i, options, env, renderer) =>
    renderer.renderToken(tokens, i, options));
md.renderer.rules.link_open = (tokens, index, ...rest) => {
  const href = tokens[index].attrGet("href");
  if (href && !/^(?:[a-z]+:|\/|#)/i.test(href))
    tokens[index].attrSet("href", "../" + href);
  return originalLink(tokens, index, ...rest);
};
const originalImage = md.renderer.rules.image;
md.renderer.rules.image = (tokens, index, ...rest) => {
  const src = tokens[index].attrGet("src"),
    path = resolve(docs, src);
  if (path.startsWith(docs + "/") && existsSync(path) && /\.png$/.test(path))
    tokens[index].attrSet(
      "src",
      "data:image/png;base64," + readFileSync(path).toString("base64"),
    );
  return originalImage(tokens, index, ...rest);
};
const body = md.render(readFileSync(resolve(docs, "handbook.md"), "utf8"));
const toc = headings
  .map((h) => `<a href="#${escape(h.id)}">${escape(h.text)}</a>`)
  .join("\n");
const style = `*{box-sizing:border-box}html{scroll-behavior:smooth}body{margin:0;font:16px/1.8 system-ui,sans-serif;color:#17243a;background:#f4f7fc}header{padding:26px 5vw;background:#172d52;color:white}header p{margin:0;font-size:13px;color:#d5dff3}.layout{display:grid;grid-template-columns:260px minmax(0,1fr);max-width:1500px;margin:auto}nav{position:sticky;top:0;align-self:start;max-height:100vh;overflow:auto;padding:24px 18px}nav a{display:block;font-size:13px;padding:5px 10px;text-decoration:none}main{background:white;padding:36px 5vw;min-width:0}h1,h2,h3,h4,h5,h6{overflow-wrap:anywhere}h1{font-size:32px}h2{border-top:1px solid #dce4f1;margin-top:56px;padding-top:24px;font-size:24px}h3{font-size:20px}h4,h5,h6{font-size:17px}a{color:#2455c9;overflow-wrap:anywhere}a:focus-visible{outline:3px solid #2455c9;outline-offset:3px}pre{overflow:auto;white-space:pre-wrap;overflow-wrap:anywhere;padding:18px;background:#f3f6fb;border:1px solid #dce4f1;border-radius:8px;font:12px/1.7 ui-monospace,monospace}code{font-size:.85em;overflow-wrap:anywhere}table{width:100%;border-collapse:collapse;display:block;overflow:auto;font-size:13px}td,th{padding:10px 12px;border:1px solid #dce4f1;text-align:left}th{background:#edf2fa}img{max-width:100%;height:auto}blockquote{margin-left:0;border-left:3px solid #2455c9;padding-left:18px;color:#526079}footer{padding:24px;text-align:center;font-size:12px;color:#526079}@media(max-width:800px){.layout{display:block}nav{position:static;max-height:300px;border-bottom:1px solid #dce4f1}main{padding:24px 18px}h1{font-size:27px}}@media print{nav{display:none}.layout{display:block}body,main{background:white}pre{max-height:none}a{color:inherit}h2{break-before:page}header{background:white;color:#17243a}}`;
const html = `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'none'"><title>SSO 完整设计与开发手册</title><style>${style}</style></head><body><header><strong>◈ Identity · SSO 完整文档</strong><p>离线阅读 · API / Playground / 架构 / UI / 安全 / 本地验收</p></header><div class="layout"><nav aria-label="文档目录">${toc}</nav><main>${body}</main></div><footer>离线文档不包含凭证，也不发起 API 请求。真实 Playground 在运行中的控制台使用。</footer></body></html>`;
mkdirSync(resolve(docs, "dist"), { recursive: true });
writeFileSync(resolve(docs, "dist/index.html"), html);
console.log("Offline documentation generated: docs/dist/index.html");
