const nextStatus = { CLAIMED: 'ARRIVED', ARRIVED: 'PROCESSING', PROCESSING: 'COMPLETED' };
const riderStatus = { AVAILABLE: '可接单', OFFLINE: '离线', BUSY: '执行中', SUSPENDED: '已停用' };
const activeOrder = { PROCESSING: 0, ARRIVED: 1, CLAIMED: 2 };
let riderProfile = null;
let openOffset = 0, activeOffset = 0, workspaceGeneration = 0;
let displayedOpenOffset = 0, displayedActiveOffset = 0;
let openTaskItems = [], activeTaskItems = [];
let riderPosition = null, sortMode = 'pickup', locating = false;
let settlements = [], payoutAccount = null;

async function loadRiderWorkspace() {
  const generation = ++workspaceGeneration;
  try {
    const locationQuery = riderPosition ? `&lat=${encodeURIComponent(riderPosition.latitude)}&lng=${encodeURIComponent(riderPosition.longitude)}` : '';
    const [profile, openResult, claimed, arrived, processing] = await Promise.all([
      api('/api/v1/riders/me'),
      api(`/api/v1/tasks?status=OPEN&limit=50&offset=${openOffset}${locationQuery}`),
      api(`/api/v1/tasks?status=CLAIMED&limit=50&offset=${activeOffset}`),
      api(`/api/v1/tasks?status=ARRIVED&limit=50&offset=${activeOffset}`),
      api(`/api/v1/tasks?status=PROCESSING&limit=50&offset=${activeOffset}`),
    ]);
    if (generation !== workspaceGeneration) return false;
    displayedOpenOffset = openOffset;
    displayedActiveOffset = activeOffset;
    riderProfile = profile;
    renderRiderProfile();

    openTaskItems = openResult.data || [];
    activeTaskItems = [...claimed.data, ...arrived.data, ...processing.data]
      .filter(item => item.worker_id)
      .sort((a, b) => (activeOrder[a.status] ?? 99) - (activeOrder[b.status] ?? 99) || createdAt(b) - createdAt(a));

    document.querySelector('#openPrevious').disabled = openOffset === 0;
    document.querySelector('#openNext').disabled = openTaskItems.length < 50;
    document.querySelector('#activePrevious').disabled = activeOffset === 0;
    document.querySelector('#activeNext').disabled = ![claimed, arrived, processing].some(page => page.data.length === 50);
    renderOpenTasks();
    renderActiveTasks();
    return true;
  } catch (error) {
    if (generation === workspaceGeneration) {
      openOffset = displayedOpenOffset;
      activeOffset = displayedActiveOffset;
      showNotice('任务列表加载失败，请稍后重试。');
    }
    return false;
  }
}

function createdAt(task) {
  return Date.parse(task.created_at || '') || 0;
}

function normalizePoint(point) {
  if (!point || !Number.isFinite(Number(point.latitude)) || !Number.isFinite(Number(point.longitude))) return null;
  return { latitude: Number(point.latitude), longitude: Number(point.longitude) };
}

function pickupPoint(task) {
  const stops = task.pickup_stops?.length ? task.pickup_stops : [{ address: task.pickup, location: task.pickup_location }];
  return stops.map(stop => normalizePoint(stop.location)).find(Boolean) || normalizePoint(task.pickup_location);
}

function deliveryPoint(task) {
  return normalizePoint(task.destination_location);
}

function distanceMeters(from, to) {
  if (!from || !to) return null;
  const earthRadius = 6371000;
  const radians = value => value * Math.PI / 180;
  const dLat = radians(to.latitude - from.latitude);
  const dLng = radians(to.longitude - from.longitude);
  const lat1 = radians(from.latitude), lat2 = radians(to.latitude);
  const a = Math.sin(dLat / 2) ** 2 + Math.cos(lat1) * Math.cos(lat2) * Math.sin(dLng / 2) ** 2;
  return earthRadius * 2 * Math.atan2(Math.sqrt(a), Math.sqrt(1 - a));
}

function distanceFor(task, mode) {
  if (!riderPosition) return null;
  const point = mode === 'delivery' ? deliveryPoint(task) : pickupPoint(task);
  return distanceMeters(riderPosition, point);
}

function formatDistance(meters) {
  if (!Number.isFinite(meters)) return '';
  if (meters < 1000) return `${Math.max(10, Math.round(meters / 10) * 10)} 米`;
  if (meters < 10000) return `${(meters / 1000).toFixed(1)} 公里`;
  return `${Math.round(meters / 1000)} 公里`;
}

function sortedOpenTasks() {
  const list = [...openTaskItems];
  if (!riderPosition) return list.sort((a, b) => createdAt(b) - createdAt(a));
  return list.sort((a, b) => {
    const da = distanceFor(a, sortMode), db = distanceFor(b, sortMode);
    if (da === null && db === null) return createdAt(b) - createdAt(a);
    if (da === null) return 1;
    if (db === null) return -1;
    return da - db || createdAt(b) - createdAt(a);
  });
}

function typeLabel(type) {
  return ({ DELIVERY: '配送', PICKUP: '代取', PURCHASE: '代购', OTHER: '其他' })[type] || type || '任务';
}

const skillAliases = { '配送': 'DELIVERY', '代取': 'PICKUP', '代购': 'PURCHASE', '其他': 'OTHER' };
function skillsText(skills) {
  return (skills || []).map(value => typeLabel(value)).join(', ');
}
function parseSkills(text) {
  return String(text || '').split(/[,，、]/).map(value => value.trim()).filter(Boolean).map(value => skillAliases[value] || value.toUpperCase());
}

function orderPrice(task) {
  const fen = Number(task?.quoted_amount_fen);
  if (!Number.isFinite(fen) || fen <= 0) return '';
  return `¥${(fen / 100).toFixed(2)}`;
}

function fenMoney(fen) {
  const value = Number(fen);
  return Number.isFinite(value) && value > 0 ? `¥${(value / 100).toFixed(2)}` : '¥0.00';
}

function shortTime(value) {
  if (!value) return '—';
  return new Date(value).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
}

async function loadWallet() {
  const host = document.querySelector('#walletSummary');
  const list = document.querySelector('#settlementList');
  try {
    const [result, account] = await Promise.all([
      api('/api/v1/riders/me/settlements?limit=20&offset=0'),
      api('/api/v1/riders/me/payout-account'),
    ]);
    settlements = result.data || [];
    payoutAccount = account || null;
    renderWallet();
  } catch (error) {
    if (host) host.innerHTML = '<div class="empty-state">结算记录加载失败。</div>';
    if (list) list.innerHTML = '';
  }
}

function renderWallet() {
  const host = document.querySelector('#walletSummary');
  const list = document.querySelector('#settlementList');
  if (!host || !list) return;
  const pending = settlements.filter(item => ['PENDING', 'PROCESSING'].includes(item.status)).reduce((sum, item) => sum + Number(item.amount_fen || 0), 0);
  const paid = settlements.filter(item => item.status === 'PAID').reduce((sum, item) => sum + Number(item.amount_fen || 0), 0);
  const accountReady = Boolean(payoutAccount?.account && payoutAccount?.provider);
  const providerLabel = ({ ALIPAY: '支付宝', WECHAT: '微信支付', MOCK: '平台结算' })[payoutAccount?.provider] || payoutAccount?.provider || '';
  host.innerHTML = `
    <div class="rider-wallet-stat"><small>待结算</small><b>${esc(fenMoney(pending))}</b></div>
    <div class="rider-wallet-stat"><small>已到账</small><b>${esc(fenMoney(paid))}</b></div>
    <div class="rider-wallet-stat rider-wallet-account"><small>收款账户</small><b>${accountReady ? `${esc(providerLabel)} · 已设置` : '未设置'}</b></div>`;
  document.querySelector('#openPayout').textContent = accountReady ? '更新收款账户' : '设置收款账户';
  const labels = { PENDING: '待结算', PROCESSING: '结算中', PAID: '已到账', FAILED: '结算失败', UNKNOWN: '状态待核对', CANCELLED: '已取消' };
  list.innerHTML = settlements.length ? settlements.slice(0, 6).map(item => `<div class="rider-settlement-row"><div><strong>任务结算</strong><span>${esc(labels[item.status] || item.status)} · ${esc(shortTime(item.created_at))}</span></div><b>${esc(fenMoney(item.amount_fen))}</b></div>`).join('') : '<div class="empty-state">暂无结算记录。</div>';
}

function openPayoutEditor() {
  const dialog = document.querySelector('#payoutDialog');
  const form = document.querySelector('#payoutForm');
  form.reset();
  if (payoutAccount?.provider) form.elements.provider.value = payoutAccount.provider;
  document.querySelector('#payoutNotice').textContent = payoutAccount?.account ? '已保存收款账户。' : '';
  dialog.showModal();
}

function stopStatusLabel(status) {
  return ({ ARRIVED: '已到达，待取件', PICKED_UP: '已取件' })[status] || '待取件';
}

function riderTaskCard(task, actions = '', options = {}) {
  const stops = task.pickup_stops?.length
    ? task.pickup_stops
    : [{ address: task.pickup || '取件地址待补充', status: 'PENDING', location: task.pickup_location }];
  const pickupDistance = distanceFor(task, 'pickup');
  const deliveryDistance = distanceFor(task, 'delivery');
  const price = orderPrice(task);
  const contact = [task.contact_name, task.contact_phone].filter(Boolean).join(' · ');
  const openIncidents = (task.incidents || []).filter(item => item.status !== 'RESOLVED');
  const incidentMarkup = openIncidents.length
    ? `<div class="rider-incident-banner"><strong>${openIncidents.length} 条异常待平台处理</strong><span>${esc(openIncidents[0].note || '')}</span></div>`
    : '';
  const handoverMarkup = contact || task.handover_note
    ? `<div class="rider-handover">${contact ? `<p><span>交接联系人</span>${esc(contact)}</p>` : ''}${task.handover_note ? `<p><span>交接要求</span>${esc(task.handover_note)}</p>` : ''}</div>`
    : '';
  const badgeMarkup = `
    <div class="rider-task-badges">
      ${price ? `<span class="rider-price"><small>任务价格</small>${esc(price)}</span>` : ''}
      ${options.recommended ? '<span class="rider-recommend">离你最近</span>' : ''}
      ${Number.isFinite(pickupDistance) ? `<span class="rider-distance pickup">距取件 ${esc(formatDistance(pickupDistance))}</span>` : ''}
      ${Number.isFinite(deliveryDistance) ? `<span class="rider-distance delivery">距送达 ${esc(formatDistance(deliveryDistance))}</span>` : ''}
    </div>`;
  const stopMarkup = stops.map((stop, index) => {
    const label = stops.length > 1 ? `取件 ${index + 1}` : '取件点';
    const time = stop.pickup_at ? new Date(stop.pickup_at).toLocaleString('zh-CN') : '立即取件';
    return `<div class="rider-route-point pickup${stop.status === 'PICKED_UP' ? ' picked' : ''}">
      <span class="rider-route-marker" aria-hidden="true">取</span>
      <div><small>${esc(label)} · ${esc(stopStatusLabel(stop.status))}</small><p>${esc(stop.address || task.pickup || '地址待补充')}</p><em>${esc(time)}</em></div>
    </div>`;
  }).join('');
  const destination = `
    <div class="rider-route-point destination">
      <span class="rider-route-marker" aria-hidden="true">送</span>
      <div><small>送达点</small><p>${esc(task.destination || '送达地址待补充')}</p><em>${esc(task.delivery_at ? new Date(task.delivery_at).toLocaleString('zh-CN') : '尽快送达')}</em></div>
    </div>`;
  const next = options.nextHint ? `<div class="rider-next"><b>下一步</b><span>${esc(options.nextHint)}</span></div>` : '';
  return `<article class="task-card rider-task-card${options.active ? ' is-active' : ''}${options.recommended ? ' is-recommended' : ''}" data-task-id="${esc(task.id)}">
    <div class="rider-task-head">
      <div class="rider-task-heading"><span class="task-status ${esc(String(task.status || '').toLowerCase())}">${esc(statusLabels[task.status] || task.status)}</span><h3>${esc(task.title || '未命名任务')}</h3></div>
      ${badgeMarkup}
    </div>
    <div class="rider-route">${stopMarkup}${destination}</div>
    ${task.description ? `<p class="rider-task-description">${esc(task.description)}</p>` : ''}
    ${handoverMarkup}
    ${incidentMarkup}
    <div class="rider-task-foot"><span>${esc(typeLabel(task.type))}</span><span>发布于 ${esc(new Date(task.created_at).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }))}</span></div>
    ${next}
    ${actions ? `<div class="rider-task-actions">${actions}</div>` : ''}
  </article>`;
}

function renderRiderProfile() {
  const profile = riderProfile;
  if (!profile) return;
  const vehicle = ({ WALK: '步行', BICYCLE: '自行车', E_BIKE: '电动车', CAR: '汽车', OTHER: '其他' })[profile.vehicle_type] || '';
  const name = profile.display_name || '骑手';
  const avatar = name.includes('骑手') ? '骑' : name.slice(0, 1);
  const status = riderStatus[profile.status] || '';
  const skills = (profile.skills || []).map(item => typeLabel(item)).join(' · ');
  const detail = [vehicle, profile.service_area, skills].filter(Boolean).join(' · ');
  const concurrency = Number(profile.max_concurrent_tasks) > 0 ? `${Number(profile.max_concurrent_tasks)} 单` : '—';
  const availability = document.querySelector('#toggleAvailability');
  if (availability) {
    const online = profile.status === 'AVAILABLE';
    availability.textContent = online ? '下线休息' : '上线接单';
    availability.classList.toggle('is-online', online);
    availability.disabled = profile.status === 'SUSPENDED';
    availability.title = online ? '下线后不再接收新任务。' : '上线后可接收新任务。';
  }
  document.querySelector('#riderProfile').innerHTML = `
    <div class="rider-profile-main">
      <span class="rider-avatar" aria-hidden="true">${esc(avatar)}</span>
      <div class="rider-profile-copy"><strong>${esc(name)}</strong>${detail ? `<small>${esc(detail)}</small>` : ''}</div>
    </div>
    <div class="rider-profile-stats">
      <span class="rider-profile-stat"><small>评分</small><b>★ ${esc(Number(profile.rating || 0).toFixed(1))}</b></span>
      <span class="rider-profile-stat"><small>累计完成</small><b>${esc(profile.completed_tasks || 0)} 单</b></span>
      <span class="rider-profile-stat"><small>同时接单</small><b>${esc(concurrency)}</b></span>
      ${status ? `<span class="rider-online">${esc(status)}</span>` : ''}
    </div>`;
}

function renderOpenTasks() {
  const list = sortedOpenTasks();
  const recommendedId = riderPosition && list.length > 1 && Number.isFinite(distanceFor(list[0], sortMode)) ? list[0].id : null;
  document.querySelector('#openCount').textContent = `可接任务 ${list.length} 项`;
  document.querySelector('#openTasks').innerHTML = list.length
    ? list.map(item => riderTaskCard(item, `<button class="button primary" data-claim="${esc(item.id)}">接下任务</button>`, { recommended: item.id === recommendedId })).join('')
    : (riderProfile?.status === 'OFFLINE'
      ? `<div class="rider-empty"><span>当前离线，上线并共享位置后可接单。</span><button type="button" class="button primary" data-go-online>上线接单</button></div>`
      : `<div class="rider-empty"><span>暂无可接任务。</span><button type="button" class="button subtle" data-locate>定位后查看</button></div>`);
  document.querySelectorAll('.rider-sort-chip').forEach(chip => {
    const active = chip.dataset.sort === sortMode;
    chip.classList.toggle('is-active', active);
    chip.setAttribute('aria-pressed', String(active));
  });
  updateLocateButton();
  document.querySelector('#openNext').disabled = list.length < 50;
}

function activeNextHint(task) {
  const pending = task.pickup_stops?.length > 1 ? task.pickup_stops.findIndex(stop => stop.status !== 'PICKED_UP') : -1;
  if (pending >= 0) {
    const stop = task.pickup_stops[pending];
    return stop.status === 'ARRIVED' ? `核对第 ${pending + 1} 个取件点的物品并确认取件。` : `前往第 ${pending + 1} 个取件点，到达后点击「到达取件点」。`;
  }
  if (task.status === 'CLAIMED') return '导航到取件点，到达后点击「已到达取件点」。';
  if (task.status === 'ARRIVED') return '核对物品并确认取件，然后前往送达点。';
  if (task.status === 'PROCESSING') return '把物品送到送达点，完成后点击「确认送达」。';
  return '';
}

function statusActionLabel(status) {
  return ({
    ARRIVED: '已到达取件点',
    PROCESSING: '确认取件，开始配送',
    COMPLETED: '确认送达',
  })[status] || (statusLabels[status] || status);
}

function renderActiveTasks() {
  document.querySelector('#activeCount').textContent = `当前 ${activeTaskItems.length} 项`;
  document.querySelector('#activeTasks').innerHTML = activeTaskItems.length
    ? activeTaskItems.map(item => {
      const next = nextStatus[item.status];
      const pending = item.pickup_stops?.length > 1 ? item.pickup_stops.findIndex(stop => stop.status !== 'PICKED_UP') : -1;
      const pickupAction = pending >= 0
        ? `<button type="button" class="button primary" data-pickup-task="${esc(item.id)}" data-index="${pending}" data-pickup-status="${item.pickup_stops[pending].status === 'ARRIVED' ? 'PICKED_UP' : 'ARRIVED'}">${item.pickup_stops[pending].status === 'ARRIVED' ? '确认取件' : '到达取件点'}</button>`
        : '';
      const statusButton = next
        ? `<button type="button" class="button ${pending >= 0 ? 'outline' : 'primary'}" ${next === 'COMPLETED' && pending >= 0 ? 'disabled title="请先完成所有取件点"' : ''} data-next="${esc(item.id)}" data-status="${next}">${item.pickup_stops?.length > 1 ? '更新整单状态：' : ''}${esc(statusActionLabel(next))}</button>`
        : '';
      const statusAction = pending >= 0 && statusButton
        ? `<details class="rider-status-actions"><summary>整单状态操作</summary>${statusButton}</details>`
        : statusButton;
      const actions = `${pickupAction}<button type="button" class="button outline" data-map-task="${esc(item.id)}">查看任务地图</button><button type="button" class="button danger-outline" data-report-incident="${esc(item.id)}">上报异常</button>${statusAction}`;
      return riderTaskCard(item, actions, { active: true, nextHint: activeNextHint(item) });
    }).join('')
    : `<div class="rider-empty"><span>暂无履约中的任务。</span><button type="button" class="button subtle" data-scroll-open>查看附近任务</button></div>`;
  document.querySelector('#activeNext').disabled = activeTaskItems.length < 50;
}

function updateLocateButton() {
  const button = document.querySelector('#locateTasks');
  if (!button) return;
  button.disabled = locating;
  button.textContent = locating ? '定位中…' : (riderPosition ? '重新定位' : '定位并排序');
}

async function hydrateRiderLocation() {
  if (!window.CityFlowLocation) return;
  const last = CityFlowLocation.getLastPosition();
  if (last) {
    riderPosition = last;
    return;
  }
  try {
    const permission = await navigator.permissions?.query({ name: 'geolocation' });
    if (permission?.state === 'granted') riderPosition = await CityFlowLocation.getCurrentPosition({ maximumAge: 60000 });
  } catch {
    // Permission or device location may be unavailable; the explicit button remains available.
  }
}

async function reportRiderPosition(point) {
  if (!point) return;
  await api('/api/v1/riders/me/location', { method: 'POST', body: JSON.stringify({ latitude: point.latitude, longitude: point.longitude, accuracy: point.accuracy || 0, source: 'device', observed_at: new Date().toISOString() }) });
}

async function toggleAvailability() {
  if (!riderProfile) return;
  const button = document.querySelector('#toggleAvailability');
  const next = riderProfile.status === 'AVAILABLE' ? 'OFFLINE' : 'AVAILABLE';
  button.disabled = true;
  try {
    riderProfile = await api('/api/v1/riders/me/status', { method: 'PUT', body: JSON.stringify({ status: next }) });
    renderRiderProfile();
    await loadRiderWorkspace();
    showNotice(next === 'AVAILABLE' ? '已上线，可接新任务。' : '已下线，当前不再接收新任务。', 'success');
  } catch (error) {
    button.disabled = false;
    showNotice('接单状态更新失败，请稍后重试。');
  }
}

async function locateForTaskSort() {
  if (locating) return;
  if (!window.CityFlowLocation) {
    showNotice('定位服务暂不可用，请稍后重试。');
    return;
  }
  locating = true;
  updateLocateButton();
  try {
    riderPosition = await CityFlowLocation.getCurrentPosition();
    await reportRiderPosition(riderPosition);
    await loadRiderWorkspace();
    showNotice('已获取当前位置，附近任务已按服务距离筛选。', 'success');
  } catch (error) {
    showNotice('定位失败，请稍后重试。');
    renderOpenTasks();
  } finally {
    locating = false;
    updateLocateButton();
  }
}

async function chooseSort(mode) {
  if (mode !== 'pickup' && mode !== 'delivery') return;
  sortMode = mode;
  renderOpenTasks();
  if (!riderPosition) await locateForTaskSort();
}

async function archiveRider() {
  if (!(await cfConfirm('归档后将无法继续接单。', { title: '归档骑手资料', confirmText: '归档', danger: true }))) return;
  const button = document.querySelector('#archiveRider');
  const notice = document.querySelector('#riderArchiveNotice');
  button.disabled = true;
  notice.textContent = '正在归档…';
  try {
    await api('/api/v1/riders/me', { method: 'DELETE' });
    riderProfile = null;
    document.querySelector('#riderDialog').close();
    document.querySelector('#riderProfile').innerHTML = '<div class="empty-state">骑手资料已归档。</div>';
    showNotice('骑手资料已归档。', 'success');
  } catch (error) {
    notice.textContent = '归档失败，请稍后重试。';
  } finally {
    button.disabled = false;
  }
}

function openRiderEditor() {
  if (!riderProfile) return;
  document.querySelector('#riderArchiveNotice').textContent = '';
  const form = document.querySelector('#riderForm');
  for (const key of ['display_name', 'vehicle_type', 'service_area', 'max_distance_meters', 'max_concurrent_tasks']) form.elements[key].value = riderProfile[key] ?? '';
  form.elements.skills.value = skillsText(riderProfile.skills || []);
  document.querySelector('#riderDialog').showModal();
}

let incidentTaskID = null;
let incidentPreviewURLs = [];

function openIncidentDialog(taskID) {
  incidentTaskID = taskID;
  const dialog = document.querySelector('#incidentDialog');
  const form = document.querySelector('#incidentForm');
  form.reset();
  document.querySelector('#incidentStatus').textContent = '';
  clearIncidentPreviews();
  dialog.showModal();
}

function clearIncidentPreviews() {
  for (const url of incidentPreviewURLs) URL.revokeObjectURL(url);
  incidentPreviewURLs = [];
  const host = document.querySelector('#incidentPreview');
  if (host) host.innerHTML = '';
}

function renderIncidentPreviews(files) {
  clearIncidentPreviews();
  const host = document.querySelector('#incidentPreview');
  for (const file of files.slice(0, 6)) {
    const url = URL.createObjectURL(file);
    incidentPreviewURLs.push(url);
    const image = document.createElement('img');
    image.src = url;
    image.alt = file.name;
    host.appendChild(image);
  }
}

async function uploadIncidentImage(file) {
  const token = getToken();
  const data = new FormData();
  data.append('file', file);
  const response = await fetch('/api/v1/uploads', { method: 'POST', headers: token ? { Authorization: `Bearer ${token}` } : {}, body: data });
  const body = await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(friendlyError(body.error, response.status));
  return body.url;
}

async function mutateTask(button, path, body) {
  button.disabled = true;
  try {
    const updated = await api(path, { method: 'POST', body: JSON.stringify(body) });
    const loaded = await loadRiderWorkspace();
    await loadWallet();
    showNotice(loaded ? '任务状态已更新。' : '操作已提交，请刷新页面。', loaded ? 'success' : 'error');
    if (path.endsWith('/claim')) {
      try { await openRiderMap(updated.id); } catch { showNotice('已接单，地图暂时无法加载。'); }
    }
  } catch (error) {
    button.disabled = false;
    showNotice(error.status === 409 ? '任务状态已变化，请刷新后重试。' : '操作失败，请稍后重试。');
  }
}

async function openRiderMap(id) {
  const panel = document.querySelector('.rider-map-details');
  panel.open = true;
  const ready = await CityFlowMapReady;
  ready?.map.invalidateSize();
  await ready?.refresh();
  const task = await api(`/api/v1/tasks/${encodeURIComponent(id)}`);
  ready?.selectTask(id, task);
  panel.scrollIntoView({ behavior: 'smooth' });
}

(async () => {
  if (!await requirePageRole('RIDER')) return;
  const dialog = document.querySelector('#riderDialog');
  document.querySelector('#refresh').addEventListener('click', loadRiderWorkspace);
  for (const [id, action] of [
    ['openPrevious', () => openOffset = Math.max(0, openOffset - 50)],
    ['openNext', () => openOffset += 50],
    ['activePrevious', () => activeOffset = Math.max(0, activeOffset - 50)],
    ['activeNext', () => activeOffset += 50],
  ]) document.querySelector(`#${id}`).addEventListener('click', () => { action(); loadRiderWorkspace(); });
  document.querySelector('#editProfile').addEventListener('click', openRiderEditor);
  document.querySelector('#toggleAvailability').addEventListener('click', toggleAvailability);
  document.querySelectorAll('[data-close-incident]').forEach(button => button.addEventListener('click', () => { clearIncidentPreviews(); document.querySelector('#incidentDialog').close(); }));
  document.querySelector('#incidentImages').addEventListener('change', event => renderIncidentPreviews(Array.from(event.target.files || [])));
  document.querySelector('#incidentForm').addEventListener('submit', async event => {
    event.preventDefault();
    if (!incidentTaskID) return;
    const form = event.currentTarget;
    const button = form.querySelector('button[type="submit"]');
    const status = document.querySelector('#incidentStatus');
    const body = Object.fromEntries(new FormData(form).entries());
    const files = Array.from(document.querySelector('#incidentImages').files || []).slice(0, 6);
    button.disabled = true;
    status.style.color = '';
    status.textContent = files.length ? '正在上传现场图片…' : '正在提交异常…';
    try {
      const image_urls = [];
      for (const file of files) image_urls.push(await uploadIncidentImage(file));
      await api(`/api/v1/tasks/${encodeURIComponent(incidentTaskID)}/incidents`, { method: 'POST', body: JSON.stringify({ kind: body.kind, note: body.note, image_urls }) });
      status.textContent = '';
      document.querySelector('#incidentDialog').close();
      clearIncidentPreviews();
      await loadRiderWorkspace();
      showNotice('异常已提交，平台处理后会更新任务记录。', 'success');
    } catch (error) {
      status.style.color = '#a04b3e';
      status.textContent = '收款账户保存失败，请稍后重试。';
    } finally { button.disabled = false; }
  });
  document.querySelector('#openPayout').addEventListener('click', openPayoutEditor);
  document.querySelector('#archiveRider').addEventListener('click', archiveRider);
  document.querySelectorAll('[data-close]').forEach(button => button.addEventListener('click', () => dialog.close()));
  document.querySelectorAll('[data-close-payout]').forEach(button => button.addEventListener('click', () => document.querySelector('#payoutDialog').close()));
  document.querySelector('#payoutForm').addEventListener('submit', async event => {
    event.preventDefault();
    const button = event.currentTarget.querySelector('[type="submit"]');
    const status = document.querySelector('#payoutNotice');
    const body = Object.fromEntries(new FormData(event.currentTarget).entries());
    button.disabled = true; status.style.color = ''; status.textContent = '正在保存…';
    try {
      payoutAccount = await api('/api/v1/riders/me/payout-account', { method: 'PUT', body: JSON.stringify(body) });
      document.querySelector('#payoutDialog').close();
      renderWallet();
      showNotice('收款账户已保存。', 'success');
    } catch (error) { status.style.color = '#a04b3e'; status.textContent = '收款账户保存失败，请稍后重试。'; }
    finally { button.disabled = false; }
  });
  document.querySelector('#riderForm').addEventListener('submit', async event => {
    event.preventDefault();
    const body = Object.fromEntries(new FormData(event.currentTarget).entries());
    body.skills = parseSkills(body.skills);
    body.max_distance_meters = Number(body.max_distance_meters);
    body.max_concurrent_tasks = Number(body.max_concurrent_tasks);
    try {
      riderProfile = await api('/api/v1/riders/me', { method: 'PUT', body: JSON.stringify(body) });
      dialog.close();
      renderRiderProfile();
      showNotice('骑手服务资料已保存。', 'success');
    } catch (error) {
      showNotice('服务资料保存失败，请稍后重试。');
    }
  });
  document.querySelector('.rider-sort-bar').addEventListener('click', event => {
    const chip = event.target.closest('[data-sort]');
    if (chip) chooseSort(chip.dataset.sort);
    if (event.target.closest('#locateTasks')) locateForTaskSort();
  });
  document.querySelector('#openTasks').addEventListener('click', event => {
    const locate = event.target.closest('[data-locate]');
    if (locate) { locateForTaskSort(); return; }
    if (event.target.closest('[data-go-online]')) { toggleAvailability(); return; }
    const button = event.target.closest('[data-claim]');
    if (button) mutateTask(button, `/api/v1/tasks/${encodeURIComponent(button.dataset.claim)}/claim`, {});
  });
  document.querySelector('#activeTasks').addEventListener('click', async event => {
    const scrollOpen = event.target.closest('[data-scroll-open]');
    if (scrollOpen) { document.querySelector('#openTasksPanel').scrollIntoView({ behavior: 'smooth', block: 'start' }); return; }
    const pickup = event.target.closest('[data-pickup-task]');
    if (pickup) {
      mutateTask(pickup, `/api/v1/tasks/${encodeURIComponent(pickup.dataset.pickupTask)}/pickups/${pickup.dataset.index}`, { status: pickup.dataset.pickupStatus });
      return;
    }
    const mapButton = event.target.closest('[data-map-task]');
    if (mapButton) { openRiderMap(mapButton.dataset.mapTask); return; }
    const incidentButton = event.target.closest('[data-report-incident]');
    if (incidentButton) { openIncidentDialog(incidentButton.dataset.reportIncident); return; }
    const button = event.target.closest('[data-next]');
    if (button) {
      if (button.dataset.status === 'COMPLETED' && !(await cfConfirm('确认后任务将标记为已完成。', { title: '完成任务', confirmText: '确认完成' }))) return;
      mutateTask(button, `/api/v1/tasks/${encodeURIComponent(button.dataset.next)}/status`, { status: button.dataset.status });
    }
  });
  await hydrateRiderLocation();
  await loadRiderWorkspace();
  await loadWallet();
  setInterval(() => { if (!document.hidden && !document.activeElement?.closest('.rider-dashboard')) loadRiderWorkspace(); }, 15000);
  const mapDetails = document.querySelector('.rider-map-details');
  const syncMapToggle = () => {
    const label = mapDetails?.querySelector('.rider-map-toggle');
    if (label) label.textContent = mapDetails.open ? '关闭地图' : '展开地图';
  };
  syncMapToggle();
  mapDetails?.addEventListener('toggle', async event => {
    syncMapToggle();
    if (event.currentTarget.open) (await CityFlowMapReady)?.map.invalidateSize();
  });
})();

