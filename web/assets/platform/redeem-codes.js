const redeemState = {
  coupons: [],
  items: [],
  offset: 0,
  limit: 20,
  loading: false,
};
let pendingBatchNo = '';

function newBatchNo() {
  const suffix = (window.crypto?.randomUUID?.() || `${Date.now().toString(36)}${Math.random().toString(36).slice(2, 8)}`)
    .replace(/-/g, '').slice(0, 12).toUpperCase();
  return `WEB-${suffix}`;
}

const redeemStatusText = { ACTIVE: '可兑换', DISABLED: '已停用' };

function redeemDate(ms) {
  if (!ms) return '长期有效';
  const date = new Date(Number(ms));
  if (Number.isNaN(date.getTime())) return '—';
  return date.toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' });
}

function creatorLabel(value) {
  if (!value) return '—';
  if (String(value).toLowerCase() === 'system') return '系统';
  return value;
}

function redeemCouponLabel(couponID) {
  const coupon = redeemState.coupons.find(item => item.id === couponID);
  return coupon ? coupon.title : '未知优惠券';
}

function redeemParams(includePaging = true) {
  const params = new URLSearchParams();
  const couponID = document.querySelector('#filterCoupon').value;
  const batchNo = document.querySelector('#filterBatch').value.trim();
  const status = document.querySelector('#filterStatus').value;
  if (couponID) params.set('coupon_id', couponID);
  if (batchNo) params.set('batch_no', batchNo);
  if (status) params.set('status', status);
  if (includePaging) {
    params.set('limit', String(redeemState.limit));
    params.set('offset', String(redeemState.offset));
  }
  return params;
}

function renderRedeemRows() {
  const body = document.querySelector('#codeTableBody');
  const items = redeemState.items;
  document.querySelector('#resultCount').textContent = `${items.length} 条记录`;
  document.querySelector('#pageLabel').textContent = `第 ${Math.floor(redeemState.offset / redeemState.limit) + 1} 页`;
  document.querySelector('#previousPage').disabled = redeemState.offset === 0;
  document.querySelector('#nextPage').disabled = items.length < redeemState.limit;
  if (!items.length) {
    body.innerHTML = '<tr><td colspan="6" class="table-empty">当前筛选条件下暂无兑换码。</td></tr>';
    return;
  }
  body.innerHTML = items.map(item => {
    const canDisable = item.status === 'ACTIVE';
    return `<tr>
      <td><span class="redeem-code">${esc(item.code)}</span></td>
      <td><span class="cell-primary">${esc(redeemCouponLabel(item.coupon_id))}</span><span class="cell-secondary">${esc(item.batch_no || '—')}</span></td>
      <td><span class="task-status ${canDisable ? 'open' : 'cancelled'}">${esc(redeemStatusText[item.status] || item.status)}</span><span class="cell-secondary">${Number(item.used_count || 0)} / ${Number(item.max_redeem || 0)} 已兑换</span></td>
      <td><span class="cell-primary">${esc(redeemDate(item.starts_at_ms))}</span><span class="cell-secondary">至 ${esc(redeemDate(item.ends_at_ms))}</span></td>
      <td><span class="cell-primary">${esc(creatorLabel(item.created_by))}</span><span class="cell-secondary">${esc(item.note || '—')}</span></td>
      <td><div class="redeem-row-actions"><button class="button outline" type="button" data-copy-code="${esc(item.code)}">复制</button>${canDisable ? `<button class="button danger" type="button" data-disable-code="${esc(item.id)}">停用</button>` : '<span class="cell-secondary">—</span>'}</div></td>
    </tr>`;
  }).join('');
}

async function loadRedeemCodes() {
  if (redeemState.loading) return;
  redeemState.loading = true;
  document.querySelector('#codeTableBody').innerHTML = '<tr><td colspan="6" class="table-empty">正在加载兑换码…</td></tr>';
  try {
    const result = await api(`/api/v1/platform/redeem-codes?${redeemParams()}`);
    redeemState.items = result.data || [];
    renderRedeemRows();
  } catch (error) {
    document.querySelector('#codeTableBody').innerHTML = '<tr><td colspan="6" class="table-empty">兑换码加载失败，请稍后重试。</td></tr>';
    document.querySelector('#resultCount').textContent = '加载失败';
    showNotice('兑换码加载失败，请稍后重试。');
  } finally {
    redeemState.loading = false;
  }
}

async function loadRedeemCoupons() {
  const result = await api('/api/v1/coupons?limit=100');
  redeemState.coupons = result.coupons || [];
  const options = redeemState.coupons.map(item => `<option value="${esc(item.id)}">${esc(item.title)}</option>`).join('');
  document.querySelector('#couponSelect').innerHTML = options || '<option value="">暂无可用优惠券</option>';
  document.querySelector('#filterCoupon').innerHTML = `<option value="">全部优惠券</option>${options}`;
}

async function generateRedeemCodes(event) {
  event.preventDefault();
  const couponID = document.querySelector('#couponSelect').value;
  const count = Number(document.querySelector('#codeCount').value);
  const maxRedeem = Number(document.querySelector('#maxRedeem').value);
  if (!couponID) {
    showNotice('请选择一张优惠券。');
    return;
  }
  if (!Number.isInteger(count) || count < 1 || count > 5000) {
    showNotice('生成数量需在 1 到 5000 之间。');
    return;
  }
  if (!Number.isInteger(maxRedeem) || maxRedeem < 1) {
    showNotice('每码可兑换人数必须大于 0。');
    return;
  }
  const startsAt = document.querySelector('#startsAt').value;
  const endsAt = document.querySelector('#endsAt').value;
  let batchNo = document.querySelector('#batchNo').value.trim();
  if (!batchNo) {
    if (!pendingBatchNo) pendingBatchNo = newBatchNo();
    batchNo = pendingBatchNo;
  }
  const body = {
    count,
    max_redeem: maxRedeem,
    batch_no: batchNo,
    note: document.querySelector('#codeNote').value.trim(),
  };
  if (startsAt) body.starts_at = new Date(startsAt).toISOString();
  if (endsAt) body.ends_at = new Date(endsAt).toISOString();
  const button = event.currentTarget.querySelector('button[type="submit"]');
  const label = button.textContent;
  button.disabled = true;
  button.textContent = '生成中…';
  try {
    const result = await api(`/api/v1/platform/coupons/${encodeURIComponent(couponID)}/redeem-codes`, {
      method: 'POST', body: JSON.stringify(body),
    });
    showNotice(`已生成 ${result.data?.length || 0} 个兑换码，批次 ${result.batch_no || '—'}。`, 'success');
    pendingBatchNo = '';
    document.querySelector('#batchNo').value = '';
    redeemState.offset = 0;
    await loadRedeemCodes();
  } catch (error) {
    showNotice(error.message);
  } finally {
    button.disabled = false;
    button.textContent = label;
  }
}

async function disableRedeemCode(id) {
  if (!(await cfConfirm('停用后该兑换码将无法继续兑换。', { title: '停用兑换码', confirmText: '停用', danger: true }))) return;
  try {
    await api(`/api/v1/platform/redeem-codes/${encodeURIComponent(id)}/disable`, { method: 'POST', body: '{}' });
    showNotice('兑换码已停用。', 'success');
    await loadRedeemCodes();
  } catch (error) {
    showNotice(error.message);
  }
}

async function copyRedeemCode(code) {
  try {
    await navigator.clipboard.writeText(code);
    showNotice('兑换码已复制。', 'success');
  } catch {
    showNotice('复制失败，请手动选择兑换码。');
  }
}

async function exportRedeemCodes() {
  const path = `/api/v1/platform/redeem-codes/export?${redeemParams(false)}`;
  let token = getToken();
  let response = await fetch(path, { headers: token ? { Authorization: `Bearer ${token}` } : {} });
  if (response.status === 401 && token) {
    token = await refreshAccessToken(token);
    response = await fetch(path, { headers: token ? { Authorization: `Bearer ${token}` } : {} });
  }
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    showNotice('导出失败，请稍后重试。');
    return;
  }
  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = 'cityflow-redeem-codes.csv';
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
  URL.revokeObjectURL(url);
  showNotice('兑换码已导出。', 'success');
}

(async () => {
  if (!await requirePageRole('ADMIN')) return;
  await loadRedeemCoupons().catch(() => showNotice('优惠券加载失败，请稍后重试。'));
  document.querySelector('#redeemForm').addEventListener('submit', generateRedeemCodes);
  document.querySelector('#refresh').addEventListener('click', loadRedeemCodes);
  document.querySelector('#filterCoupon').addEventListener('change', () => { redeemState.offset = 0; loadRedeemCodes(); });
  document.querySelector('#filterStatus').addEventListener('change', () => { redeemState.offset = 0; loadRedeemCodes(); });
  let batchTimer = 0;
  document.querySelector('#filterBatch').addEventListener('input', () => {
    clearTimeout(batchTimer);
    batchTimer = setTimeout(() => { redeemState.offset = 0; loadRedeemCodes(); }, 260);
  });
  document.querySelector('#exportButton').addEventListener('click', exportRedeemCodes);
  document.querySelector('#previousPage').addEventListener('click', () => {
    redeemState.offset = Math.max(0, redeemState.offset - redeemState.limit);
    loadRedeemCodes();
  });
  document.querySelector('#nextPage').addEventListener('click', () => {
    redeemState.offset += redeemState.limit;
    loadRedeemCodes();
  });
  document.querySelector('#codeTableBody').addEventListener('click', event => {
    const copy = event.target.closest('[data-copy-code]');
    if (copy) { copyRedeemCode(copy.dataset.copyCode); return; }
    const disable = event.target.closest('[data-disable-code]');
    if (disable) { disableRedeemCode(disable.dataset.disableCode); }
  });
  await loadRedeemCodes();
})();
