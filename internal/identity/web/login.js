'use strict';
const $=id=>document.getElementById(id);
const flow=document.querySelector('[name=flow-id]').content;
const csrf=document.querySelector('[name=flow-csrf]').content;
const kind=document.querySelector('[name=flow-kind]').content;
// Remove protocol challenges immediately; they never go into browser storage.
history.replaceState(null,'',kind==='logout'?'/sso/logout':'/flow');
let stage='';
function state(value){stage=value;$('reject').hidden=false;for(const [id,visible]of [['password-form','password'],['mfa-form','mfa'],['passed','passed'],['consent-form','consent']])$(id).hidden=visible!==value;}
async function post(action,body){
 $('error').textContent='';const buttons=document.querySelectorAll('button');buttons.forEach(b=>b.disabled=true);
 try{
  const url=kind==='logout'?'/v1/sso/logout':'/v1/flows/'+flow+'/'+action;
  const response=await fetch(url,{method:'POST',headers:{'Content-Type':'application/json','X-CSRF-Token':csrf},body:JSON.stringify(body),redirect:'error'});
  if(!response.ok)throw new Error('身份验证未完成。请检查输入；请求过期或次数过多时，请从应用重新发起登录。');
  return response.status===204?{}:await response.json();
 }catch(error){$('error').textContent=error.message;return null}
 finally{buttons.forEach(b=>b.disabled=false)}
}
$('password-form').addEventListener('submit',async event=>{event.preventDefault();const data=await post('password',{email:$('email').value,password:$('password').value});$('password').value='';if(!data)return;state('mfa');$('title').textContent=data.state==='enroll'?'设置双重验证':'验证你的身份';$('description').textContent='输入认证器中的六位动态验证码。';$('setup').hidden=!data.secret;$('secret').textContent=data.secret||'';$('recovery-option').hidden=data.state==='enroll';$('code').focus()});
$('mfa-form').addEventListener('submit',async event=>{event.preventDefault();const data=await post('mfa',{code:$('code').value,recovery_code:$('recovery').value});$('code').value='';$('recovery').value='';if(!data)return;$('secret').textContent='';state('passed');$('title').textContent='身份验证完成';$('description').textContent='继续返回应用的授权流程。';$('codes-panel').hidden=!data.recovery_codes?.length;$('codes').textContent=(data.recovery_codes||[]).join('\n');$('continue').textContent=data.recovery_codes?.length?'我已安全保存，继续':'继续';});
$('continue').addEventListener('click',async()=>{const data=await post('complete',{});if(data?.redirect_to){$('codes').textContent='';location.assign(data.redirect_to)}});
$('consent-form').addEventListener('submit',async event=>{event.preventDefault();const data=await post('consent',{tenant_id:$('tenant').value});if(data?.redirect_to)location.assign(data.redirect_to)});
$('reject').addEventListener('click',async()=>{const data=await post('reject',{});if(data?.redirect_to)location.assign(data.redirect_to)});
$('logout').addEventListener('click',async()=>{const data=await post('',{});if(data){$('logout').hidden=true;$('title').textContent='已退出统一登录';$('description').textContent='所有设备上的统一会话已撤销。应用再次访问时需要重新登录。'}});
(async()=>{
 if(kind==='logout'){$('title').textContent='退出所有设备';$('description').textContent='这会撤销当前账户所有统一登录会话及其后续访问资格。';$('logout').hidden=false;return}
 if(!flow||!csrf){$('title').textContent='请从应用重新发起登录';return}
 try{
  const response=await fetch('/v1/flows/'+flow,{redirect:'error'});if(!response.ok)throw new Error('登录请求无效或已过期，请从应用重新开始。');const data=await response.json();$('application').textContent=data.application;
  if(data.state==='consent'){state('consent');$('title').textContent='确认本次授权';$('description').textContent='选择有访问资格的空间，本次授权不会改变其他标签页的空间。';$('tenant').replaceChildren();for(const tenant of data.tenants){const option=document.createElement('option');option.value=tenant.id;option.textContent=tenant.name;$('tenant').append(option)}const labels={openid:'识别你的账户',offline_access:'在会话有效期内保持登录','demo:read':'查看示范工作台资料','platform:read':'查看身份与空间资料','platform:write':'管理获授权的空间与会话'};$('scopes').textContent=data.scopes.map(scope=>labels[scope]||scope).join('；');if(!data.tenants.length){$('error').textContent='没有可访问的空间。请联系空间管理员确认应用启用和成员访问授权。';$('consent-form').querySelector('button').disabled=true;}}
  else if(data.state==='password'){state('password');$('title').textContent='登录你的账户';$('description').textContent='完成密码与双重验证后，再确认访问空间。';}
  else if(data.state==='enroll'||data.state==='mfa'){state('mfa');$('title').textContent='验证你的身份';$('description').textContent='输入认证器验证码。如尚未保存设置密钥，请重新开始登录。'}
  else if(data.state==='passed'){state('passed');$('title').textContent='身份验证完成';$('continue').textContent='继续'}
  else throw new Error('请从应用重新开始登录。');
 }catch(error){$('error').textContent=error.message}
})();
