// Optional UI design validation; requires the environment's Playwright and Chromium.
const { chromium } = require('playwright');
const http = require('node:http');
const fs = require('node:fs');
const path = require('node:path');
const assert = require('node:assert/strict');
const root = path.resolve(__dirname, '../../docs/ui');
const allowed = {'/':'index.html','/prototype.css':'prototype.css','/prototype.js':'prototype.js'};
const server = http.createServer((req,res) => {
  const file = allowed[req.url];
  if (!file) {res.writeHead(404);res.end();return;}
  res.setHeader('Content-Type',file.endsWith('.css')?'text/css':file.endsWith('.js')?'text/javascript':'text/html; charset=utf-8');
  fs.createReadStream(path.join(root,file)).pipe(res);
});
(async () => {
  await new Promise(resolve => server.listen(0,'127.0.0.1',resolve));
  let browser;
  try {
    browser = await chromium.launch({executablePath:process.env.CHROMIUM_PATH || '/usr/bin/chromium',headless:true,args:['--no-sandbox']});
    const page = await browser.newPage({viewport:{width:1440,height:1024}});
    const errors=[];page.on('pageerror',e=>errors.push(e.message));
    await page.goto(`http://127.0.0.1:${server.address().port}/`);
    await page.getByRole('button',{name:'创建凭证示意'}).click();
    assert((await page.locator('#dialog-tenant').textContent()).includes('北辰'));
    await page.keyboard.press('Escape');
    assert(await page.locator('#create').evaluate(e=>e===document.activeElement));
    await page.locator('#tenant').selectOption('b');
    assert((await page.locator('#rows').textContent()).includes('实验分析服务'));
    await page.getByRole('button',{name:'创建凭证示意'}).click();
    await page.locator('#create-dialog').getByRole('button',{name:'关闭',exact:true}).click();
    assert.equal(await page.locator('#create-dialog').evaluate(e=>e.open),false);
    await page.getByRole('button',{name:'查看认证流程'}).click();
    await page.getByRole('button',{name:'返回控制台原型'}).click();
    await page.locator('#tenant').selectOption('a');
    await page.screenshot({path:path.join(root,'desktop.png'),fullPage:true});
    await page.setViewportSize({width:390,height:844});
    assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>window.innerWidth),false);
    await page.screenshot({path:path.join(root,'mobile.png'),fullPage:true});
    assert.deepEqual(errors,[]);
    console.log('UI checks passed: desktop/mobile, tenant switch, dialogs, Escape/focus, no JS errors');
  } finally {if(browser) await browser.close();await new Promise(resolve=>server.close(resolve));}
})().catch(e=>{console.error(e);server.close();process.exitCode=1;});
