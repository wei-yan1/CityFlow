(async () => {
  const user = await requirePageRole('USER');
  if (!user) return;
  const $ = selector => document.querySelector(selector);
  const notice = (text, kind = 'error') => {
    const el = $('#taskNotice');
    el.textContent = text || '';
    el.classList.toggle('success', kind === 'success');
  };
  const taskID = decodeURIComponent(location.pathname.split('/').filter(Boolean).pop() || '');
  if (!taskID) { notice('任务编号缺失。'); return; }
  $('#taskWelcome').textContent = user.name || '';
  let task = null;
  let riderView = null;
  let mapReady = null;
  let merchantTarget = null;
  const merchantBlock = $('#merchantReviewBlock');
  const feedbackGrid = $('#feedbackGrid');
  function syncFeedbackLayout() {
    feedbackGrid.classList.toggle('two-up', merchantBlock.hidden);
  }
  syncFeedbackLayout();

  const statusLabel = value => statusLabels[value] || value || '—';
  const statusClass = value => ({ OPEN: 'open', COMPLETED: 'completed', CANCELLED: 'cancelled' })[value] || 'claimed';
  const money = fen => `¥${((Number(fen) || 0) / 100).toFixed(2)}`;
  const time = value => value ? new Date(value).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) : '—';
  const reviewSentence = text => {
    const value = String(text || '').trim();
    return value ? `${value.replace(/[。！？!?.]+$/u, '')}。` : '未填写评价内容。';
  };
  const skillLabels = { DELIVERY: '配送', PICKUP: '代取', PURCHASE: '代购', ERRAND: '跑腿', OTHER: '其他' };

  function renderTask() {
    $('#taskTitle').textContent = task.title || '任务详情';
    $('#taskStatus').textContent = statusLabel(task.status);
    $('#taskStatus').className = `task-status ${statusClass(task.status)}`;
    $('#taskPrice').textContent = money(task.quoted_amount_fen);
    const stops = (task.pickup_stops?.length ? task.pickup_stops : [{ address: task.pickup }]).map((stop, index) => `取件${task.pickup_stops?.length > 1 ? index + 1 : ''}：${stop.address || '未填写'}`).join(' · ');
    $('#taskRoute').textContent = `${stops}  →  送达：${task.destination || '未填写'}`;
    $('#taskMeta').innerHTML = [
      `<span>类型：${esc(({ DELIVERY: '配送', PICKUP: '代取', PURCHASE: '代购', OTHER: '其他' })[task.type] || task.type || '任务')}</span>`,
      `<span>发布：${esc(time(task.created_at))}</span>`,
    ].join('');
    renderContact();
    renderTimeline();
  }

  function renderContact() {
    const host = $('#contactContent');
    const name = task.contact_name || '', phone = task.contact_phone || '', note = task.handover_note || '';
    if (!name && !phone && !note) {
      host.innerHTML = '<p class="muted">未填写交接信息。</p>';
      return;
    }
    host.innerHTML = `
      ${name ? `<p><span>联系人</span><strong>${esc(name)}</strong></p>` : ''}
      ${phone ? `<p><span>联系电话</span><a href="tel:${esc(phone)}">${esc(phone)}</a></p>` : ''}
      ${note ? `<p class="contact-note"><span>交接要求</span>${esc(note)}</p>` : ''}`;
  }

  function renderTimeline() {
    const events = Array.isArray(task.events) ? task.events.filter(event => event?.to) : [];
    const incidents = Array.isArray(task.incidents) ? task.incidents : [];
    const list = $('#timeline');
    if (!events.length && !incidents.length) {
      list.innerHTML = `<li class="timeline-item current"><strong>${esc(statusLabel(task.status))}</strong><time>${esc(time(task.updated_at || task.created_at))}</time></li>`;
      return;
    }
    const labels = { CONTACT_FAILED: '联系不到人', PICKUP_FAILED: '取件失败', ADDRESS_WRONG: '地址有误', RIDER_CANCEL: '骑手申请取消', USER_CANCEL: '用户申请取消', PLATFORM_HELP: '请求平台协助', OTHER: '其他异常' };
    const rows = events.map((event, index) => {
      const current = index === events.length - 1 && !incidents.length;
      const label = index === 0 && event.to === 'OPEN' ? '任务已发布' : index === 0 ? `任务已${statusLabel(event.to)}` : statusLabel(event.to);
      return `<li class="timeline-item${current ? ' current' : ''}"><strong>${esc(label)}</strong><time>${esc(time(event.created_at))}</time></li>`;
    });
    for (const incident of incidents) {
      const resolved = incident.status === 'RESOLVED';
      rows.push(`<li class="timeline-item incident${resolved ? '' : ' current'}"><strong>${resolved ? '异常已处理' : '异常待平台处理'} · ${esc(labels[incident.kind] || incident.kind)}</strong><p>${esc(incident.note || '未填写说明')}</p>${incident.resolution ? `<p class="muted">处理结果：${esc(incident.resolution)}</p>` : ''}<time>${esc(time(incident.created_at))}</time></li>`);
    }
    list.innerHTML = rows.join('');
  }

  function riderAvatar(rider) {
    const image = rider.avatar_url ? `<img src="${esc(rider.avatar_url)}" alt="${esc(rider.display_name)}的头像" loading="lazy">` : esc((rider.display_name || '骑').slice(0, 1));
    return `<a class="rider-avatar" href="/riders/${encodeURIComponent(rider.id)}?task_id=${encodeURIComponent(task.id)}" aria-label="查看 ${esc(rider.display_name)} 的公开主页">${image}</a>`;
  }

  function renderRider() {
    const content = $('#riderContent');
    if (!task.worker_id) {
      $('#riderLive').textContent = '等待接单';
      $('#riderLive').className = 'live-pill waiting';
      content.innerHTML = '<p class="rider-empty">等待骑手接单。</p>';
      setFeedbackAvailability();
      return;
    }
    if (!riderView) {
      content.innerHTML = '<p class="rider-empty">正在加载骑手资料…</p>';
      return;
    }
    const rider = riderView.rider;
    $('#riderLive').textContent = task.status === 'COMPLETED' ? '已完成' : task.status === 'CANCELLED' ? '已取消' : '履约中';
    $('#riderLive').className = `live-pill ${task.status === 'CANCELLED' ? 'offline' : ''}`;
    const latestReview = rider.reviews?.[0];
    const reviewCount = Number(rider.review_count) || 0;
    const tipTotal = money(rider.tip_total_fen);
    const skills = (Array.isArray(rider.skills) ? rider.skills : []).map(skill => skillLabels[skill] || skill).filter(Boolean).join(' · ');
    content.innerHTML = `
      <div class="rider-identity">${riderAvatar(rider)}<div><h3>${esc(rider.display_name || '骑手')}</h3><p>${esc(({ WALK: '步行', BICYCLE: '自行车', E_BIKE: '电动车', CAR: '汽车', OTHER: '其他' })[rider.vehicle_type] || rider.vehicle_type || '骑手')} · ${esc(rider.service_area || '桂林市')}</p></div><div class="rider-rating"><strong>★ ${(Number(rider.rating) || 0).toFixed(1)}</strong>${reviewCount} 条评价</div></div>
      <p class="rider-bio">${esc(skills || '提供即时取送与配送服务')}</p>
      <div class="rider-stats"><span>已完成<strong>${Number(rider.completed_tasks) || 0}</strong></span><span>评价<strong>${reviewCount}</strong></span><span>收到打赏<strong>${esc(tipTotal)}</strong></span></div>
      ${latestReview ? `<p class="rider-bio">最近评价：${'★'.repeat(Math.max(1, Number(latestReview.rating) || 1))} ${esc(reviewSentence(latestReview.comment))}</p>` : ''}
      <div class="rider-actions"><button id="callRider" class="button primary" type="button" ${riderView.can_call ? '' : 'disabled'}>呼叫骑手</button><a class="button outline" href="/riders/${encodeURIComponent(rider.id)}?task_id=${encodeURIComponent(task.id)}">查看骑手主页</a></div>
      <p id="callStatus" class="form-status" role="status"></p>`;
    content.querySelector('#callRider')?.addEventListener('click', callRider);
    setFeedbackAvailability();
  }

  function setFeedbackAvailability() {
    const canReview = Boolean(riderView?.can_review);
    const canTip = Boolean(riderView?.can_tip);
    const canReport = Boolean(riderView?.can_report);
    const reviewForm = $('#reviewForm');
    for (const element of reviewForm.querySelectorAll('select, textarea, button')) element.disabled = !canReview;
    $('#reviewStatus').textContent = canReview ? '' : '任务完成后可评价。';
    if (riderView?.my_review) {
      $('#reviewStatus').textContent = `已提交 ${riderView.my_review.rating} 星评价：${reviewSentence(riderView.my_review.comment)}`;
    }
    const tipForm = $('#tipForm');
    for (const element of tipForm.querySelectorAll('input, button')) element.disabled = !canTip;
    for (const button of document.querySelectorAll('[data-tip-fen]')) button.disabled = !canTip;
    $('#reportOpen').disabled = !canReport;
  }

  function setMerchantReviewAvailability(canReview, hint) {
    const form = $('#merchantReviewForm');
    for (const element of form.querySelectorAll('select, textarea, button')) element.disabled = !canReview;
    $('#merchantReviewStatus').textContent = hint;
  }

  async function loadMerchantTarget() {
    if (task.status !== 'COMPLETED') {
      merchantBlock.hidden = true;
      syncFeedbackLayout();
      setMerchantReviewAvailability(false, '任务完成后可评价商家。');
      return;
    }
    setMerchantReviewAvailability(false, '正在加载商家信息…');
    try {
      merchantTarget = await api(`/api/v1/tasks/${encodeURIComponent(task.id)}/merchant-review-target`);
      if (merchantTarget.source !== 'merchant') {
        merchantTarget = null;
        merchantBlock.hidden = true;
        syncFeedbackLayout();
        return;
      }
      merchantBlock.hidden = false;
      syncFeedbackLayout();
      if (merchantTarget.reviewed) {
        setMerchantReviewAvailability(false, '商家评价已提交。');
        $('#merchantReviewStatus').textContent = '商家评价已提交。';
        return;
      }
      setMerchantReviewAvailability(true, `本次商家：${merchantTarget.name}。`);
    } catch (error) {
      merchantBlock.hidden = true;
      syncFeedbackLayout();
    }
  }

  $('#merchantReviewForm').addEventListener('submit', async event => {
    event.preventDefault();
    if (!merchantTarget) return;
    const form = event.currentTarget;
    const button = form.querySelector('button[type="submit"]');
    const body = new FormData(form);
    button.disabled = true;
    $('#merchantReviewStatus').style.color = '';
    $('#merchantReviewStatus').textContent = '正在提交商家评价…';
    try {
      await api(`/api/v1/tasks/${encodeURIComponent(task.id)}/merchant-reviews`, { method: 'POST', body: JSON.stringify({ rating: Number(body.get('rating')), comment: String(body.get('comment') || ''), anonymous: body.get('anonymous') === 'on' }) });
      $('#merchantReviewStatus').style.color = '#267151';
      form.reset();
      setMerchantReviewAvailability(false, '商家评价已提交。');
    } catch (error) {
      $('#merchantReviewStatus').style.color = '#a04b3e';
      $('#merchantReviewStatus').textContent = '评价提交失败，请稍后重试。';
    } finally { button.disabled = false; }
  });

  function renderFeedbackNotice(text, kind = '') {
    $('#reviewStatus').textContent = text;
    if (kind === 'error') $('#reviewStatus').style.color = '#a04b3e';
  }

  async function callRider() {
    const status = $('#callStatus');
    if (!status) return;
    status.style.color = '';
    status.textContent = '正在呼叫骑手…';
    try {
      const result = await api(`/api/v1/tasks/${encodeURIComponent(task.id)}/call-rider`, { method: 'POST' });
      status.textContent = `已发送呼叫请求（${time(result.created_at)}）。`;
			status.style.color = '#267151';
    } catch (error) {
      status.textContent = '呼叫请求发送失败，请稍后重试。';
      status.style.color = '#a04b3e';
    }
  }

  async function loadPayment() {
    const host = $('#paymentContent');
    try {
      const summary = await api(`/api/v1/tasks/${encodeURIComponent(task.id)}/payment`);
      const payment = summary.payment, settlement = summary.settlement;
      if (!payment) {
        host.innerHTML = '<p class="muted">暂无支付记录。</p>';
        return;
      }
      const statusText = ({ PENDING: '等待支付', PAID: '已支付', REFUND_PENDING: '退款处理中', REFUND_FAILED: '退款失败', REFUNDED: '已退款', CLOSED: '支付已关闭', FAILED: '支付失败' })[payment.status] || '未知';
      const settlementText = settlement ? ({ PENDING: '待结算给骑手', PAID: '已结算给骑手', PROCESSING: '结算处理中', UNKNOWN: '结算状态待核对', FAILED: '结算失败' })[settlement.status] || '未知' : '任务完成后生成';
      const original = Number(payment.original_amount_fen || task.quoted_amount_fen || payment.amount_fen || 0);
      const discount = Number(payment.discount_fen || 0);
      host.innerHTML = `
        <div class="payment-summary-row"><span>任务原价</span><strong>${esc(money(original))}</strong></div>
        <div class="payment-summary-row payment-row-discount"><span>平台优惠${payment.coupon_title ? ` · ${esc(payment.coupon_title)}` : ''}</span><strong>${discount > 0 ? `-${esc(money(discount))}` : '无'}</strong></div>
        <div class="payment-summary-row payment-row-total"><span>实付金额</span><strong>${esc(money(payment.amount_fen))}</strong></div>
        <div class="payment-summary-row"><span>支付渠道</span><span>${esc(({ MOCK: '平台支付', ALIPAY: '支付宝', WECHAT: '微信支付' })[payment.provider] || payment.provider)}</span></div>
        <div class="payment-summary-row"><span>支付状态</span><span class="payment-state">${esc(statusText)}</span></div>
        <div class="payment-summary-row"><span>骑手结算</span><span>${esc(settlementText)}${settlement ? ` · ${esc(money(settlement.amount_fen))}` : ''}</span></div>
        ${payment.pay_url ? `<div class="payment-action"><a class="button primary" href="${esc(payment.pay_url)}" target="_blank" rel="noopener noreferrer">继续支付</a></div>` : ''}`;
    } catch (error) { host.innerHTML = '<p class="muted">支付状态加载失败，请稍后重试。</p>'; }
  }

  async function loadRider() {
    if (!task?.worker_id) { renderRider(); return; }
    try {
      riderView = await api(`/api/v1/tasks/${encodeURIComponent(task.id)}/rider`);
      renderRider();
    } catch (error) {
      $('#riderContent').innerHTML = '<p class="rider-empty">骑手资料加载失败。</p>';
      setFeedbackAvailability();
    }
  }

  $('#reviewForm').addEventListener('submit', async event => {
    event.preventDefault();
    const form = event.currentTarget;
    const button = form.querySelector('button[type="submit"]');
    const body = new FormData(form);
    button.disabled = true;
    renderFeedbackNotice('正在提交评价…');
    try {
      await api(`/api/v1/tasks/${encodeURIComponent(task.id)}/rider/reviews`, { method: 'POST', body: JSON.stringify({ rating: Number(body.get('rating')), comment: String(body.get('comment') || '') }) });
      renderFeedbackNotice('评价已提交。', 'success');
      await loadRider();
    } catch (error) { renderFeedbackNotice('评价提交失败，请稍后重试。', 'error'); }
    finally { button.disabled = false; }
  });

  $('#tipForm').addEventListener('submit', async event => {
    event.preventDefault();
    const form = event.currentTarget;
    const button = form.querySelector('button[type="submit"]');
    const amountYuan = Number(form.elements.amount.value);
    if (!Number.isFinite(amountYuan) || amountYuan <= 0) { $('#tipStatus').textContent = '请输入有效的打赏金额。'; return; }
    button.disabled = true;
    $('#tipStatus').style.color = '';
    $('#tipStatus').textContent = '正在记录打赏…';
    try {
      const result = await api(`/api/v1/tasks/${encodeURIComponent(task.id)}/rider/tip`, { method: 'POST', body: JSON.stringify({ amount_fen: Math.round(amountYuan * 100), message: String(form.elements.message.value || '') }) });
      $('#tipStatus').style.color = '#267151';
      $('#tipStatus').textContent = `已记录 ${money(result.amount_fen)} 打赏。`;
      form.reset();
      document.querySelectorAll('[data-tip-fen]').forEach(item => item.setAttribute('aria-pressed', 'false'));
      await loadRider();
    } catch (error) { $('#tipStatus').style.color = '#a04b3e'; $('#tipStatus').textContent = '打赏提交失败，请稍后重试。'; }
    finally { button.disabled = false; }
  });

  document.querySelectorAll('[data-tip-fen]').forEach(button => button.addEventListener('click', () => {
    $('#tipForm').elements.amount.value = (Number(button.dataset.tipFen) / 100).toFixed(2);
    document.querySelectorAll('[data-tip-fen]').forEach(item => item.setAttribute('aria-pressed', String(item === button)));
  }));

  $('#reportOpen').addEventListener('click', () => $('#reportDialog').showModal());
  document.querySelectorAll('[data-close-report]').forEach(button => button.addEventListener('click', () => $('#reportDialog').close()));
  $('#reportImages').addEventListener('change', async event => {
    const files = [...(event.currentTarget.files || [])].slice(0, 6);
    const preview = $('#reportPreview');
    preview.replaceChildren();
    for (const file of files) {
      if (!file.type.startsWith('image/')) continue;
      const data = await readFile(file);
      const image = document.createElement('img'); image.src = data; image.alt = file.name; image.title = `${file.name} · ${(file.size / 1024).toFixed(0)}KB`;
      preview.append(image);
    }
    if (!preview.children.length) preview.innerHTML = '<span class="muted">暂无图片。</span>';
  });
  $('#reportForm').addEventListener('submit', async event => {
    event.preventDefault();
    const form = event.currentTarget;
    const button = form.querySelector('button[type="submit"]');
    const status = $('#reportStatus');
    const files = [...($('#reportImages').files || [])].slice(0, 6);
    button.disabled = true; status.style.color = ''; status.textContent = '正在提交举报…';
    try {
      const images = [];
      for (const file of files) {
        if (!file.type.startsWith('image/')) continue;
        if (file.size > 3 * 1024 * 1024) throw new Error(`图片 ${file.name} 超过 3MB。`);
        images.push(await readFile(file));
      }
      const formData = new FormData(form);
      await api(`/api/v1/tasks/${encodeURIComponent(task.id)}/rider/reports`, { method: 'POST', body: JSON.stringify({ reason: String(formData.get('reason') || ''), details: String(formData.get('details') || ''), images }) });
      status.style.color = '#267151'; status.textContent = '举报已提交，平台会记录并处理。';
      setTimeout(() => $('#reportDialog').close(), 900);
    } catch (error) { status.style.color = '#a04b3e'; status.textContent = '举报提交失败，请稍后重试。'; }
    finally { button.disabled = false; }
  });

  function readFile(file) {
    return new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve(String(reader.result || ''));
      reader.onerror = () => reject(new Error(`无法读取图片 ${file.name}`));
      reader.readAsDataURL(file);
    });
  }

  try {
    task = await api(`/api/v1/tasks/${encodeURIComponent(taskID)}`);
    renderTask();
    notice('');
    loadRider();
    loadPayment();
    loadMerchantTarget();
  } catch (error) {
    notice('任务加载失败，请稍后重试。');
    return;
  }
  try {
    mapReady = await window.CityFlowMapReady;
    if (mapReady) {
      mapReady.selectTask(task.id, task);
      let attempts = 0;
      const planTimer = setInterval(() => {
        attempts++;
        mapReady.planRoute();
        if (attempts >= 12) clearInterval(planTimer);
      }, 2500);
    }
  } catch (error) { notice('地图加载失败，请稍后重试。'); }
})();



