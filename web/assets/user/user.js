const createTaskForm = document.querySelector('#taskForm');
let ownTasks = [];
let editingTaskID = '', editSnapshot = null, submitting = false, taskOffset=0, pendingCreateKey=null, pendingCreateBody='', taskLoadGeneration=0, displayedTaskOffset=0, paymentTask=null, paymentOrder=null, paymentQuotes=[], paymentQuote=null;
window.CityFlowUserTasks = () => ownTasks;

async function loadTasks() {
  const generation=++taskLoadGeneration;
  try {
    const result = await api(`/api/v1/tasks?limit=50&offset=${taskOffset}`);
    if(generation!==taskLoadGeneration)return;
    displayedTaskOffset=taskOffset;
    ownTasks = result.data || [];
    await Promise.all(ownTasks.filter(item => item.status === 'OPEN' && Number(item.quoted_amount_fen) > 0).map(async item => {
      try { const summary = await api(`/api/v1/tasks/${encodeURIComponent(item.id)}/payment`); item.paymentStatus = summary.payment?.status || ''; }
      catch { item.paymentStatus = ''; }
    }));
    if(generation!==taskLoadGeneration)return;
    const selected=document.querySelector('#trackedTask')?.value;if(selected&&ownTasks.some(task=>task.id===selected))window.CityFlowRenderTrackingDetails?.(selected);
    document.querySelector('#taskCount').textContent = `当前 ${ownTasks.length} 项`;document.querySelector('#taskPrevious').disabled=taskOffset===0;document.querySelector('#taskNext').disabled=ownTasks.length<50;
    document.querySelector('#taskList').innerHTML = ownTasks.length
      ? ownTasks.map(item => {
        const priced = item.status==='OPEN' && Number(item.quoted_amount_fen)>0;
        const refundLocked = item.paymentStatus === 'REFUND_PENDING' || item.paymentStatus === 'REFUND_FAILED';
        const refundHint = item.paymentStatus === 'REFUND_FAILED' ? '退款失败' : '退款处理中';
        const pay = priced && item.paymentStatus !== 'PAID' ? (refundLocked ? `<span class="task-paid-hint">${refundHint}</span>` : `<button class="button primary" data-pay-task="${esc(item.id)}">支付 ¥${(Number(item.quoted_amount_fen)/100).toFixed(2)}</button>`) : priced ? '<span class="task-paid-hint">已支付 · 等待骑手接单</span>' : '';
        return taskCard(item, `${pay}${item.status==='OPEN'?`<button class="button outline" data-edit-task="${esc(item.id)}">编辑</button>`:''}${['OPEN','CLAIMED','ARRIVED'].includes(item.status)?`<button class="button danger" data-cancel-task="${esc(item.id)}">取消任务</button>`:''}`);
      }).join('')
      : `<div class="empty-state">暂无任务。</div>`;
  } catch (error) { if(generation===taskLoadGeneration){taskOffset=displayedTaskOffset;showNotice('任务列表加载失败，请稍后重试。');} }
}

async function loadProfile() {
  try {
    const account = await api('/api/v1/users/me');
    const form = document.querySelector('#profileForm');
    for (const key of ['name', 'email', 'phone', 'avatar_url']) form.elements[key].value = account[key] || '';
  } catch (error) { showNotice('账号资料加载失败，请稍后重试。'); }
}

function openTaskEditor(id) {
  const item = ownTasks.find(task => task.id === id);
  if (!item) return;
  editingTaskID = id;
  const form = createTaskForm, dialog = document.querySelector('#taskDialog');
  editSnapshot = {parent:form.parentNode,next:form.nextSibling,draft:CityFlowTaskCompose.draft(),fields:Object.fromEntries(new FormData(form).entries())};
  document.querySelector('#taskEditHost').append(form);
  for (const key of ['title','type','description','quoted_amount','contact_name','contact_phone','handover_note']) form.elements[key].value = key === 'quoted_amount' ? (item.quoted_amount_fen ? (Number(item.quoted_amount_fen) / 100).toFixed(2) : '') : (item[key] || '');
  CityFlowTaskCompose.setDraft({stops:(item.pickup_stops?.length ? item.pickup_stops : [{address:item.pickup,location:item.pickup_location}]).map(stop=>({...stop})),destination:{address:item.destination,location:item.destination_location,poi_id:item.destination_poi_id},deliveryAt:item.delivery_at || null,contact:{name:item.contact_name||'',phone:item.contact_phone||'',note:item.handover_note||'',description:item.description||''}});
  form.querySelector('[type=submit]').textContent='保存修改'; dialog.showModal();

}

async function cancelTask(id,button) {
  const item=ownTasks.find(task=>task.id===id);
  if (!(await cfConfirm(item?.status==='OPEN'?'取消后任务将关闭。':'骑手可能已开始履约，取消后任务将停止。',{title:'取消任务',confirmText:'取消任务',cancelText:'返回',danger:true}))) return;
  button.disabled=true;
  try {
    await api(`/api/v1/tasks/${encodeURIComponent(id)}`, { method: 'DELETE' });
    showNotice('任务已取消。', 'success');
    await loadTasks();
  } catch (error) { button.disabled=false;showNotice(error.status===409?'任务状态已变化，请刷新后重试。':'任务取消失败，请稍后重试。'); }
}

function paymentLabel(status) {
  return ({PENDING:'等待支付',PAID:'已支付',REFUND_PENDING:'退款处理中',REFUND_FAILED:'退款失败',REFUNDED:'已退款',CLOSED:'支付已关闭',FAILED:'支付失败'})[status] || status || '—';
}

function fenText(fen) {
  return `¥${(Number(fen || 0) / 100).toFixed(2)}`;
}

function setPaymentBreakdown(originalFen, discountFen, payableFen) {
  const original = Number(originalFen || 0), discount = Number(discountFen || 0);
  const payable = Number(payableFen ?? Math.max(original - discount, 1));
  document.querySelector('#paymentOriginal').textContent = fenText(original);
  document.querySelector('#paymentDiscount').textContent = discount > 0 ? `-${fenText(discount)}` : '—';
  document.querySelector('#paymentPayable').textContent = fenText(payable);
}

function syncPaymentQuote() {
  if (paymentOrder && !['CLOSED', 'FAILED'].includes(paymentOrder.status)) return;
  const select = document.querySelector('#paymentCoupon');
  const hint = document.querySelector('#paymentCouponHint');
  paymentQuote = paymentQuotes.find(item => item.user_coupon?.id === select.value) || null;
  if (!paymentTask) return;
  if (paymentQuote) {
    setPaymentBreakdown(paymentQuote.original_amount_fen, paymentQuote.discount_fen, paymentQuote.payable_amount_fen);
    hint.textContent = `已使用「${paymentQuote.coupon?.title || '优惠券'}」，平台优惠 ${fenText(paymentQuote.discount_fen)}。`;
  } else {
    setPaymentBreakdown(Number(paymentTask.quoted_amount_fen || 0), 0, Number(paymentTask.quoted_amount_fen || 0));
    const best = paymentQuotes.reduce((max, item) => Math.max(max, Number(item.discount_fen || 0)), 0);
    hint.innerHTML = paymentQuotes.length
      ? `有 ${paymentQuotes.length} 张可用优惠券，本单最高优惠 ${fenText(best)}。`
      : '暂无可用优惠券。';
  }
}

function renderPaymentCoupons(quotes) {
  const select = document.querySelector('#paymentCoupon');
  paymentQuotes = Array.isArray(quotes) ? quotes : [];
  select.innerHTML = '<option value="">不使用优惠券</option>' + paymentQuotes.map(item => {
    const title = item.coupon?.title || '优惠券';
    return `<option value="${esc(item.user_coupon?.id || '')}">${esc(title)} · 优惠 ${esc(fenText(item.discount_fen))}</option>`;
  }).join('');
  if (paymentOrder?.coupon_claim_id && paymentQuotes.some(item => item.user_coupon?.id === paymentOrder.coupon_claim_id)) {
    select.value = paymentOrder.coupon_claim_id;
  } else if (!paymentOrder && paymentQuotes.length) {
    const best = [...paymentQuotes].sort((a, b) => Number(b.discount_fen || 0) - Number(a.discount_fen || 0))[0];
    if (best?.user_coupon?.id) select.value = best.user_coupon.id;
  }
  select.disabled = Boolean(paymentOrder && !['CLOSED', 'FAILED'].includes(paymentOrder.status));
  syncPaymentQuote();
}

async function loadPaymentCoupons(task) {
  const hint = document.querySelector('#paymentCouponHint');
  const select = document.querySelector('#paymentCoupon');
  select.innerHTML = '<option value="">正在加载可用优惠券…</option>';
  select.disabled = true;
  hint.textContent = '正在计算可用优惠…';
  try {
    const result = await api(`/api/v1/coupons/task/${encodeURIComponent(task.id)}`);
    renderPaymentCoupons(result.items);
  } catch (error) {
    paymentQuotes = [];
    select.innerHTML = '<option value="">不使用优惠券</option>';
    select.disabled = false;
    hint.textContent = '优惠券查询失败，将按原价支付。';
  }
}

async function refreshCouponBadge() {
  const badge = document.querySelector('#couponBadge');
  if (!badge) return;
  try {
    const result = await api('/api/v1/coupons/mine?status=UNUSED&limit=50');
    const count = (result.items || []).length;
    badge.textContent = count > 99 ? '99+' : String(count);
    badge.hidden = count === 0;
  } catch { badge.hidden = true; }
}

function renderPayment(result = null) {
  const dialog = document.querySelector('#paymentDialog');
  const record = result?.payment || paymentOrder;
  if (record) paymentOrder = record;
  const status = document.querySelector('#paymentStatus');
  const host = document.querySelector('#paymentResult');
  if (!paymentOrder) {
    host.innerHTML = '';
    status.textContent = '';
    if (paymentTask) setPaymentBreakdown(Number(paymentTask.quoted_amount_fen || 0), 0, Number(paymentTask.quoted_amount_fen || 0));
    return;
  }
  const paid = paymentOrder.status === 'PAID' || result?.already_paid;
  status.style.color = paid ? '#267151' : '';
  status.textContent = paid ? '支付已完成。' : '';
  const original = Number(paymentOrder.original_amount_fen || paymentOrder.amount_fen || 0);
  const discount = Number(paymentOrder.discount_fen || 0);
  setPaymentBreakdown(original, discount, Number(paymentOrder.amount_fen || 0));
  const couponSelect = document.querySelector('#paymentCoupon');
  couponSelect.disabled = !['CLOSED', 'FAILED'].includes(paymentOrder.status);
  const couponHint = document.querySelector('#paymentCouponHint');
  if (paymentOrder.coupon_title) {
    couponHint.textContent = `已使用「${paymentOrder.coupon_title}」，平台优惠 ${fenText(discount)}。`;
  }
  const payload = result?.qr_code || paymentOrder.pay_payload || '';
  const payURL = result?.pay_url || paymentOrder.pay_url || '';
  const reference = paymentOrder.provider_order_no || (paymentOrder.id ? `${paymentOrder.id.slice(0, 8)}…` : '—');
  host.innerHTML = `
    <div class="payment-meta"><span>支付编号</span><code>${esc(reference)}</code></div>
    <div class="payment-meta"><span>渠道</span><strong>${esc({MOCK:'平台支付',ALIPAY:'支付宝',WECHAT:'微信支付'}[paymentOrder.provider] || paymentOrder.provider)}</strong><span>金额</span><strong>${fenText(paymentOrder.amount_fen)}</strong></div>
    ${paymentOrder.coupon_title ? `<div class="payment-meta"><span>优惠券</span><strong>${esc(paymentOrder.coupon_title)}</strong><span>平台补贴</span><strong>${fenText(discount)}</strong></div>` : ''}
    <div class="payment-meta"><span>状态</span><strong>${esc(paymentLabel(paymentOrder.status))}</strong>${paymentOrder.expires_at ? `<span>有效期至</span><small>${esc(new Date(paymentOrder.expires_at).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }))}</small>` : ''}</div>
    ${payload && !paid ? `<div class="payment-code"><span>付款码</span><code>${esc(payload)}</code><button type="button" class="button outline" data-copy-payment>复制付款码</button></div>` : ''}
    ${payURL && !paid ? `<div class="payment-action"><a class="button outline" href="${esc(payURL)}" target="_blank" rel="noopener noreferrer">打开支付页面</a></div>` : ''}
    <div class="payment-actions">
      ${paymentOrder.status === 'PENDING' && paymentOrder.provider === 'MOCK' ? '<button type="button" class="button primary" data-mock-payment>确认支付</button>' : ''}
      ${paymentOrder.status === 'PENDING' ? '<button type="button" class="button outline" data-sync-payment>我已支付，查询结果</button>' : ''}
    </div>`;
  host.querySelector('[data-copy-payment]')?.addEventListener('click', async () => {
    try { await navigator.clipboard.writeText(payload); status.textContent = '付款码已复制。'; }
    catch { status.textContent = '复制失败，请手动复制。'; }
  });
  host.querySelector('[data-mock-payment]')?.addEventListener('click', async event => {
    const button = event.currentTarget; button.disabled = true; status.textContent = '正在确认支付…';
    try {
      const response = await api(`/api/v1/payments/${encodeURIComponent(paymentOrder.id)}/mock-pay`, { method: 'POST', body: '{}' });
      renderPayment({ payment: response.payment });
      await loadTasks(); await refreshCouponBadge();
    } catch (error) { status.style.color = '#a04b3e'; status.textContent = '操作失败，请稍后重试。'; button.disabled = false; }
  });
  host.querySelector('[data-sync-payment]')?.addEventListener('click', async event => {
    const button = event.currentTarget; button.disabled = true; status.textContent = '正在查询支付结果…';
    try {
      const response = await api(`/api/v1/payments/${encodeURIComponent(paymentOrder.id)}/sync`, { method: 'POST', body: '{}' });
      renderPayment({ payment: response.payment });
      await loadTasks(); await refreshCouponBadge();
    } catch (error) { status.style.color = '#a04b3e'; status.textContent = '操作失败，请稍后重试。'; button.disabled = false; }
  });
  dialog.dataset.paymentID = paymentOrder.id;
}

function openPayment(id) {
  const task = ownTasks.find(item => item.id === id);
  if (!task) return;
  paymentTask = task; paymentOrder = null; paymentQuote = null;
  document.querySelector('#paymentTaskName').value = task.title || '配送任务';
  document.querySelector('#paymentForm').elements.provider.value = 'MOCK';
  document.querySelector('#paymentGenerate').disabled = false;
  document.querySelector('#paymentDialog').showModal();
  setPaymentBreakdown(Number(task.quoted_amount_fen || 0), 0, Number(task.quoted_amount_fen || 0));
  renderPayment();
  void loadPaymentCoupons(task);
}

(async () => {
  const account=await requirePageRole('USER'); if (!account) return;
  CityFlowCustom.initialize(account.id);
  const draftKey=`cityflow:task-draft:${account.id}`;let restoringDraft=false;
  try {const saved=JSON.parse(sessionStorage.getItem(draftKey)||'null');if(saved?.draft&&saved.fields){pendingCreateKey=saved.pendingCreateKey||null;pendingCreateBody=saved.pendingCreateBody||'';restoringDraft=true;for(const key of ['title','type','description','quoted_amount','contact_name','contact_phone','handover_note'])createTaskForm.elements[key].value=saved.fields[key]||'';CityFlowTaskCompose.setDraft(saved.draft);restoringDraft=false;showNotice('已恢复上次未提交的任务。','success')}}catch{restoringDraft=false}
  function saveDraft(force=false){if(restoringDraft||editingTaskID||CityFlowCustom.isEditing()||(submitting&&!force))return;try{sessionStorage.setItem(draftKey,JSON.stringify({fields:Object.fromEntries(new FormData(createTaskForm).entries()),draft:CityFlowTaskCompose.draft(),pendingCreateKey,pendingCreateBody}))}catch{}}
  for(const eventName of ['input','change','cityflow-draft-change'])createTaskForm.addEventListener(eventName,()=>saveDraft());

  const profileDialog = document.querySelector('#profileDialog');
  const taskDialog = document.querySelector('#taskDialog');
  const paymentDialog = document.querySelector('#paymentDialog');
  document.querySelector('#refresh').addEventListener('click', loadTasks);
  document.querySelector('#taskPrevious').addEventListener('click',()=>{taskOffset=Math.max(0,taskOffset-50);loadTasks()});
  document.querySelector('#taskNext').addEventListener('click',()=>{taskOffset+=50;loadTasks()});
  document.querySelector('#editProfile').addEventListener('click', async () => { await loadProfile(); profileDialog.showModal(); });
  document.querySelectorAll('[data-close-dialog]').forEach(button => button.addEventListener('click', () => profileDialog.close()));
  document.querySelectorAll('[data-close-payment]').forEach(button => button.addEventListener('click', () => paymentDialog.close()));
  document.querySelector('#paymentCoupon').addEventListener('change', syncPaymentQuote);
  void refreshCouponBadge();
  document.querySelector('#paymentForm').addEventListener('submit', async event => {
    event.preventDefault();
    if (!paymentTask) return;
    const button = document.querySelector('#paymentGenerate');
    const status = document.querySelector('#paymentStatus');
    const provider = event.currentTarget.elements.provider.value;
    const userCouponID = document.querySelector('#paymentCoupon').value;
    button.disabled = true; status.style.color = ''; status.textContent = '正在准备支付…';
    try {
      const result = await api(`/api/v1/tasks/${encodeURIComponent(paymentTask.id)}/payment`, {
        method: 'POST',
        body: JSON.stringify({ provider, user_coupon_id: userCouponID, idempotency_key: crypto.randomUUID() }),
      });
      renderPayment(result);
      if (result?.already_paid) await loadTasks();
    } catch (error) { status.style.color = '#a04b3e'; status.textContent = '支付请求失败，请稍后重试。'; }
    finally { button.disabled = false; }
  });
  function restoreEdit() { if(!editSnapshot)return; const snapshot=editSnapshot;editSnapshot=null;editingTaskID='';snapshot.parent.insertBefore(createTaskForm,snapshot.next);for(const key of ['title','type','description','quoted_amount','contact_name','contact_phone','handover_note'])createTaskForm.elements[key].value=snapshot.fields[key] || '';CityFlowTaskCompose.setDraft(snapshot.draft);createTaskForm.querySelector('[type=submit]').textContent='发布任务'; }
  document.querySelectorAll('[data-close-task]').forEach(button => button.addEventListener('click', () => taskDialog.close()));taskDialog.addEventListener('close',restoreEdit);
  createTaskForm.addEventListener('submit', async event => {
    event.preventDefault();
    if(CityFlowCustom.isEditing()) { CityFlowCustom.save(); return; }
    if(submitting)return;
    const data = new FormData(createTaskForm), places = CityFlowTaskCompose.value();
    if (!places) return;
    const original = ownTasks.find(task=>task.id===editingTaskID);
    const body = { title:data.get('title'), type:data.get('type'), ...places, description:data.get('description'), contact_name:data.get('contact_name'), contact_phone:data.get('contact_phone'), handover_note:data.get('handover_note'), priority:original?.priority || 0 };
    const amountYuan = Number(data.get('quoted_amount'));
    if (Number.isFinite(amountYuan) && amountYuan > 0) body.quoted_amount_fen = Math.round(amountYuan * 100);
    const button=createTaskForm.querySelector('[type=submit]'), label=button.textContent;submitting=true;button.disabled=true;button.textContent='保存中…';
    try {
      if(editingTaskID){await api(`/api/v1/tasks/${encodeURIComponent(editingTaskID)}`,{method:'PUT',body:JSON.stringify(body)});taskDialog.close();showNotice('任务已更新。','success');}
      else {const encoded=JSON.stringify(body);if(encoded!==pendingCreateBody){pendingCreateKey=crypto.randomUUID();pendingCreateBody=encoded}saveDraft(true);await api('/api/v1/tasks',{method:'POST',headers:{'Idempotency-Key':pendingCreateKey},body:encoded});pendingCreateKey=null;pendingCreateBody='';createTaskForm.reset();try{sessionStorage.removeItem(draftKey)}catch{};showNotice('任务已发布。','success');}
      await loadTasks();(await window.CityFlowMapReady)?.refresh();
    } catch(error) { document.querySelector('#composeNotice').textContent=error.message; }
    finally {submitting=false;button.disabled=false;button.textContent=editingTaskID ? label : '发布任务';}
  });
  document.querySelector('#userAvatarFile')?.addEventListener('change', async event => {
    const file = event.target.files?.[0];
    if (!file) return;
    const status = document.querySelector('#profileStatus');
    status.textContent = '正在上传头像…';
    try {
      const data = new FormData();
      data.append('file', file);
      const token = getToken();
      const response = await fetch('/api/v1/uploads', { method: 'POST', headers: token ? { Authorization: `Bearer ${token}` } : {}, body: data });
      const result = await response.json().catch(() => ({}));
      if (!response.ok) throw new Error(friendlyError(result.error, response.status));
      document.querySelector('#profileForm').elements.avatar_url.value = result.url;
      status.textContent = '头像已上传。';
    } catch (error) {
      status.textContent = error.message;
    }
  });
  document.querySelector('#profileForm').addEventListener('submit', async event => {
    event.preventDefault();
    const form = event.currentTarget;
    const status = document.querySelector('#profileStatus');
    status.textContent = '';
    form.elements.email.removeAttribute('aria-invalid');
    form.elements.phone.removeAttribute('aria-invalid');
    if (!form.checkValidity()) {
      const emailInvalid = !form.elements.email.checkValidity();
      if (emailInvalid) form.elements.email.setAttribute('aria-invalid', 'true');
      if (!form.elements.phone.checkValidity()) form.elements.phone.setAttribute('aria-invalid', 'true');
      status.textContent = emailInvalid ? '请输入有效邮箱，例如 name@example.com。' : '手机号应为 11 位中国大陆手机号。';
      (emailInvalid ? form.elements.email : form.elements.phone).focus();
      return;
    }
    const body = {
      name: form.elements.name.value.trim(),
      email: form.elements.email.value.trim(),
      phone: form.elements.phone.value.trim(),
      avatar_url: form.elements.avatar_url.value.trim(),
    };
    try {
      await api('/api/v1/users/me', { method: 'PUT', body: JSON.stringify(body) });
      profileDialog.close();
      showNotice('账号资料已保存。', 'success');
      await loadProfile();
    } catch (error) {
      status.textContent = error.message || '账号资料保存失败，请稍后重试。';
      if (String(error.message).includes('email')) form.elements.email.setAttribute('aria-invalid', 'true');
    }
  });
  document.querySelector('#taskList').addEventListener('click', event => {
    const pay = event.target.closest('[data-pay-task]');
    if(pay){openPayment(pay.dataset.payTask);return;}
    const view = event.target.closest('[data-view-tracking]');
    if(view){window.CityFlowOpenTracking(view.dataset.viewTracking);return;}
    const edit = event.target.closest('[data-edit-task]');
    if (edit) { openTaskEditor(edit.dataset.editTask); return; }
    const cancel = event.target.closest('[data-cancel-task]');
    if (cancel) cancelTask(cancel.dataset.cancelTask,cancel);
  });
  await Promise.all([loadProfile(), loadTasks()]);
  setInterval(()=>{if(!document.hidden&&!editingTaskID&&!document.activeElement?.closest('#taskList'))loadTasks()},15000);
})();

