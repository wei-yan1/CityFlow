const views = {
  tasks: {
    title: '任务调度', kicker: 'CITY OPERATIONS', description: '查看任务流转情况，及时发现待处理事项。', tableTitle: '平台任务',
    endpoint: '/api/v1/tasks', create: false, filters: [['', '全部状态'], ['OPEN', '待接单'], ['CLAIMED', '已接单'], ['ARRIVED', '已到达'], ['PROCESSING', '进行中'], ['COMPLETED', '已完成'], ['CANCELLED', '已取消']],
    columns: [['任务', '任务'], ['type', '类型'], ['route', '行程'], ['created_by', '发布用户'], ['status', '状态'], ['created_at', '发布时间']]
  },
  users: {
    title: '用户账号', kicker: 'ACCOUNT DIRECTORY', description: '管理用户、商家和骑手的登录账号与使用状态。', tableTitle: '账号列表',
    endpoint: '/api/v1/platform/users', create: true, createLabel: '新建账号', filters: [['', '全部状态'], ['ACTIVE', '正常'], ['DISABLED', '已停用'], ['PENDING', '待启用']],
    columns: [['name', '账号'], ['email', '邮箱'], ['role', '角色'], ['phone', '电话'], ['status', '状态'], ['updated_at', '最近更新']]
  },
  merchants: {
    title: '商家资料', kicker: 'MERCHANT DIRECTORY', description: '维护门店位置、服务范围和接单状态。', tableTitle: '商家门店',
    endpoint: '/api/v1/platform/merchants', create: true, createLabel: '新建商家资料', filters: [['', '全部状态'], ['OPEN', '营业中'], ['CLOSED', '已关闭'], ['PAUSED', '暂停接单']],
    columns: [['name', '门店'], ['address', '门店地址'], ['phone', '联系电话'], ['business_status', '营业状态'], ['capacity_per_hour', '每小时容量'], ['updated_at', '最近更新']]
  },
  riders: {
    title: '骑手资料', kicker: 'RIDER DIRECTORY', description: '维护骑手服务能力、车辆和接单资料。', tableTitle: '骑手名册',
    endpoint: '/api/v1/platform/riders', create: true, createLabel: '新建骑手资料', filters: [['', '全部状态'], ['AVAILABLE', '可接单'], ['OFFLINE', '离线'], ['BUSY', '执行中'], ['SUSPENDED', '已停用']],
    columns: [['display_name', '骑手'], ['vehicle_type', '车辆'], ['service_area', '服务区域'], ['status', '工作状态'], ['rating', '评分'], ['completed_tasks', '完成任务']]
  },
  registrations: {
    title: '骑手注册', kicker: 'RIDER ONBOARDING', description: '核对骑手提交的支付金额和支付宝凭证，通过后自动创建账号与骑手资料。', tableTitle: '注册申请',
    endpoint: '/api/v1/platform/rider-registrations', create: false, filters: [['', '全部状态'], ['PENDING', '待核验'], ['APPROVED', '已开通'], ['REJECTED', '已驳回']],
    columns: [['created_at', '提交时间'], ['display_name', '申请人'], ['email', '邮箱'], ['phone', '手机号'], ['paid_amount_fen', '支付金额'], ['payment_reference', '支付凭证'], ['status', '审核状态'], ['review_note', '审核备注']]
  },
  reports: {
    title: '骑手举报', kicker: 'SAFETY & SUPPORT', description: '查看用户提交的骑手举报、图片证据，并标记处理结果。', tableTitle: '举报记录',
    endpoint: '/api/v1/platform/rider-reports', create: false, filters: [['', '全部状态'], ['OPEN', '待处理'], ['RESOLVED', '已处理'], ['DISMISSED', '已驳回']],
    columns: [['created_at', '提交时间'], ['task_title', '关联订单'], ['rider_name', '被举报骑手'], ['user_name', '举报用户'], ['reason', '原因与说明'], ['images', '图片证据'], ['status', '处理状态']]
  },
  incidents: {
    title: '任务异常', kicker: 'EXCEPTION DESK', description: '处理骑手或用户上报的履约异常，填写处理结果，必要时取消订单。', tableTitle: '异常记录',
    endpoint: '/api/v1/platform/task-incidents', create: false, filters: [['', '全部状态'], ['OPEN', '待处理'], ['RESOLVED', '已处理']],
    columns: [['created_at', '上报时间'], ['task_id', '关联订单'], ['reporter_role', '上报方'], ['kind', '异常类型'], ['note', '说明'], ['images', '现场图片'], ['status', '处理状态'], ['resolution', '处理结果']]
  },
  reviews: {
    title: '评价审计', kicker: 'PRIVACY & TRUST', description: '默认只看脱敏标签；查看真实身份必须填写理由并写入审计。', tableTitle: '商家评价',
    endpoint: '/api/v1/platform/reviews', create: false, filters: [],
    columns: [['created_at', '评价时间'], ['author_label', '公开标签'], ['anonymous', '匿名'], ['user_id_masked', '用户标识'], ['order_id', '关联订单'], ['rating', '评分'], ['comment', '内容']]
  }
};

const statusText = { OPEN: '待接单', CLAIMED: '已接单', ARRIVED: '已到达', PROCESSING: '进行中', COMPLETED: '已完成', CANCELLED: '已取消', ACTIVE: '正常', DISABLED: '已停用', PENDING: '待启用', MERCHANT: '商家', RIDER: '骑手', USER: '用户', ADMIN: '平台管理员', AVAILABLE: '可接单', OFFLINE: '离线', BUSY: '执行中', SUSPENDED: '已停用', CLOSED: '已关闭', PAUSED: '暂停接单', OPEN_BUSINESS: '营业中', RESOLVED: '已处理', DISMISSED: '已驳回', PUBLISHED: '已发布', RECORDED: '已记录', APPROVED: '已开通', REJECTED: '已驳回', CONTACT_FAILED: '联系不到人', PICKUP_FAILED: '取不到物品', ADDRESS_WRONG: '地址有误', RIDER_CANCEL: '骑手申请取消', USER_CANCEL: '用户申请取消', PLATFORM_HELP: '请求平台协助', OTHER: '其他异常' };
const registrationStatus = { PENDING: '待核验', APPROVED: '已开通', REJECTED: '已驳回' };
const incidentStatus = { OPEN: '待处理', RESOLVED: '已处理' };
const reportStatus = { OPEN: '待处理', RESOLVED: '已处理', DISMISSED: '已驳回' };
let activeView = 'tasks';
let currentItems = [];
let pendingIncident = null;
let pageOffset = 0;
const pageLimit = 20;
let editingID = '';
let loadSequence = 0;

function valueText(value) { return statusText[value] || value || '—'; }
function safeDate(value) { return value ? new Date(value).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) : '—'; }
function userRole(role) { return valueText(role); }
function itemID(item) { return ['tasks', 'reports', 'registrations', 'incidents', 'reviews'].includes(activeView) ? item.id : item.user_id; }

function setView(view) {
  if (!views[view]) return;
  activeView = view;
  pageOffset = 0;
  document.querySelectorAll('[data-view]').forEach(button => button.classList.toggle('active', button.dataset.view === view));
  const config = views[view];
  document.querySelector('#crumbTitle').textContent = config.title;
  document.querySelector('#viewTitle').textContent = config.title;
  document.querySelector('#viewKicker').textContent = config.kicker;
  document.querySelector('#viewDescription').textContent = config.description;
  document.querySelector('#tableTitle').textContent = config.tableTitle;
  document.querySelector('#createButton').hidden = !config.create;
  document.querySelector('#createLabel').textContent = config.createLabel || '新建';
  document.querySelector('#search').value = '';
  fillFilters(config.filters);
  renderHead(config.columns);
  loadCurrentPage();
}

function fillFilters(filters) {
  const select = document.querySelector('#statusFilter');
  select.innerHTML = filters.map(([value, label]) => `<option value="${esc(value)}">${esc(label)}</option>`).join('');
  select.hidden = filters.length < 2;
}

function renderHead(columns) {
  document.querySelector('#tableHead').innerHTML = `<tr>${columns.map(([, title]) => `<th>${esc(title)}</th>`).join('')}<th class="actions-head">操作</th></tr>`;
}

function statusPill(value, field = 'status') {
  const className = ['OPEN', 'AVAILABLE', 'ACTIVE', 'APPROVED'].includes(value) ? 'open' : ['COMPLETED', 'RESOLVED'].includes(value) ? 'completed' : ['CANCELLED', 'DISABLED', 'SUSPENDED', 'DISMISSED', 'REJECTED'].includes(value) ? 'cancelled' : ['CLAIMED', 'ARRIVED', 'PROCESSING', 'BUSY', 'PAUSED', 'PENDING'].includes(value) ? 'claimed' : '';
  const label = activeView === 'registrations' ? (registrationStatus[value] || valueText(value)) : activeView === 'incidents' ? (incidentStatus[value] || valueText(value)) : activeView === 'reports' ? (reportStatus[value] || valueText(value)) : field === 'business_status' && value === 'OPEN' ? '营业中' : valueText(value);
  return `<span class="task-status ${className}">${esc(label)}</span>`;
}

function reportImages(images) {
  if (!Array.isArray(images) || !images.length) return '<span class="cell-secondary">无图片</span>';
  return `<div class="report-thumbs">${images.slice(0, 4).map((src, index) => `<a href="${esc(src)}" target="_blank" rel="noopener" title="打开举报图片 ${index + 1}"><img src="${esc(src)}" alt="举报图片 ${index + 1}" loading="lazy"></a>`).join('')}${images.length > 4 ? `<span class="cell-secondary">+${images.length - 4}</span>` : ''}</div>`;
}

function renderCell(item, field) {
  if (field === 'name') return `<span class="cell-primary">${esc(item.name)}</span><span class="cell-secondary">${esc(item.id)}</span>`;
  if (field === 'display_name') return `<span class="cell-primary">${esc(item.display_name)}</span>${item.user_id ? `<span class="cell-secondary">${esc(item.user_id)}</span>` : ''}`;
  if (field === 'route') return `<span class="cell-primary">${esc(item.pickup)}</span><span class="cell-secondary">至 ${esc(item.destination)}</span>`;
  if (field === 'status' || field === 'business_status') return statusPill(item[field], field);
  if (field === 'role') return esc(userRole(item.role));
  if (field === 'created_at' || field === 'updated_at') return esc(safeDate(item[field]));
  if (field === 'service_area') return esc(item.service_area || '未设置');
  if (field === 'capacity_per_hour') return `${esc(item.capacity_per_hour)} 单/小时`;
  if (field === 'completed_tasks') return `${esc(item.completed_tasks)} 单`;
  if (field === 'rating') return `★ ${esc(Number(item.rating || 0).toFixed(1))}`;
  if (field === 'task_title') return item.task_title ? `<span class="cell-primary">${esc(item.task_title)}</span><span class="cell-secondary">${esc(item.task_id)}</span>` : esc(item.task_id);
  if (field === 'paid_amount_fen') return `<span class="cell-primary">¥${(Number(item[field] || 0) / 100).toFixed(2)}</span>`;
  if (field === 'payment_reference') return `<span class="cell-secondary">${esc(item[field] || '—')}</span>`;
  if (field === 'review_note') return item[field] ? `<span class="cell-secondary">${esc(item[field])}</span>` : '<span class="cell-secondary">—</span>';
  if (field === 'rider_name') return esc(item.rider_name || item.rider_id);
  if (field === 'user_name') return `${esc(item.user_name || item.user_id)}<span class="cell-secondary">${esc(item.user_id)}</span>`;
  if (field === 'reason') return `<span class="cell-primary">${esc(item.reason)}</span>${item.details ? `<span class="cell-secondary">${esc(item.details)}</span>` : ''}`;
  if (field === 'images') return reportImages(item.images || item.image_urls);
  if (field === 'reporter_role') return esc(valueText(item.reporter_role));
  if (field === 'kind') return `<span class="cell-primary">${esc(valueText(item.kind))}</span>`;
  if (field === 'note') return esc(item.note || '—');
  if (field === 'resolution') return esc(item.resolution || '—');
  if (field === 'task_id') return `<span class="cell-secondary">${esc(item.task_id || '—')}</span>`;
  if (field === 'author_label') return `<span class="cell-primary">${esc(item.author_label || '—')}</span>`;
  if (field === 'anonymous') return item.anonymous ? '<span class="task-status claimed">匿名</span>' : '<span class="cell-secondary">实名</span>';
  if (field === 'user_id_masked') return `<span class="cell-secondary">${esc(item.user_id_masked || '—')}</span>`;
  if (field === 'comment') return esc(item.comment || '仅评分');
  if (field === 'order_id') return `<span class="cell-secondary">${esc((item.order_id || '').slice(0, 8) || '—')}…</span>`;
  return esc(item[field]);
}

function rowMarkup(item, columns) {
  const id = itemID(item);
  let actions = '<span class="cell-secondary">—</span>';
  if (activeView === 'reports') {
    actions = item.status === 'OPEN' ? `<div class="row-actions"><button class="button outline" data-report-resolve="RESOLVED" data-report-id="${esc(id)}">标记已处理</button><button class="button danger" data-report-resolve="DISMISSED" data-report-id="${esc(id)}">驳回</button></div>` : `<div class="row-actions"><button class="button outline" data-report-resolve="OPEN" data-report-id="${esc(id)}">重新打开</button></div>`;
  } else if (activeView === 'incidents') {
    actions = item.status === 'OPEN' ? `<div class="row-actions"><button class="button outline" data-incident-resolve="${esc(id)}" data-incident-task="${esc(item.task_id)}">标记已处理</button><button class="button danger" data-incident-resolve="${esc(id)}" data-incident-task="${esc(item.task_id)}" data-incident-cancel="true">处理并取消订单</button></div>` : '<span class="cell-secondary">已处理</span>';
  } else if (activeView === 'registrations') {
    actions = item.status === 'PENDING' ? `<div class="row-actions"><button class="button primary" data-registration-approve="${esc(id)}">核验并开通</button><button class="button danger" data-registration-reject="${esc(id)}">驳回</button></div>` : `<span class="cell-secondary">${esc(registrationStatus[item.status] || item.status)}</span>`;
  } else if (activeView === 'reviews') {
    actions = `<div class="row-actions"><button class="button outline" data-review-reveal="${esc(id)}">查看身份</button></div>`;
  } else if (activeView !== 'tasks') {
    const deleteLabel = activeView === 'users' ? '停用' : '归档';
    actions = `<div class="row-actions"><button class="button outline" data-edit="${esc(id)}">编辑</button><button class="button danger" data-delete="${esc(id)}">${deleteLabel}</button></div>`;
  }
  return `<tr data-row-search="${esc(JSON.stringify(item).toLowerCase())}">${columns.map(([field]) => `<td>${renderCell(item, field)}</td>`).join('')}<td>${actions}</td></tr>`;
}

async function loadCurrentPage() {
  const sequence = ++loadSequence;
  const config = views[activeView];
  const status = document.querySelector('#statusFilter').value;
  const params = new URLSearchParams({ limit: String(pageLimit), offset: String(pageOffset) });
  if (status) params.set('status', status);
  const body = document.querySelector('#tableBody');
  body.innerHTML = `<tr><td colspan="${config.columns.length + 1}" class="table-empty">正在读取资料…</td></tr>`;
  try {
    const result = await api(`${config.endpoint}?${params}`);
    if (sequence !== loadSequence) return;
    currentItems = result.data || [];
    renderRows();
    document.querySelector('#resultCount').textContent = `${currentItems.length} 条记录`;
    document.querySelector('#pageLabel').textContent = `第 ${Math.floor(pageOffset / pageLimit) + 1} 页`;
    document.querySelector('#previousPage').disabled = pageOffset === 0;
    document.querySelector('#nextPage').disabled = currentItems.length < pageLimit;
    showNotice('');
  } catch (error) {
    if (sequence !== loadSequence) return;
    body.innerHTML = `<tr><td colspan="${config.columns.length + 1}" class="table-empty">${esc(error.message)}</td></tr>`;
    document.querySelector('#resultCount').textContent = '读取失败';
    showNotice(error.message);
  }
}

function renderRows() {
  const columns = views[activeView].columns;
  const query = document.querySelector('#search').value.trim().toLowerCase();
  const visible = currentItems.filter(item => !query || JSON.stringify(item).toLowerCase().includes(query));
  document.querySelector('#resultCount').textContent = query ? `${visible.length} 条匹配` : `${currentItems.length} 条记录`;
  document.querySelector('#tableBody').innerHTML = visible.length ? visible.map(item => rowMarkup(item, columns)).join('') : `<tr><td colspan="${columns.length + 1}" class="table-empty">${query ? '没有匹配的记录，换个关键词试试。' : '这里还没有资料。使用右上角按钮创建第一条记录。'}</td></tr>`;
}

const commonFields = {
  userCreate: [
    ['name', '姓名', 'text', true], ['email', '邮箱', 'email', true], ['phone', '手机号', 'tel', false],
    ['role', '账号角色', 'select', true, [['USER','用户'],['MERCHANT','商家'],['RIDER','骑手'],['ADMIN','平台管理员']]],
    ['password', '初始密码（至少 8 位）', 'password', true]
  ],
  userEdit: [['name','姓名','text',true],['email','邮箱','email',true],['phone','手机号','tel',false],['avatar_url','头像链接','url',false]],
  merchant: [
    ['user_id','关联账号 ID','text',true], ['name','门店名称','text',true], ['phone','联系电话','tel',false], ['address','门店地址','text',true],
    ['latitude','纬度','number',true], ['longitude','经度','number',true], ['business_status','营业状态','select',true,[['OPEN','营业中'],['CLOSED','已关闭'],['PAUSED','暂停接单']]],
    ['service_types','服务类型（逗号分隔）','text',true], ['capacity_per_hour','每小时容量','number',true], ['description','门店介绍','textarea',false]
  ],
  merchantEdit: [
    ['name','门店名称','text',true], ['phone','联系电话','tel',false], ['address','门店地址','text',true], ['latitude','纬度','number',true],
    ['longitude','经度','number',true], ['business_status','营业状态','select',true,[['OPEN','营业中'],['CLOSED','已关闭'],['PAUSED','暂停接单']]],
    ['service_types','服务类型（逗号分隔）','text',true], ['capacity_per_hour','每小时容量','number',true], ['description','门店介绍','textarea',false]
  ],
  rider: [
    ['user_id','关联账号 ID','text',true], ['display_name','骑手姓名','text',true], ['vehicle_type','车辆类型','select',true,[['WALK','步行'],['BICYCLE','自行车'],['E_BIKE','电动车'],['CAR','汽车'],['OTHER','其他']]],
    ['service_area','服务区域','text',false], ['skills','服务类型（逗号分隔）','text',true], ['max_distance_meters','最大服务距离（米）','number',true], ['max_concurrent_tasks','最大并行任务','number',true]
  ],
  riderEdit: [
    ['display_name','骑手姓名','text',true], ['vehicle_type','车辆类型','select',true,[['WALK','步行'],['BICYCLE','自行车'],['E_BIKE','电动车'],['CAR','汽车'],['OTHER','其他']]],
    ['service_area','服务区域','text',false], ['skills','服务类型（逗号分隔）','text',true], ['max_distance_meters','最大服务距离（米）','number',true], ['max_concurrent_tasks','最大并行任务','number',true]
  ]
};

function fieldMarkup([name, label, type, required, options]) {
  const full = ['textarea'].includes(type) || ['service_types', 'skills', 'address', 'description'].includes(name) ? ' full' : '';
  let input;
  if (type === 'select') input = `<select name="${name}" ${required ? 'required' : ''}>${options.map(([value, text]) => `<option value="${value}">${text}</option>`).join('')}</select>`;
  else if (type === 'textarea') input = `<textarea name="${name}" ${required ? 'required' : ''}></textarea>`;
  else input = `<input name="${name}" type="${type}" ${type === 'number' ? 'step="any"' : ''} ${required ? 'required' : ''} ${name === 'password' ? 'minlength="8"' : ''}>`;
  return `<label class="dialog-field${full}">${esc(label)}${input}</label>`;
}

function editorValues(item) {
  if (!item) return {};
  if (activeView === 'users') return item;
  return { ...item, service_types: (item.service_types || item.skills || []).join(', ') };
}

function openEditor(item = null) {
  editingID = item ? itemID(item) : '';
  const fields = activeView === 'users' ? (item ? commonFields.userEdit : commonFields.userCreate)
    : activeView === 'merchants' ? (item ? commonFields.merchantEdit : commonFields.merchant)
    : (item ? commonFields.riderEdit : commonFields.rider);
  document.querySelector('#dialogTitle').textContent = `${item ? '编辑' : '新建'}${activeView === 'users' ? '账号' : activeView === 'merchants' ? '商家资料' : '骑手资料'}`;
  document.querySelector('#editorFields').innerHTML = fields.map(fieldMarkup).join('');
  const values = editorValues(item);
  for (const [key, value] of Object.entries(values)) {
    const input = document.querySelector(`#editorFields [name="${key}"]`);
    if (input && value !== null && value !== undefined) input.value = value;
  }
  document.querySelector('#saveButton').textContent = item ? '保存修改' : '创建资料';
  document.querySelector('#editorDialog').showModal();
}

function formPayload(form) {
  const data = Object.fromEntries(new FormData(form).entries());
  for (const key of ['latitude', 'longitude', 'capacity_per_hour', 'max_distance_meters', 'max_concurrent_tasks']) {
    if (key in data) data[key] = Number(data[key]);
  }
  for (const key of ['service_types', 'skills']) {
    if (key in data) data[key] = data[key].split(',').map(value => value.trim()).filter(Boolean);
  }
  if (activeView === 'riders' && 'service_types' in data) { data.skills = data.service_types; delete data.service_types; }
  return data;
}

async function saveItem(event) {
  event.preventDefault();
  const button = document.querySelector('#saveButton');
  button.disabled = true;
  try {
    const body = formPayload(event.currentTarget);
    const endpoint = views[activeView].endpoint;
    const path = editingID ? `${endpoint}/${encodeURIComponent(editingID)}` : endpoint;
    const result = await api(path, { method: editingID ? 'PUT' : 'POST', body: JSON.stringify(body) });
    document.querySelector('#editorDialog').close();
    showNotice(editingID ? '修改已保存。' : '资料已创建。');
    if (!editingID && activeView === 'users' && ['MERCHANT', 'RIDER'].includes(body.role)) {
      showNotice('账号已创建。请切换到对应资料页，为该账号补充商家或骑手资料。');
    }
    if (result.id && pageOffset > 0 && !editingID) pageOffset = 0;
    await loadCurrentPage();
  } catch (error) {
    showNotice(error.message);
  } finally {
    button.disabled = false;
  }
}

async function archiveItem(id) {
  const item = currentItems.find(candidate => itemID(candidate) === id);
  const label = activeView === 'users' ? `停用账号“${item?.name || id}”` : `归档“${item?.name || item?.display_name || id}”资料`;
  if (!window.confirm(`${label}？历史任务记录会保留。`)) return;
  try {
    await api(`${views[activeView].endpoint}/${encodeURIComponent(id)}`, { method: 'DELETE' });
    showNotice(activeView === 'users' ? '账号已停用。' : '资料已归档。');
    await loadCurrentPage();
  } catch (error) { showNotice(error.message); }
}

async function resolveReport(id, status) {
  const item = currentItems.find(candidate => candidate.id === id);
  const label = status === 'RESOLVED' ? '标记为已处理' : status === 'DISMISSED' ? '驳回举报' : '重新打开举报';
  const note = window.prompt(`${label}${item?.rider_name ? `（${item.rider_name}）` : ''}，可填写处理说明：`, '');
  if (note === null) return;
  try {
    await api(`${views[activeView].endpoint}/${encodeURIComponent(id)}`, { method: 'PATCH', body: JSON.stringify({ status, note }) });
    showNotice('举报状态已更新。');
    await loadCurrentPage();
  } catch (error) { showNotice(error.message); }
}

function resolveIncident(taskID, incidentID, cancel) {
  const item = currentItems.find(candidate => candidate.id === incidentID);
  pendingIncident = { taskID, incidentID, cancel };
  const dialog = document.querySelector('#incidentResolveDialog');
  document.querySelector('#incidentResolveTitle').textContent = cancel ? '处理异常并取消订单' : '标记异常已处理';
  document.querySelector('#incidentResolveSummary').textContent = `${item?.kind ? valueText(item.kind) : '履约异常'} · 订单 ${String(taskID).slice(0, 8)}…`;
  document.querySelector('#incidentCancelHint').textContent = cancel ? '确认后这笔订单会被取消，骑手和用户都会看到状态变更。' : '这里只关闭异常记录，不会改变订单状态。';
  document.querySelector('#incidentResolution').value = cancel ? '平台核实后取消订单' : '已协调处理';
  document.querySelector('#incidentResolveStatus').textContent = '';
  dialog.showModal();
  document.querySelector('#incidentResolution').focus();
}

async function submitIncidentResolution(event) {
  event.preventDefault();
  if (!pendingIncident) return;
  const resolution = document.querySelector('#incidentResolution').value.trim();
  if (!resolution) {
    document.querySelector('#incidentResolveStatus').textContent = '请填写处理结果。';
    return;
  }
  const button = event.currentTarget.querySelector('button[type="submit"]');
  const status = document.querySelector('#incidentResolveStatus');
  button.disabled = true;
  status.textContent = '正在保存处理结果…';
  try {
    await api(`/api/v1/tasks/${encodeURIComponent(pendingIncident.taskID)}/incidents/${encodeURIComponent(pendingIncident.incidentID)}/resolve`, {
      method: 'POST',
      body: JSON.stringify({ resolution, cancel: Boolean(pendingIncident.cancel) }),
    });
    const cancelled = pendingIncident.cancel;
    pendingIncident = null;
    document.querySelector('#incidentResolveDialog').close();
    showNotice(cancelled ? '异常已处理，订单已取消。' : '异常已标记为处理。', 'success');
    await loadCurrentPage();
  } catch (error) {
    status.textContent = error.message;
  } finally {
    button.disabled = false;
  }
}

async function resolveRegistration(id, action) {
  const item = currentItems.find(candidate => candidate.id === id);
  const approving = action === 'approve';
  const label = approving ? '核验并开通' : '驳回注册';
  const note = window.prompt(`${label}${item?.display_name ? `（${item.display_name}）` : ''}，可填写审核说明：`, approving ? '已核对支付宝账单' : '未找到对应付款');
  if (note === null) return;
  try {
    await api(`${views[activeView].endpoint}/${encodeURIComponent(id)}/${action}`, { method: 'POST', body: JSON.stringify({ note }) });
    showNotice(approving ? '骑手账号与资料已开通。' : '注册申请已驳回。', 'success');
    await loadCurrentPage();
  } catch (error) { showNotice(error.message); }
}

async function revealReview(id) {
  const reason = window.prompt('查看真实身份会写入审计。请填写至少 10 个字的理由：', '');
  if (reason === null) return;
  if ([...reason.trim()].length < 10) {
    showNotice('理由至少需要 10 个字，未发起查看。');
    return;
  }
  try {
    const result = await api(`/api/v1/platform/reviews/${encodeURIComponent(id)}/reveal`, {
      method: 'POST', body: JSON.stringify({ reason: reason.trim() }),
    });
    showNotice(`真实身份：${result.user?.name || '未命名'}${result.user?.phone ? ` · ${result.user.phone}` : ''}。本次查看已记录审计。`, 'success');
  } catch (error) { showNotice(error.message); }
}

(async () => {
  if (!await requirePageRole('ADMIN')) return;
  document.querySelectorAll('[data-view]').forEach(button => button.addEventListener('click', () => setView(button.dataset.view)));
  document.querySelector('#refresh').addEventListener('click', loadCurrentPage);
  document.querySelector('#statusFilter').addEventListener('change', () => { pageOffset = 0; loadCurrentPage(); });
  document.querySelector('#search').addEventListener('input', renderRows);
  document.querySelector('#previousPage').addEventListener('click', () => { pageOffset = Math.max(0, pageOffset - pageLimit); loadCurrentPage(); });
  document.querySelector('#nextPage').addEventListener('click', () => { pageOffset += pageLimit; loadCurrentPage(); });
  document.querySelector('#createButton').addEventListener('click', () => openEditor());
  document.querySelector('#editorForm').addEventListener('submit', saveItem);
  document.querySelector('#incidentResolveForm').addEventListener('submit', submitIncidentResolution);
  document.querySelectorAll('[data-close-incident-resolve]').forEach(button => button.addEventListener('click', () => {
    pendingIncident = null;
    document.querySelector('#incidentResolveDialog').close();
  }));
  document.querySelector('#closeDialog').addEventListener('click', () => document.querySelector('#editorDialog').close());
  document.querySelector('#cancelDialog').addEventListener('click', () => document.querySelector('#editorDialog').close());
  document.querySelector('#tableBody').addEventListener('click', event => {
    const report = event.target.closest('[data-report-resolve]');
    if (report) { resolveReport(report.dataset.reportId, report.dataset.reportResolve); return; }
    const incident = event.target.closest('[data-incident-resolve]');
    if (incident) { resolveIncident(incident.dataset.incidentTask, incident.dataset.incidentResolve, incident.dataset.incidentCancel === 'true'); return; }
    const registrationApprove = event.target.closest('[data-registration-approve]');
    if (registrationApprove) { resolveRegistration(registrationApprove.dataset.registrationApprove, 'approve'); return; }
    const registrationReject = event.target.closest('[data-registration-reject]');
    if (registrationReject) { resolveRegistration(registrationReject.dataset.registrationReject, 'reject'); return; }
    const reviewReveal = event.target.closest('[data-review-reveal]');
    if (reviewReveal) { revealReview(reviewReveal.dataset.reviewReveal); return; }
    const edit = event.target.closest('[data-edit]');
    if (edit) { const item = currentItems.find(candidate => itemID(candidate) === edit.dataset.edit); if (item) openEditor(item); return; }
    const remove = event.target.closest('[data-delete]');
    if (remove) archiveItem(remove.dataset.delete);
  });
  setView('tasks');
})();
