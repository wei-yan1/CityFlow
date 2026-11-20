const message=document.querySelector('#message');
const loginForm=document.querySelector('#loginForm');
const registerForm=document.querySelector('#registerForm');
const panels=[...document.querySelectorAll('[data-auth-panel]')];
const tabs=[...document.querySelectorAll('[data-auth-mode]')];

function showMode(mode){
  panels.forEach(panel=>{panel.hidden=panel.dataset.authPanel!==mode});
  tabs.forEach(tab=>tab.classList.toggle('active',tab.dataset.authMode===mode));
  document.title=(mode==='register'?'注册':mode==='reset'?'找回密码':'登录')+' · CityFlow';
  const firstInput=[...panels.find(panel=>panel.dataset.authPanel===mode)?.querySelectorAll('input')||[]][0];
  if(firstInput) firstInput.focus({preventScroll:true});
}

function modeFromPath(){
  if(location.pathname==='/register') return 'register';
  if(location.pathname==='/forgot-password'||location.pathname==='/reset-password') return 'reset';
  return 'login';
}

async function login(email,password){
  const response=await fetch('/api/v1/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({email,password})});
  const result=await response.json().catch(()=>({}));
  if(!response.ok) throw new Error(result.error||'登录失败');
  saveToken(result.access_token);
  location.replace(routeForRole(result.user.role));
}

document.querySelectorAll('[data-email]').forEach(button=>button.addEventListener('click',()=>{document.querySelector('#email').value=button.dataset.email;document.querySelector('#password').value='cityflow123';const role=button.querySelector('strong')?.textContent||'测试';message.textContent=`已填入${role}账号和密码，点击登录继续。`}));
tabs.forEach(tab=>tab.addEventListener('click',()=>showMode(tab.dataset.authMode)));
showMode(modeFromPath());

loginForm.addEventListener('submit',async event=>{
  event.preventDefault();message.textContent='正在登录…';
  try{await login(document.querySelector('#email').value.trim(),document.querySelector('#password').value)}
  catch(error){message.textContent=error.message}
});

registerForm.addEventListener('submit',async event=>{
  event.preventDefault();
  const registerMessage=document.querySelector('#registerMessage');
  registerMessage.textContent='正在创建账号…';
  const email=document.querySelector('#registerEmail').value.trim(),password=document.querySelector('#registerPassword').value;
  try{
    const response=await fetch('/api/v1/auth/register',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({name:document.querySelector('#registerName').value.trim(),email,phone:document.querySelector('#registerPhone').value.trim(),password})});
    const result=await response.json().catch(()=>({}));
    if(!response.ok) throw new Error(result.error||'注册失败');
    registerMessage.textContent='账号已创建，正在登录…';
    await login(email,password);
  }catch(error){registerMessage.textContent=error.message}
});

const resetRequestForm=document.querySelector('#resetRequestForm');
resetRequestForm.addEventListener('submit',async event=>{
  event.preventDefault();
  const notice=document.querySelector('#resetRequestMessage'),email=document.querySelector('#resetEmail').value.trim();
  notice.textContent='正在生成重置码…';
  try{
    const response=await fetch('/api/v1/auth/password-reset/request',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({email})});
    const result=await response.json().catch(()=>({}));
    if(!response.ok) throw new Error(result.error||'找回服务暂不可用');
    document.querySelector('#resetConfirmEmail').value=email;
    if(result.dev_token){
      const box=document.querySelector('#resetTokenBox');
      box.hidden=false;
      document.querySelector('#resetTokenValue').textContent=result.dev_token;
      document.querySelector('#resetToken').value=result.dev_token;
      notice.textContent='重置码已生成，15 分钟内有效。';
    }else{
      notice.textContent=result.message||'如果该邮箱已注册，重置码已生成，请查收邮件。';
    }
  }catch(error){notice.textContent=error.message}
});

document.querySelector('#resetConfirmForm').addEventListener('submit',async event=>{
  event.preventDefault();
  const notice=document.querySelector('#resetConfirmMessage');
  notice.textContent='正在重置密码…';
  try{
    const response=await fetch('/api/v1/auth/password-reset/confirm',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({email:document.querySelector('#resetConfirmEmail').value.trim(),token:document.querySelector('#resetToken').value.trim(),new_password:document.querySelector('#resetPassword').value})});
    const result=response.status===204?{}:await response.json().catch(()=>({}));
    if(!response.ok) throw new Error(result.error||'重置失败');
    document.querySelector('#email').value=document.querySelector('#resetConfirmEmail').value.trim();
    showMode('login');
    message.textContent='密码已重置，请用新密码登录。';
  }catch(error){notice.textContent=error.message}
});

(async()=>{
  if(!getToken()||modeFromPath()!=='login')return;
  try{const user=await api('/api/v1/auth/me');location.replace(routeForRole(user.role))}
  catch(error){if(error.status===401){const tab=tokenClaims(sessionStorage.getItem(tokenKey)),shared=tokenClaims(localStorage.getItem(tokenKey));sessionStorage.removeItem(tokenKey);if(tab?.sub===shared?.sub)localStorage.removeItem(tokenKey)}}
})();
