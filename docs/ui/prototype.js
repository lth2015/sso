'use strict';
// Static design artifact: no authentication, credential generation or network API.
const $ = id => document.getElementById(id);
const tenantName = () => $('tenant').selectedOptions[0].textContent;
function render() {
  $('workspace').textContent = tenantName() + ' · 示例环境';
  $('rows').replaceChildren();
  const row = document.createElement('tr');
  const cells = $('tenant').value === 'a'
    ? ['内容生成服务','ak_example_非真实凭证','music:composition:read','示例：已启用']
    : ['实验分析服务','ak_example_非真实凭证','lab:workspace:read','示例：已停用'];
  cells.forEach((text, index) => {
    const td = document.createElement('td');
    const node = document.createElement(index === 3 ? 'span' : index === 1 || index === 2 ? 'code' : 'strong');
    node.textContent = text;
    if (index === 3) node.className = 'badge ' + ($('tenant').value === 'a' ? 'active' : 'disabled');
    td.append(node); row.append(td);
  });
  const td = document.createElement('td');
  const button = document.createElement('button');
  button.textContent = '查看设计规则';
  button.addEventListener('click', () => { $('status').textContent = '撤销必须检查当前租户、权限和最近 MFA；此处不会执行真实撤销。'; });
  td.append(button); row.append(td); $('rows').append(row);
}
$('tenant').addEventListener('change', () => {
  render(); $('create-dialog').close();
  $('status').textContent = '已切换合成租户；真实产品将重新发起授权并清空旧租户缓存。';
});
document.querySelectorAll('[data-page]').forEach(button => button.addEventListener('click', () => {
  document.querySelector('[aria-current]')?.removeAttribute('aria-current');
  button.setAttribute('aria-current','page');
  const credentials = button.dataset.page === 'credentials';
  $('credentials').hidden = !credentials; $('placeholder').hidden = credentials;
  $('page-title').textContent = button.textContent; $('crumb').textContent = button.textContent; $('status').textContent = '';
}));
$('create').addEventListener('click', () => { $('dialog-tenant').textContent = tenantName(); $('create-dialog').showModal(); });
$('cancel').addEventListener('click', () => $('create-dialog').close());
$('confirm').addEventListener('click', () => {
  $('create-dialog').close();
  $('status').textContent = '创建成功后只显示一次 SK；若响应丢失，应撤销并重新创建。此原型没有生成任何密钥。';
});
$('show-login').addEventListener('click', () => $('login-dialog').showModal());
$('close-login').addEventListener('click', () => $('login-dialog').close());
$('finish-login').addEventListener('click', () => $('login-dialog').close());
render();
