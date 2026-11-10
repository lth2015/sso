'use strict';
const $=id=>document.getElementById(id);
let purpose='register',token=null;
function consumeFragment(){
 const fragment=new URLSearchParams(location.hash.slice(1));
 if(!fragment.has('token'))return;
 token=fragment.get('token');purpose=fragment.get('purpose')==='reset'?'reset':'register';
 history.replaceState(null,'',location.pathname);
 $('password').value='';$('confirm').value='';$('status').textContent='';$('error').textContent='';
 render();
}
function render(){
 $('email-form').hidden=Boolean(token);$('password-form').hidden=!token;
 $('title').textContent=purpose==='reset'?'重设账户密码':token?'验证邮箱并设置密码':'创建你的账户';
 $('intro').textContent=token?'验证链接仅可使用一次，请在有效期内完成操作。':purpose==='reset'?'提交你的邮箱，我们会发送后续操作说明。':'先验证邮箱归属，再设置密码并创建个人空间。';
 $('request').textContent=purpose==='reset'?'发送重置说明':'发送验证说明';
 $('complete').textContent=purpose==='reset'?'保存新密码':'验证邮箱并创建账户';
 $('switch').textContent=token?'返回邮箱输入':purpose==='reset'?'返回创建账户':'已有账户，找回密码';
}
const errors={invalid_challenge:'链接无效、已过期或已使用，请重新申请。',password_policy:'密码需要 12–128 个字符。',rate_limited:'请求过于频繁，请稍后重试。'};
async function post(path,body,button){
 $('status').textContent='';$('error').textContent='';button.disabled=true;
 try{
  const response=await fetch(path,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body),credentials:'same-origin',redirect:'error'});
  if(!response.ok){let error={};try{error=await response.json()}catch{}throw new Error(errors[error.code]||'暂时无法完成操作，请稍后重试。')}
  return true;
 }catch(error){$('error').textContent=error instanceof Error?error.message:'网络不可用，请稍后重试。';return false}
 finally{button.disabled=false}
}
$('email-form').addEventListener('submit',async event=>{
 event.preventDefault();const path=purpose==='reset'?'/v1/password-resets':'/v1/registrations';
 if(await post(path,{email:$('email').value},$('request'))){$('status').textContent='若地址符合条件，将发送后续操作说明。';$('email').value=''}
});
$('password-form').addEventListener('submit',async event=>{
 event.preventDefault();if($('password').value!==$('confirm').value){$('error').textContent='两次输入的密码不一致。';return}
 const path=purpose==='reset'?'/v1/password-resets/complete':'/v1/email-verifications';
 const completed=await post(path,{token,password:$('password').value},$('complete'));
 $('password').value='';$('confirm').value='';
 if(completed){token=null;render();$('status').textContent=purpose==='reset'?'密码已更新，请从应用重新发起登录。':'邮箱验证与账户创建完成，请从应用发起登录。'}
});
$('switch').addEventListener('click',()=>{if(token){token=null}else{purpose=purpose==='reset'?'register':'reset'};$('password').value='';$('confirm').value='';$('status').textContent='';$('error').textContent='';render();$('email').focus()});
window.addEventListener('hashchange',consumeFragment);
consumeFragment();
render();
