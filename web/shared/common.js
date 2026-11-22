const tokenKey='cityflow_token';
const esc=value=>String(value??'').replace(/[&<>"']/g,char=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[char]));
function friendlyError(value,status){
  const text=String(value||'').trim();
  if(text&&!/[A-Za-z]{4,}/.test(text))return text;
  const messages={400:'操作未完成，请检查填写内容。',401:'登录状态已失效，请重新登录。',403:'没有权限执行此操作。',404:'内容不存在或已删除。',409:'数据已更新，请刷新后重试。',429:'操作过于频繁，请稍后重试。'};
  return messages[status]||'服务暂不可用，请稍后重试。';
}
function getToken(){const tab=sessionStorage.getItem(tokenKey),shared=localStorage.getItem(tokenKey),a=tokenClaims(tab),b=tokenClaims(shared);if(!a)return shared;if(b&&a.sub===b.sub&&b.exp>a.exp)return shared;return tab}
function tokenClaims(token){try{const payload=token.split('.')[1].replace(/-/g,'+').replace(/_/g,'/');return JSON.parse(new TextDecoder().decode(Uint8Array.from(atob(payload),c=>c.charCodeAt(0))))}catch{return null}}
function saveToken(token){sessionStorage.setItem(tokenKey,token);localStorage.setItem(tokenKey,token)}
let refreshing=null;
async function refreshAccessToken(used){
  if(refreshing)return refreshing;
  const run=async()=>{const current=getToken();if(current&&current!==used&&tokenClaims(current)?.exp>Date.now()/1000+30)return current;
    const response=await fetch('/api/v1/auth/refresh',{method:'POST',headers:{'Content-Type':'application/json'},credentials:'same-origin'});const body=await response.json().catch(()=>({}));if(!response.ok){const error=new Error(body.error||'登录状态刷新失败');error.status=response.status;throw error}if(tokenClaims(used)?.sub!==tokenClaims(body.access_token)?.sub){const error=new Error('登录账号已切换，请重新登录');error.status=401;throw error}saveToken(body.access_token);return body.access_token};
  refreshing=(navigator.locks?navigator.locks.request('cityflow-auth-refresh',run):run()).finally(()=>{refreshing=null});return refreshing;
}
async function api(path,options={}){let token=getToken();const send=()=>{const headers={'Content-Type':'application/json',...(options.headers||{})};if(token)headers.Authorization=`Bearer ${token}`;return fetch(path,{...options,headers})};let response=await send();if(response.status===401&&token){token=await refreshAccessToken(token);response=await send()}const body=await response.json().catch(()=>({}));if(!response.ok){const error=new Error(friendlyError(body.error||body.message,response.status));error.status=response.status;throw error;}return body}
function showNotice(message,kind='error'){const el=document.querySelector('#notice');if(el){el.textContent=message||'';el.classList.toggle('success',kind==='success')}}
function cfDialog({title='确认操作',message='',input='',value='',placeholder='',confirmText='确定',cancelText='取消',danger=false}={}){
  return new Promise(resolve=>{
    const dialog=document.createElement('dialog');
    dialog.className='editor-dialog cf-dialog';
    dialog.innerHTML=`<form method="dialog"><div class="dialog-top"><div><h2>${esc(title)}</h2></div></div><p class="cf-dialog-message">${esc(message)}</p>${input?`<label class="dialog-field">${esc(input)}<textarea rows="4" maxlength="500"></textarea></label>`:''}<div class="dialog-actions"><button type="button" class="button outline" data-cf-cancel>${esc(cancelText)}</button><button type="submit" class="button ${danger?'danger':'primary'}">${esc(confirmText)}</button></div></form>`;
    document.body.append(dialog);
    const form=dialog.querySelector('form');
    const textarea=dialog.querySelector('textarea');
    if(textarea){textarea.value=value;textarea.placeholder=placeholder}
    let settled=false;
    const finish=result=>{if(settled)return;settled=true;dialog.close();dialog.remove();resolve(result)};
    form.addEventListener('submit',event=>{event.preventDefault();finish(textarea?textarea.value.trim():true)});
    dialog.querySelector('[data-cf-cancel]').addEventListener('click',()=>finish(null));
    dialog.addEventListener('cancel',event=>{event.preventDefault();finish(null)});
    dialog.showModal();
    (textarea||dialog.querySelector('button[type="submit"]')).focus();
  });
}
async function cfConfirm(message,options={}){return (await cfDialog({title:options.title||'确认操作',message,confirmText:options.confirmText||'确定',cancelText:options.cancelText||'取消',danger:Boolean(options.danger)}))===true}
async function cfPrompt(message,options={}){return cfDialog({title:options.title||'填写说明',message,input:options.input||'说明',value:options.value||'',placeholder:options.placeholder||'',confirmText:options.confirmText||'提交',cancelText:options.cancelText||'取消',danger:Boolean(options.danger)})}
async function logout(){const run=async()=>{const response=await fetch('/api/v1/auth/logout',{method:'POST',credentials:'same-origin'});if(!response.ok)throw new Error('退出失败，请稍后重试。');sessionStorage.removeItem(tokenKey);localStorage.removeItem(tokenKey)};try{if(navigator.locks)await navigator.locks.request('cityflow-auth-refresh',run);else await run()}catch(error){showNotice(error.message);return}location.replace('/login')}
function wireLogout(){document.querySelectorAll('[data-logout]').forEach(button=>button.addEventListener('click',logout))}
function routeForRole(role){return ({USER:'/user',MERCHANT:'/merchant',RIDER:'/rider',ADMIN:'/admin'})[role]||'/login'}
async function requirePageRole(role){const token=getToken();if(!token){location.replace('/login');return null}try{const user=await api('/api/v1/auth/me');if(user.role!==role){location.replace(routeForRole(user.role));return null}const welcome=document.querySelector('#welcome');if(welcome)welcome.textContent=user.name;wireLogout();return user}catch(error){if(error.status===401)logout();else {showNotice('网络或服务暂不可用，请刷新重试。');}return null}}
const statusLabels={OPEN:'待接单',CLAIMED:'已接单',ARRIVED:'已到达',PROCESSING:'进行中',COMPLETED:'已完成',CANCELLED:'已取消'};
/* One wheel-zoom behavior for every map: the page handles the wheel itself so
 * Leaflet and AMap step exactly the same way (the native handlers stay off). */
window.CityFlowWheelZoom = (host, map, options = {}) => {
  let enabled = options.enabled !== false;
  let busy = false, pendingSteps = 0, pointer = null, disposed = false;
  let notchDirection = 0, notchCount = 0, notchTimer = 0;
  let finishTimer = 0;
  let activeCleanup = null;
  const minZoom = options.minZoom ?? 2;
  const maxZoom = options.maxZoom ?? 20;
  const notchWindow = 300;
  const clampSteps = value => Math.max(-4, Math.min(4, value));

  function finish() {
    if (disposed || !busy) return;
    clearTimeout(finishTimer);
    finishTimer = 0;
    busy = false;
    if (pendingSteps && enabled) {
      const steps = pendingSteps;
      pendingSteps = 0;
      applySteps(steps);
    }
  }

  function applySteps(steps) {
    if (disposed || !enabled || !steps || !pointer) { pendingSteps = 0; return; }
    const current = Number(map.getZoom?.());
    if (!Number.isFinite(current)) { pendingSteps = 0; return; }
    const next = Math.max(minZoom, Math.min(maxZoom, Math.round(current) + steps));
    if (next === current) { pendingSteps = 0; return; }
    const x = pointer.x, y = pointer.y;
    // Wheel zoom anchors on the pointer. The native +/- controls keep their
    // own center-anchored behavior because this handler only owns wheel events.
    if (map.raw && window.AMap && typeof map.raw.containerToLngLat === 'function' && typeof map.raw.setZoomAndCenter === 'function') {
      const anchor = map.raw.containerToLngLat(new window.AMap.Pixel(x, y));
      // AMap's second argument is the next map center, not the point that must
      // remain under the cursor. Move the center by the inverse zoom ratio so
      // the geographic point under the pointer stays fixed.
      const before = map.raw.getCenter();
      const beforeLng = before?.getLng?.() ?? before?.lng;
      const beforeLat = before?.getLat?.() ?? before?.lat;
      const anchorLng = anchor?.getLng?.() ?? anchor?.lng;
      const anchorLat = anchor?.getLat?.() ?? anchor?.lat;
      const beforeZoom = Number(map.raw.getZoom?.());
      const ratio = Number.isFinite(beforeZoom) ? Math.pow(2, beforeZoom - next) : 0.5;
      if (![beforeLng, beforeLat, anchorLng, anchorLat].every(Number.isFinite)) { pendingSteps = 0; return; }
      const center = new window.AMap.LngLat(
        anchorLng + (beforeLng - anchorLng) * ratio,
        anchorLat + (beforeLat - anchorLat) * ratio,
      );
      busy = true;
      let settled = false, cleaned = false, fallbackTimer = 0;
      const cleanup = () => {
        if (cleaned) return;
        cleaned = true;
        if (typeof map.raw.off === 'function') map.raw.off('zoomend', done);
        clearTimeout(fallbackTimer);
        if (activeCleanup === cleanup) activeCleanup = null;
      };
      const done = () => {
        if (settled || disposed) return;
        const applied = Number(map.raw.getZoom?.());
        if ((!Number.isFinite(applied) || Math.abs(applied - next) > 0.4) && performance.now() < deadline) {
          // A previous zoom's zoomend can arrive before this target is applied.
          // Never start the next queued step from that stale baseline.
          fallbackTimer = setTimeout(done, 60);
          return;
        }
        settled = true;
        cleanup();
        // Let AMap commit the new center for this zoom before the next queued
        // wheel step reads it.
        finishTimer = setTimeout(finish, 40);
      };
      const deadline = performance.now() + 420;
      activeCleanup = cleanup;
      // AMap applies setZoomAndCenter asynchronously. Waiting for zoomend is
      // required; otherwise a second wheel tick reads the stale center/zoom and
      // the next correction is applied from the wrong baseline, which makes a
      // target drift away after repeated wheel steps.
      map.raw.on('zoomend', done);
      fallbackTimer = setTimeout(done, 220);
      try {
        map.raw.setZoomAndCenter(next, center, true);
      } catch (error) {
        settled = true;
        cleanup();
        finish();
      }
      return;
    }
    if (typeof map.containerPointToLatLng === 'function' && typeof map.setZoomAround === 'function') {
      map.setZoomAround(map.containerPointToLatLng([x, y]), next, { animate: false });
      return;
    }
    if (map.raw && typeof map.raw.setZoom === 'function') map.raw.setZoom(next, true);
    else map.setZoom?.(next, { animate: false });
  }

  const handler = event => {
    if (disposed || !enabled || !event.deltaY) return;
    event.preventDefault();
    event.stopPropagation();
    const rect = host.getBoundingClientRect();
    pointer = { x: event.clientX - rect.left, y: event.clientY - rect.top };
    const delta = Number(event.deltaY) || 0;
    const direction = delta < 0 ? 1 : -1;
    // A physical mouse wheel arrives as one large delta and should zoom
    // immediately. Trackpads emit a stream of small deltas, so keep the
    // two-tick accumulator there to avoid a single gesture jumping levels.
    const discrete = event.deltaMode !== 0 || Math.abs(delta) >= 50;
    if (discrete) {
      clearTimeout(notchTimer);
      notchCount = 0;
      notchDirection = 0;
    } else {
      if (direction !== notchDirection) {
        notchDirection = direction;
        notchCount = 0;
        pendingSteps = 0;
      }
      notchCount++;
      clearTimeout(notchTimer);
      notchTimer = setTimeout(() => {
        notchCount = 0;
        notchDirection = 0;
      }, notchWindow);
      if (notchCount < 2) return;
      notchCount = 0;
    }
    if (busy) {
      pendingSteps = clampSteps(pendingSteps + direction);
      return;
    }
    applySteps(direction);
  };

  host.addEventListener('wheel', handler, { passive: false });
  return {
    get enabled() { return enabled; },
    setEnabled(value) {
      enabled = Boolean(value);
      if (!enabled) {
        clearTimeout(notchTimer);
        notchCount = 0;
        notchDirection = 0;
        pendingSteps = 0;
        clearTimeout(finishTimer);
        finishTimer = 0;
        activeCleanup?.();
        busy = false;
      }
      host.classList.toggle('map-wheel-zoom', enabled);
    },
    destroy() {
      disposed = true;
      clearTimeout(notchTimer);
      clearTimeout(finishTimer);
      activeCleanup?.();
      activeCleanup = null;
      busy = false;
      pendingSteps = 0;
      pointer = null;
      host.removeEventListener('wheel', handler);
    },
  };
};
function taskCard(task,actions=''){
  const type=({DELIVERY:'配送',PICKUP:'代取',PURCHASE:'代购',OTHER:'其他'})[task.type]||task.type;
  const shortTime=value=>value?new Date(value).toLocaleString('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'}):'';
  const stops=task.pickup_stops?.length?task.pickup_stops:[{address:task.pickup}];
  const pickup=stops.map((stop,i)=>`<br>取件${stops.length>1?i+1:''}：${esc(stop.address)}${stops.length>1 ? `（${({ARRIVED:'已到达',PICKED_UP:'已取件'})[task.status==='COMPLETED'&&stop.status!=='PICKED_UP'?'PICKED_UP':stop.status] || '待取件'}）` : ''}${stop.pickup_at?` · ${esc(shortTime(stop.pickup_at))}`:' · 立即'}`).join('');
  const amount=Number(task.quoted_amount_fen)>0?` · 任务价格 ¥${(Number(task.quoted_amount_fen)/100).toFixed(2)}`:'';
  const history=['COMPLETED','CANCELLED'].includes(task.status),track=document.querySelector('#openTracking')&&task.worker_id?`<button class="button outline" data-view-tracking="${esc(task.id)}">${history?'历史轨迹':'配送进度'}</button>`:'';
  const riderLink=task.worker_id?`<a class="button primary task-rider-entry" href="/tasks/${encodeURIComponent(task.id)}"><span class="task-rider-avatar" aria-hidden="true">骑</span>任务与骑手</a>`:'';
  return `<article class="task-card"><div class="task-top"><h3 class="task-title">${esc(task.title)}</h3><span class="task-status ${esc(task.status.toLowerCase())}">${esc(statusLabels[task.status]||task.status)}</span></div><p class="task-meta">${esc(type)}${pickup}<br>送达：${esc(task.destination)}${task.delivery_at?` · ${esc(shortTime(task.delivery_at))}`:' · 尽快'}${amount}<br>发布时间：${esc(shortTime(task.created_at))}</p>${task.description?`<p class="task-description">${esc(task.description)}</p>`:''}${actions||track||riderLink?`<div class="task-actions">${actions}${track}${riderLink}</div>`:''}</article>`;
}





