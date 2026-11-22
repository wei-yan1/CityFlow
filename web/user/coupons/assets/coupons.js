const couponPage = document.querySelector('.coupon-page');
const isMinePage = location.pathname.replace(/\/$/, '').endsWith('/coupons/mine');
const couponState = { items: [], claims: [], filter: '', account: null, loading: false };
let toastTimer = 0;

function couponYuan(fen) {
  const amount = Number(fen || 0);
  if (!amount) return '¥0';
  return `¥${(amount / 100).toFixed(2).replace(/\.00$/, '')}`;
}

function couponDate(value) {
  if (!value) return '长期有效';
  const date = typeof value === 'number' || /^\d+$/.test(String(value)) ? new Date(Number(value)) : new Date(value);
  if (Number.isNaN(date.getTime())) return '长期有效';
  return date.toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' });
}

function couponScope(coupon) {
  const types = coupon?.applicable_task_types || [];
  if (!types.length) return '全部任务可用';
  const labels = { DELIVERY: '配送', PICKUP: '代取', PURCHASE: '代购', OTHER: '其他' };
  return `${types.map(type => labels[type] || type).join(' / ')}任务可用`;
}

function couponRule(coupon) {
  if (!coupon) return '';
  const parts = [];
  if (Number(coupon.min_spend_fen) > 0) parts.push(`满 ${couponYuan(coupon.min_spend_fen)} 可用`);
  if (coupon.type === 'PERCENT') {
    parts.push(`减 ${Number(coupon.discount_percent || 0)}%`);
    if (Number(coupon.max_discount_fen) > 0) parts.push(`最高减 ${couponYuan(coupon.max_discount_fen)}`);
  }
  if (!parts.length) parts.push('无门槛');
  return parts.join(' · ');
}

function couponValue(coupon) {
  if (!coupon) return '—';
  if (coupon.type === 'PERCENT') return `${Number(coupon.discount_percent || 0)}%`;
  return couponYuan(coupon.value_fen);
}

function hideCouponToast() {
  const toast = document.querySelector('#couponToast');
  clearTimeout(toastTimer);
  if (toast) {
    toast.classList.remove('show', 'error');
    toast.textContent = '';
  }
}

function showCouponToast(message, kind = 'success') {
  const toast = document.querySelector('#couponToast');
  if (!toast) return;
  toast.textContent = message;
  toast.classList.toggle('error', kind === 'error');
  toast.classList.add('show');
  clearTimeout(toastTimer);
  toastTimer = setTimeout(() => toast.classList.remove('show'), 2600);
}

function couponCard(coupon, claim = null, mode = 'center') {
  const status = claim?.status || 'UNUSED';
  const title = coupon?.title || '配送优惠券';
  const subtitle = coupon?.subtitle || '';
  const description = coupon?.description || '';
  const value = couponValue(coupon);
  const isPercent = coupon?.type === 'PERCENT';
  const used = status === 'USED';
  const expired = status === 'EXPIRED';
  const locked = status === 'LOCKED';
  const cardClass = ['coupon-card', isPercent ? 'percent' : '', used ? 'used' : '', expired ? 'expired' : ''].filter(Boolean).join(' ');
  const statusLabel = { UNUSED: '未使用', LOCKED: '支付中', USED: '已使用', EXPIRED: '已过期' }[status] || status;
  const remaining = Number(coupon?.remaining || 0);
  const total = Number(coupon?.total_quantity || 0);
  const stockPercent = total > 0 ? Math.min(100, Math.max(0, (remaining / total) * 100)) : 0;
  const claimCount = Number(coupon?.user_claim_count || 0);
  const limit = Number(coupon?.per_user_limit || 1);
  const ownedStatus = coupon?.user_coupon_status || 'UNUSED';
  const ownedLabel = { UNUSED: '已领取', LOCKED: '支付中', USED: '已使用', EXPIRED: '已过期' }[ownedStatus] || '已领取';
  let action = '';
  if (mode === 'center') {
    if (claimCount >= limit) {
      action = `<span class="coupon-claimed">${ownedLabel} <a href="/coupons/mine">${ownedStatus === 'UNUSED' ? '去使用' : '查看'}</a></span>`;
    } else if (remaining <= 0) {
      action = '<button class="button outline" type="button" disabled>已领完</button>';
    } else {
      action = `<button class="button primary" type="button" data-claim-coupon="${esc(coupon.id)}">立即领取</button>`;
    }
  } else if (status === 'UNUSED') {
    action = `<a class="button primary" href="/user">去使用</a>`;
  } else if (used && claim?.task_id) {
    action = `<a class="button outline" href="/tasks/${esc(claim.task_id)}">查看任务</a>`;
  } else {
    action = `<span class="coupon-claimed">${locked ? '支付完成后核销' : used ? '优惠已核销' : expired ? '已过有效期' : ''}</span>`;
  }
  const footMeta = mode === 'mine'
    ? `<span>领取于 ${couponDate(claim?.claimed_at_ms)}</span>${claim?.used_at_ms ? `<span>使用于 ${couponDate(claim.used_at_ms)}</span>` : ''}`
    : `<span>${couponScope(coupon)}</span><span>有效期至 ${couponDate(coupon?.ends_at_ms)}</span>`;
  return `<article class="${cardClass}">
    <div class="coupon-stub">
      <div class="coupon-value">${isPercent ? esc(value) : `<span class="coupon-yuan">¥</span>${esc(value.replace('¥', ''))}`}${isPercent ? '<small>OFF</small>' : ''}</div>
      <div class="coupon-rule">${esc(couponRule(coupon))}</div>
      <div class="coupon-stub-foot">CITYFLOW 配送券</div>
    </div>
    <div class="coupon-body">
      <div class="coupon-card-top">
        <span class="coupon-type-badge">${isPercent ? '折扣券' : '立减券'}</span>
        ${mode === 'mine' ? `<span class="coupon-status ${esc(status.toLowerCase())}">${esc(statusLabel)}</span>` : `<span class="coupon-stock">剩余 ${remaining} / ${total}</span>`}
      </div>
      <h2>${esc(title)}</h2>
      ${subtitle ? `<p class="coupon-subtitle">${esc(subtitle)}</p>` : ''}
      ${description ? `<p class="coupon-desc">${esc(description)}</p>` : ''}
      <div class="coupon-meta">${footMeta}${mode === 'mine' && Number(claim?.discount_fen) > 0 ? `<span>实省 <em>${esc(couponYuan(claim.discount_fen))}</em></span>` : ''}</div>
      ${mode === 'center' ? `<div class="coupon-progress" aria-hidden="true"><i style="width:${stockPercent}%"></i></div>` : ''}
      <div class="coupon-actions">${action}${mode === 'center' ? `<span class="coupon-stock">每人限领 ${limit} 张</span>` : ''}</div>
    </div>
  </article>`;
}

function renderCenter() {
  const grid = document.querySelector('#couponGrid');
  const items = couponState.items;
  const claimable = items.filter(item => Number(item.user_claim_count || 0) < Number(item.per_user_limit || 1) && Number(item.remaining || 0) > 0);
  const maxSave = items.reduce((max, item) => Math.max(max, Number(item.max_discount_fen || item.value_fen || 0)), 0);
  document.querySelector('#availableCount').textContent = String(claimable.length);
  document.querySelector('#maxSaveCount').textContent = maxSave ? couponYuan(maxSave) : '—';
  document.querySelector('#totalRemaining').textContent = String(items.reduce((sum, item) => sum + Number(item.remaining || 0), 0));
  grid.setAttribute('aria-busy', 'false');
  grid.innerHTML = items.length
    ? items.map(item => couponCard(item, null, 'center')).join('')
    : '<div class="coupon-empty"><strong>暂无可领取的优惠券</strong></div>';
  if (couponState.claims.length >= 0) document.querySelector('#myCouponCount').textContent = String(couponState.claims.filter(item => item.status === 'UNUSED').length);
  const mineTab = document.querySelector('#mineTabCount');
  if (mineTab) {
    mineTab.textContent = String(couponState.claims.length);
    mineTab.hidden = couponState.claims.length === 0;
  }
}

function updateMineSummary() {
  const claims = couponState.claims;
  const counts = { UNUSED: 0, LOCKED: 0, USED: 0, EXPIRED: 0 };
  let saved = 0;
  for (const claim of claims) {
    if (counts[claim.status] !== undefined) counts[claim.status]++;
    if (claim.status === 'USED') saved += Number(claim.discount_fen || 0);
  }
  for (const [id, value] of Object.entries({ unusedCount: counts.UNUSED, usedCount: counts.USED, lockedCount: counts.LOCKED })) {
    const node = document.querySelector(`#${id}`); if (node) node.textContent = String(value);
  }
  const savedNode = document.querySelector('#savedTotal'); if (savedNode) savedNode.textContent = couponYuan(saved);
  const mineTab = document.querySelector('#mineTabCount');
  if (mineTab) { mineTab.textContent = String(claims.length); mineTab.hidden = claims.length === 0; }
}

function renderMine() {
  const grid = document.querySelector('#couponGrid');
  const filtered = couponState.filter ? couponState.claims.filter(item => item.status === couponState.filter) : couponState.claims;
  grid.setAttribute('aria-busy', 'false');
  grid.innerHTML = filtered.length
    ? filtered.map(claim => couponCard(claim.coupon, claim, 'mine')).join('')
    : `<div class="coupon-empty"><strong>${couponState.filter === 'UNUSED' ? '暂无未使用的优惠券' : '暂无领取记录'}</strong><a class="button primary" href="/coupons">前往领券中心</a></div>`;
  updateMineSummary();
}

async function loadCenter() {
  if (couponState.loading) return;
  couponState.loading = true;
  try {
    const result = await api('/api/v1/coupons?limit=50');
    couponState.items = result.coupons || [];
    const mine = await api('/api/v1/coupons/mine?limit=100').catch(() => ({ coupons: [] }));
    couponState.claims = mine.coupons || [];
    renderCenter();
  } catch (error) {
    document.querySelector('#couponGrid').setAttribute('aria-busy', 'false');
    document.querySelector('#couponGrid').innerHTML = '<div class="coupon-empty"><strong>优惠券加载失败</strong><p>请稍后重试。</p><button class="button outline" type="button" data-retry-coupons>重试</button></div>';
  } finally { couponState.loading = false; }
}

async function loadMine() {
  if (couponState.loading) return;
  couponState.loading = true;
  try {
    const result = await api('/api/v1/coupons/mine?limit=100');
    couponState.claims = result.coupons || [];
    renderMine();
  } catch (error) {
    document.querySelector('#couponGrid').setAttribute('aria-busy', 'false');
    document.querySelector('#couponGrid').innerHTML = '<div class="coupon-empty"><strong>优惠券加载失败</strong><p>请稍后重试。</p><button class="button outline" type="button" data-retry-coupons>重试</button></div>';
  } finally { couponState.loading = false; }
}

async function claimCoupon(id, button) {
  if (!id || button.disabled) return;
  const label = button.textContent;
  button.disabled = true; button.textContent = '领取中…';
  hideCouponToast();
  const { key: idempotencyKey, storageKey } = claimRequestKey(id);
  try {
    for (let attempt = 0; attempt < 6; attempt++) {
      const result = await api(`/api/v1/coupons/${encodeURIComponent(id)}/claim`, {
        method: 'POST', body: '{}', headers: { 'Idempotency-Key': idempotencyKey },
      });
      if (result?.status === 'processing') {
        showCouponToast('正在领取…');
        await new Promise(resolve => setTimeout(resolve, 400));
        continue;
      }
      clearClaimRequestKey(storageKey);
      showCouponToast('领取成功，已存入“我的优惠券”。');
      await loadCenter();
      return;
    }
    button.disabled = false; button.textContent = label;
    showCouponToast('领取仍在处理中，可稍后在“我的优惠券”查看结果。');
  } catch (error) {
    button.disabled = false; button.textContent = label;
    showCouponToast(error.message, 'error');
  }
}

function claimRequestKey(id) {
  const storageKey = `coupon-claim:${id}`;
  try {
    const existing = sessionStorage.getItem(storageKey);
    if (existing) return { key: existing, storageKey };
    const key = newIdempotencyKey();
    sessionStorage.setItem(storageKey, key);
    return { key, storageKey };
  } catch (_) {
    return { key: newIdempotencyKey(), storageKey: '' };
  }
}

function clearClaimRequestKey(storageKey) {
  if (!storageKey) return;
  try { sessionStorage.removeItem(storageKey); } catch (_) { /* ignore */ }
}

function newIdempotencyKey() {
  if (window.crypto?.randomUUID) return window.crypto.randomUUID();
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 12)}`;
}

async function redeemCoupon(event) {
  event.preventDefault();
  const form = event.currentTarget;
  const input = form.querySelector('#redeemCode');
  const button = form.querySelector('button[type="submit"]');
  const code = (input?.value || '').trim();
  if (!code) {
    showCouponToast('请输入兑换码。', 'error');
    input?.focus();
    return;
  }
  const label = button.textContent;
  button.disabled = true;
  button.textContent = '兑换中…';
  hideCouponToast();
  try {
    const result = await api('/api/v1/coupons/redeem', {
      method: 'POST', body: JSON.stringify({ code }),
    });
    const title = result?.coupon?.coupon?.title || '优惠券';
    showCouponToast(`兑换成功：${title} 已放入「我的优惠券」。`);
    if (input) input.value = '';
    if (isMinePage) await loadMine(); else await loadCenter();
  } catch (error) {
    showCouponToast(error.message, 'error');
  } finally {
    button.disabled = false;
    button.textContent = label;
  }
}

document.addEventListener('click', async event => {
  const claimButton = event.target.closest('[data-claim-coupon]');
  if (claimButton) { event.preventDefault(); await claimCoupon(claimButton.dataset.claimCoupon, claimButton); return; }
  const retry = event.target.closest('[data-retry-coupons]');
  if (retry) { event.preventDefault(); if (isMinePage) await loadMine(); else await loadCenter(); return; }
  const filter = event.target.closest('[data-status]');
  if (filter && isMinePage) {
    event.preventDefault();
    couponState.filter = filter.dataset.status || '';
    document.querySelectorAll('[data-status]').forEach(item => item.setAttribute('aria-pressed', String(item === filter)));
    renderMine();
  }
});

(async () => {
  const account = await requirePageRole('USER');
  if (!account) return;
  couponState.account = account;
  document.querySelector('#redeemForm')?.addEventListener('submit', redeemCoupon);
  if (isMinePage) await loadMine(); else await loadCenter();
})();
