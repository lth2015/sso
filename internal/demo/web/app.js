'use strict';const $=id=>document.getElementById(id);let csrf='';
async function load(){
 try{const state=await fetch('/session',{redirect:'error'});if(!state.ok){$('session').hidden=true;$('login').hidden=false;return}const session=await state.json();csrf=session.csrf;$('name').textContent=session.application;const result=await fetch('/api/me',{redirect:'error'});if(!result.ok)throw new Error('会话需要重新登录或续期。');const me=await result.json();$('tenant').textContent=me.tenant_id;$('user').textContent=me.user_id;$('session').hidden=false;$('login').hidden=true;}
 catch(error){$('error').textContent=error.message}
}
async function post(path){const response=await fetch(path,{method:'POST',headers:{'X-CSRF-Token':csrf},redirect:'error'});if(!response.ok){$('error').textContent='会话不可用，请重新登录。';return false}return true}
$('refresh').addEventListener('click',async()=>{if(await post('/session/refresh')){$('status').textContent='会话已续期';await load()}});
$('logout').addEventListener('click',async()=>{if(await post('/session/logout')){csrf='';$('session').hidden=true;$('login').hidden=false;$('status').textContent='已退出当前应用'}});
load();
