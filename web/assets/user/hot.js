/* Monthly merchant ranking: large merchant cards with avatar, story and first review. */
(() => {
  'use strict';

  const root = document.querySelector('#hotWorkspace');
  if (!root) return;

  const stage = root.querySelector('#hotStage');
  const statusEl = root.querySelector('#hotStatus');
  const noticeEl = root.querySelector('#hotNotice');
  const categorySelect = root.querySelector('#hotCategory');
  const monthSelect = root.querySelector('#hotMonth');
  const boardCount = root.querySelector('#hotBoardCount');
  const refreshButton = root.querySelector('#hotRefresh');
  const monthLabel = root.querySelector('#hotMonthLabel');
  const metricLikes = root.querySelector('#hotMetricLikes');
  const metricShops = root.querySelector('#hotMetricShops');
  const metricTop = root.querySelector('#hotMetricTop');
  const heroMonth = root.querySelector('#hotHeroMonth');
  const heroYear = root.querySelector('#hotHeroYear');

  const categories = {
    FOOD: '饮食',
    SUPPLIES: '用品',
    OTHER: '其它',
  };

  const state = {
    category: 'FOOD',
    month: '',
    data: null,
    controller: null,
    started: false,
    busy: false,
    lastLoaded: 0,
    pendingLikes: new Set(),
  };

  let noticeTimer = 0;
  const pad = value => String(value).padStart(2, '0');
  const monthNow = () => {
    const now = new Date();
    return `${now.getFullYear()}${pad(now.getMonth() + 1)}`;
  };
  const isCurrentMonth = value => String(value || '') === monthNow();
  const monthKeys = (count = 12) => {
    const today = new Date();
    const out = [];
    for (let index = 0; index < count; index += 1) {
      const date = new Date(today.getFullYear(), today.getMonth() - index, 1);
      out.push(`${date.getFullYear()}${pad(date.getMonth() + 1)}`);
    }
    return out;
  };
  function fillMonthOptions() {
    if (!monthSelect) return;
    monthSelect.innerHTML = monthKeys().map(value => {
      const suffix = isCurrentMonth(value) ? ' · 本月' : '';
      return `<option value="${value}">${monthText(value)}${suffix}</option>`;
    }).join('');
    monthSelect.value = state.month;
  }
  const categoryName = value => categories[value] || '门店';
  const numberValue = value => {
    const parsed = Number(value);
    return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
  };

  function monthText(value) {
    const match = String(value || '').match(/^(\d{4})[-/]?(\d{2})$/);
    if (!match) return '本月';
    return `${match[1]}年${Number(match[2])}月`;
  }

  function setMonth(value) {
    const match = String(value || '').match(/^(\d{4})[-/]?(\d{2})$/);
    const current = isCurrentMonth(value);
    monthLabel.textContent = `${monthText(value)} · ${current ? '本月统计' : '历史榜单'}`;
    heroMonth.textContent = match ? match[2] : pad(new Date().getMonth() + 1);
    heroYear.textContent = match ? match[1] : String(new Date().getFullYear());
    if (monthSelect && match) monthSelect.value = `${match[1]}${match[2]}`;
  }

  function formatCount(value) {
    const count = numberValue(value);
    if (count >= 10000) {
      const wan = count / 10000;
      return `${wan.toFixed(wan >= 10 ? 0 : 1).replace(/\.0$/, '')}万`;
    }
    return count.toLocaleString('zh-CN');
  }

  function monogram(name) {
    return String(name || '店').trim().slice(0, 1).toUpperCase() || '店';
  }

  function showHotNotice(message, tone = 'success') {
    if (!message) {
      noticeEl.textContent = '';
      noticeEl.hidden = true;
      clearTimeout(noticeTimer);
      return;
    }
    noticeEl.textContent = message;
    noticeEl.hidden = false;
    noticeEl.classList.toggle('is-error', tone === 'error');
    clearTimeout(noticeTimer);
    noticeTimer = setTimeout(() => {
      noticeEl.hidden = true;
      noticeEl.textContent = '';
    }, 4200);
  }

  function resetMetrics() {
    metricLikes.textContent = '—';
    metricShops.textContent = '—';
    metricTop.textContent = '—';
    boardCount.textContent = '共 — 家';
  }

  function skeleton() {
    stage.innerHTML = `
      <div class="hot-skeleton" aria-hidden="true">
        <div class="hot-skeleton-head"><span></span><span></span></div>
        <div class="hot-skeleton-card"><i></i><div><b></b><b></b><b></b></div><em></em></div>
        <div class="hot-skeleton-card"><i></i><div><b></b><b></b><b></b></div><em></em></div>
        <div class="hot-skeleton-card"><i></i><div><b></b><b></b><b></b></div><em></em></div>
      </div>
      <p class="hot-loading-copy"><span class="hot-loading-ring" aria-hidden="true"></span>正在加载本月热店榜…</p>`;
  }

  function avatarMarkup(item) {
    const image = item.avatar_url
      ? `<img src="${esc(item.avatar_url)}" alt="${esc(item.name)}门店头像" loading="lazy" decoding="async" referrerpolicy="no-referrer">`
      : '';
    return `<span class="hot-avatar${item.avatar_url ? '' : ' is-fallback'}" data-hot-avatar>
      ${image}
      <span class="hot-avatar-fallback hot-avatar-${esc(item.category)}"${item.avatar_url ? ' hidden' : ''}>
        <strong>${esc(monogram(item.name))}</strong>
      </span>
    </span>`;
  }

  function ratingLine(item) {
    if (!numberValue(item.review_count)) return '暂无评价';
    return `★ ${Number(item.rating || 0).toFixed(1)} · ${numberValue(item.review_count)} 条评价`;
  }

  function reviewMarkup(item) {
    const review = item.top_review;
    if (!review) {
      return `<div class="hot-review is-empty">
        <div class="hot-review-head"><span>精选评价</span></div>
        <p>暂无评价。</p>
      </div>`;
    }
    const comment = String(review.comment || '').trim() || '未填写评价内容。';
    return `<div class="hot-review">
      <div class="hot-review-head">
        <span>精选评价</span>
        <small>${'★'.repeat(Math.max(0, Math.min(5, Number(review.rating) || 0)))}<b>${Number(review.rating) || 0}.0</b></small>
      </div>
      <blockquote>“${esc(comment)}”</blockquote>
      <div class="hot-review-meta">
        <span>${esc(review.author_label || 'CityFlow 用户')}</span>
        <time>${esc(review.date || '')}</time>
        <em>${numberValue(review.likes)} 人赞过</em>
      </div>
    </div>`;
  }

  function likeButton(item) {
    const liked = Boolean(item.liked);
    return `<button type="button" class="hot-like${liked ? ' is-liked' : ''}" data-hot-like="${esc(item.merchant_id)}" data-liked="${liked}" aria-pressed="${liked}" aria-label="${liked ? '取消点赞' : '点赞'} ${esc(item.name)}">
      <span class="hot-like-mark" data-like-mark aria-hidden="true">${liked ? '♥' : '♡'}</span>
      <span class="hot-like-text" data-like-text>${liked ? '已赞' : '点赞'}</span>
    </button>`;
  }

  function merchantCard(item, index, historical) {
    const rank = Number(item.rank) || index + 1;
    const open = String(item.business_status || 'OPEN').toUpperCase() === 'OPEN';
    return `<article class="hot-card hot-rank-${rank}" data-hot-merchant="${esc(item.merchant_id)}" style="--i:${index}">
      <div class="hot-card-rank">
        <strong>${pad(rank)}</strong>
        <i aria-hidden="true"></i>
      </div>
      ${avatarMarkup(item)}
      <div class="hot-card-identity">
        <div class="hot-card-tags">
          <span class="hot-category">${esc(categoryName(item.category))}</span>
          <span class="hot-open${open ? '' : ' is-closed'}"><i aria-hidden="true"></i>${open ? '营业中' : '暂停营业'}</span>
          <span class="hot-rating">${esc(ratingLine(item))}</span>
        </div>
        <h3>${esc(item.name)}</h3>
        <p class="hot-card-address">${esc(item.address || '—')}</p>
      </div>
      <div class="hot-card-body">
        <p class="hot-card-desc">${esc(item.description || '暂无门店介绍。')}</p>
        ${reviewMarkup(item)}
      </div>
      <aside class="hot-card-side">
        <div class="hot-score"><strong data-hot-score>${formatCount(item.like_count)}</strong><small>${historical ? '当月获赞' : '本月获赞'}</small></div>
        ${historical ? '<span class="hot-history-chip">历史榜单</span>' : likeButton(item)}
        <button type="button" class="hot-open-card" data-hot-open>查看门店</button>
      </aside>
    </article>`;
  }

  function mapCta(label = '浏览地图') {
    return `<button type="button" class="hot-map-cta" data-hot-map>
      <span class="hot-map-cta-copy"><strong>${label}</strong></span>
      <span class="hot-map-cta-arrow" aria-hidden="true">→</span>
    </button>`;
  }

  function board(data, items) {
    const month = data.month;
    const historical = !isCurrentMonth(month);
    const period = historical ? monthText(month) : '本月';
    return `<section class="hot-board" aria-labelledby="hotBoardTitle">
      <header class="hot-board-head">
        <div>
          <h2 id="hotBoardTitle">${esc(period)}${esc(categoryName(data.category))}热店</h2>
        </div>
        <div class="hot-board-summary">
          <strong>${items.length}</strong>
          <span>家门店</span>
        </div>
      </header>
      <div class="hot-card-list">
        ${items.map((item, index) => merchantCard(item, index, historical)).join('')}
      </div>
      <footer class="hot-board-foot">
        ${mapCta()}
      </footer>
    </section>`;
  }

  function emptyState(data) {
    const historical = !isCurrentMonth(data.month);
    const period = historical ? monthText(data.month) : '本月';
    return `<div class="hot-empty">
      <div class="hot-empty-main">
        <div class="hot-empty-art" aria-hidden="true">
          <span>00</span>
          <i></i>
        </div>
        <div class="hot-empty-copy">
          <h2>${esc(period)}${esc(categoryName(data.category))}热店榜暂无上榜门店。</h2>
          ${historical ? '' : mapCta()}
        </div>
      </div>
    </div>`;
  }

  function errorState(error) {
    return `<div class="hot-error" role="alert">
      <div class="hot-error-mark" aria-hidden="true">!</div>
      <div>
        <h2>热店榜暂不可用</h2>
        <p>请稍后重试。</p>
        <button type="button" class="button outline" data-hot-retry>重试</button>
      </div>
    </div>`;
  }

  function updateMetrics() {
    const items = state.data?.items || [];
    const total = items.reduce((sum, item) => sum + numberValue(item.like_count), 0);
    metricLikes.textContent = formatCount(total);
    metricShops.textContent = String(items.length);
    metricTop.textContent = items.length ? items[0].name : '—';
    boardCount.textContent = `共 ${items.length} 家`;
  }

  function wireAvatarFallbacks() {
    root.querySelectorAll('[data-hot-avatar] img').forEach(image => {
      image.addEventListener('error', () => {
        image.hidden = true;
        const fallback = image.parentElement?.querySelector('.hot-avatar-fallback');
        if (fallback) fallback.hidden = false;
      }, { once: true });
    });
  }

  function render(data) {
    const items = data.items || [];
    setMonth(data.month);
    categorySelect.value = data.category;
    updateMetrics();
    stage.innerHTML = items.length ? board(data, items) : emptyState(data);
    wireAvatarFallbacks();
    statusEl.textContent = items.length
      ? `${monthText(data.month)}，${categoryName(data.category)}热店榜共 ${items.length} 家门店，榜首 ${items[0].name}。`
      : `${monthText(data.month)}，${categoryName(data.category)}热店榜暂无门店上榜。`;
  }

  function normalizeReview(value) {
    if (!value || typeof value !== 'object') return null;
    return {
      id: String(value.id || ''),
      author_label: String(value.author_label || ''),
      anonymous: Boolean(value.anonymous),
      rating: numberValue(value.rating),
      comment: String(value.comment || ''),
      likes: numberValue(value.likes),
      date: String(value.date || ''),
    };
  }

  function normalize(payload) {
    const items = (payload.items || [])
      .filter(item => item && item.merchant_id)
      .map((item, index) => ({
        ...item,
        rank: numberValue(item.rank) || index + 1,
        like_count: numberValue(item.like_count),
        category: String(item.category || state.category).toUpperCase(),
        avatar_url: String(item.avatar_url || ''),
        description: String(item.description || ''),
        review_count: numberValue(item.review_count),
        rating: Number(item.rating) || 0,
        top_review: normalizeReview(item.top_review),
        liked: false,
      }));
    return {
      month: String(payload.month || state.month || monthNow()),
      category: String(payload.category || state.category).toUpperCase(),
      items,
    };
  }

  async function hydrateLiked(items, signal) {
    if (!items.length || typeof getToken !== 'function' || !getToken()) return;
    const params = new URLSearchParams({
      target_type: 'MERCHANT',
      kind: 'LIKE',
      target_ids: items.map(item => item.merchant_id).join(','),
    });
    try {
      const payload = await api(`/api/v1/reactions/state?${params}`, { signal });
      const states = new Map((payload.items || []).map(item => [String(item.target_id), Boolean(item.liked)]));
      items.forEach(item => {
        if (states.has(item.merchant_id)) item.liked = states.get(item.merchant_id);
      });
    } catch (error) {
      if (error.name !== 'AbortError') console.warn('merchant like state:', error);
    }
  }

  async function load({ quiet = false } = {}) {
    if (state.controller) state.controller.abort();
    const controller = new AbortController();
    state.controller = controller;
    state.busy = true;
    stage.setAttribute('aria-busy', 'true');
    refreshButton.disabled = true;
    refreshButton.classList.add('is-spinning');
    showHotNotice('');
    if (!quiet) {
      resetMetrics();
      skeleton();
    }
    statusEl.textContent = `正在加载${categoryName(state.category)}热店榜…`;

    try {
      const query = new URLSearchParams({ category: state.category, month: state.month, limit: '20' });
      const payload = await api(`/api/v1/merchants/hot?${query}`, { signal: controller.signal });
      if (controller.signal.aborted) return;
      const data = normalize(payload);
      if (isCurrentMonth(data.month)) await hydrateLiked(data.items, controller.signal);
      if (controller.signal.aborted) return;
      state.data = data;
      state.lastLoaded = Date.now();
      render(state.data);
    } catch (error) {
      if (controller.signal.aborted || error.name === 'AbortError') return;
      state.data = null;
      resetMetrics();
      setMonth(state.month);
      stage.innerHTML = errorState(error);
      statusEl.textContent = '热店榜加载失败，请稍后重试。';
    } finally {
      if (!controller.signal.aborted) {
        state.busy = false;
        stage.setAttribute('aria-busy', 'false');
        refreshButton.disabled = false;
        refreshButton.classList.remove('is-spinning');
      }
      if (state.controller === controller) state.controller = null;
    }
  }

  function patchCardLike(item, button) {
    const card = button.closest('.hot-card');
    if (!card) return;
    const score = card.querySelector('[data-hot-score]');
    if (score) score.textContent = formatCount(item.like_count);
    button.dataset.liked = String(Boolean(item.liked));
    button.setAttribute('aria-pressed', String(Boolean(item.liked)));
    button.classList.toggle('is-liked', Boolean(item.liked));
    button.setAttribute('aria-label', `${item.liked ? '取消点赞' : '点赞'} ${item.name}`);
    const mark = button.querySelector('[data-like-mark]');
    const text = button.querySelector('[data-like-text]');
    if (mark) mark.textContent = item.liked ? '♥' : '♡';
    if (text) text.textContent = item.liked ? '已赞' : '点赞';
  }

  async function toggleLike(button) {
    if (!isCurrentMonth(state.month)) return;
    const id = button.dataset.hotLike;
    const item = state.data?.items.find(entry => entry.merchant_id === id);
    if (!id || !item || state.pendingLikes.has(id)) return;
    const next = !item.liked;
    state.pendingLikes.add(id);
    button.disabled = true;
    button.setAttribute('aria-busy', 'true');
    try {
      const result = await api(`/api/v1/merchants/${encodeURIComponent(id)}/likes`, {
        method: 'POST',
        body: JSON.stringify({ active: next }),
      });
      item.liked = Boolean(result.active);
      item.like_count = numberValue(result.count);
      patchCardLike(item, button);
      updateMetrics();
      state.lastLoaded = 0;
      const message = item.liked
        ? `已为「${item.name}」点赞，本月累计 ${numberValue(result.count)} 个赞。`
        : `已取消对「${item.name}」的点赞，本月剩 ${numberValue(result.count)} 个赞。`;
      showHotNotice(message);
      statusEl.textContent = message;
    } catch (error) {
      showHotNotice('点赞失败，请稍后重试。', 'error');
      statusEl.textContent = '点赞失败，请稍后重试。';
    } finally {
      state.pendingLikes.delete(id);
      if (button.isConnected) {
        button.disabled = false;
        button.removeAttribute('aria-busy');
      }
    }
  }

  categorySelect.addEventListener('change', () => {
    if (!categories[categorySelect.value]) return;
    state.category = categorySelect.value;
    load();
  });

  monthSelect?.addEventListener('change', () => {
    if (!/^\d{6}$/.test(monthSelect.value)) return;
    state.month = monthSelect.value;
    load();
  });

  stage.addEventListener('click', async event => {
    const like = event.target.closest('[data-hot-like]');
    if (like) {
      event.preventDefault();
      event.stopPropagation();
      await toggleLike(like);
      return;
    }
    if (event.target.closest('[data-hot-map]')) {
      if (window.CityFlowSetUserMode) window.CityFlowSetUserMode('map');
      else document.querySelector('#browseMode')?.click();
      return;
    }
    if (event.target.closest('[data-hot-retry]')) {
      load();
      return;
    }
    const card = event.target.closest('[data-hot-merchant]');
    if (card) {
      const id = card.dataset.hotMerchant;
      if (id && window.CityFlowBrowsePlace) {
        Promise.resolve(window.CityFlowBrowsePlace('merchant', id)).catch(() => {
          window.CityFlowSetUserMode?.('map');
        });
      }
    }
  });

  refreshButton.addEventListener('click', () => load());

  window.CityFlowHot = {
    activate() {
      if (!state.started) {
        state.started = true;
        load();
        return;
      }
      if (!state.busy && Date.now() - state.lastLoaded > 60000) load({ quiet: true });
    },
  };

  state.month = monthNow();
  fillMonthOptions();
  setMonth(state.month);
  resetMetrics();
})();
