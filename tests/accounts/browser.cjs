// Real browser -> local HTTP API -> MySQL -> encrypted queue -> private mailbox.
const {chromium}=require('playwright');
const fs=require('node:fs/promises');
const path=require('node:path');
const crypto=require('node:crypto');
const assert=require('node:assert/strict');
const {execFileSync}=require('node:child_process');
const root=path.resolve(__dirname,'../..');
const mailbox=path.join(root,'.local/mailbox');
const email='browser-'+crypto.randomBytes(16).toString('hex')+'@example.invalid';
const created=new Set();
let stage='startup';
const pause=ms=>new Promise(r=>setTimeout(r,ms));
async function proof(purpose){
 for(let i=0;i<100;i++){
  for(const name of await fs.readdir(mailbox)){
   if(!/^[a-f0-9]{32}\.json$/.test(name)) continue;
   const file=path.join(mailbox,name);if(!(await fs.lstat(file)).isFile()) continue;
   let value;try{value=JSON.parse(await fs.readFile(file,'utf8'))}catch{continue}
   if(value.recipient===email && value.purpose===purpose){created.add(file);return value.token}
  }
  await pause(150);
 }
 throw new Error('Expected private mailbox proof was not delivered');
}
(async()=>{
 let browser;
 try{
  browser=await chromium.launch({executablePath:process.env.CHROMIUM_PATH||'/usr/bin/chromium',headless:true,args:['--no-sandbox']});
  const page=await browser.newPage({viewport:{width:1280,height:900}});
  const errors=[];page.on('pageerror',()=>errors.push('page error'));
  stage='open-account-page';
  await page.goto('http://127.0.0.1:8080/account');
  stage='submit-email';
  await page.getByLabel('邮箱地址').fill(email);
  await page.getByRole('button',{name:'发送验证说明',exact:true}).click();
  await page.getByRole('status').filter({hasText:'若地址符合条件'}).waitFor();
  stage='await-registration-mail';
  const token=await proof('register');
  stage='open-verification';
  await page.goto('http://127.0.0.1:8080/account#purpose=register&token='+token);
  await page.waitForFunction(()=>location.hash==='');
  await page.getByLabel('设置新密码',{exact:true}).fill('Local browser initial passphrase 123!');
  await page.getByLabel('再次输入密码',{exact:true}).fill('Mismatch passphrase 456!');
  await page.getByRole('button',{name:'验证邮箱并创建账户',exact:true}).click();
  await page.getByRole('alert').filter({hasText:'两次输入的密码不一致'}).waitFor();
  await page.getByLabel('再次输入密码',{exact:true}).fill('Local browser initial passphrase 123!');
  await page.getByRole('button',{name:'验证邮箱并创建账户',exact:true}).click();
  stage='complete-registration';
  await page.getByRole('status').filter({hasText:'邮箱验证与账户创建完成'}).waitFor();
  await page.getByRole('button',{name:'已有账户，找回密码',exact:true}).click();
  await page.getByLabel('邮箱地址').fill(email);
  await page.getByRole('button',{name:'发送重置说明',exact:true}).click();
  await page.getByRole('status').filter({hasText:'若地址符合条件'}).waitFor();
  stage='await-reset-mail';
  const reset=await proof('reset');
  await page.goto('http://127.0.0.1:8080/account#purpose=reset&token='+reset);
  await page.getByLabel('设置新密码',{exact:true}).fill('Local browser updated passphrase 789!');
  await page.getByLabel('再次输入密码',{exact:true}).fill('Local browser updated passphrase 789!');
  await page.getByRole('button',{name:'保存新密码',exact:true}).click();
  stage='complete-password-reset';
  await page.getByRole('status').filter({hasText:'密码已更新'}).waitFor();
  assert.equal(await page.evaluate(()=>location.hash),'');
  assert.deepEqual(await page.evaluate(()=>[localStorage.length,sessionStorage.length]),[0,0]);
  assert.deepEqual(await page.context().cookies(),[]);
  await page.screenshot({path:path.join(root,'docs/ui/accounts-desktop.png'),fullPage:true});
  await page.setViewportSize({width:390,height:844});
  assert.equal(await page.evaluate(()=>document.documentElement.scrollWidth>innerWidth),false);
  await page.screenshot({path:path.join(root,'docs/ui/accounts-mobile.png'),fullPage:true});
  assert.deepEqual(errors,[]);
  console.log('Account browser acceptance passed: registration, private mailbox, validation, reset, no persisted browser credentials, desktop/mobile');
 }finally{
  if(browser)await browser.close();
  execFileSync('python3',[path.join(__dirname,'cleanup.py'),email],{cwd:root,stdio:'pipe'});
  for(const file of created)await fs.unlink(file);
 }
})().catch(error=>{console.error('Account browser acceptance failed: stage='+stage+', error='+error.name+' (sensitive diagnostics withheld)');process.exitCode=1});
