(async () => {
  const user = await requirePageRole('USER');
  if (!user) return;
  const $ = selector => document.querySelector(selector);
  const notice = (text, kind = '') => {
    const el = $('#profileNotice');
    el.textContent = text || '';
    el.classList.toggle('success', kind === 'success');
  };
  const riderID = decodeURIComponent(location.pathname.split('/').filter(Boolean).pop() || '');
  const taskID = new URLSearchParams(location.search).get('task_id') || '';
  if (!riderID) { notice('未找到骑手资料。'); return; }
  $('#profileWelcome').textContent = user.name || '';
  let rider;
  const time = value => value ? new Date(value).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }) : '—';
  const stars = value => '★★★★★'.slice(0, Math.max(1, Math.min(5, Math.round(Number(value) || 0)))) + '☆☆☆☆☆'.slice(0, 5 - Math.max(1, Math.min(5, Math.round(Number(value) || 0))));

  function render() {
    $('#profileName').textContent = rider.display_name || '骑手';
    $('#profileAvatar').innerHTML = rider.avatar_url ? `<img src="${esc(rider.avatar_url)}" alt="${esc(rider.display_name)}的头像">` : esc((rider.display_name || '骑').slice(0, 1));
    $('#profileRating').textContent = `★ ${(Number(rider.rating) || 0).toFixed(1)}`;
    $('#profileReviewCount').textContent = `${Number(rider.review_count) || 0} 条评价`;
    $('#profileCompleted').textContent = `已完成 ${Number(rider.completed_tasks) || 0} 单`;
    const skills = (rider.skills || []).map(skill => ({ DELIVERY: '配送', PICKUP: '代取', PURCHASE: '代购', OTHER: '其他' })[skill] || skill);
    $('#profileTags').innerHTML = [
      ({ WALK: '步行', BICYCLE: '自行车', E_BIKE: '电动车', CAR: '汽车', OTHER: '其他' })[rider.vehicle_type] || rider.vehicle_type,
      rider.service_area,
      ...skills,
    ].filter(Boolean).map(tag => `<span>${esc(tag)}</span>`).join('');
    const reviews = rider.reviews || [];
    $('#profileReviews').innerHTML = reviews.length ? reviews.map(review => `<article class="review-item"><div class="review-item-head"><span>${esc(review.author_label || '匿名用户')} · ${esc(stars(review.rating))}</span><time>${esc(review.date || '')}</time></div><p>${esc(review.comment || '未填写评价内容。')}</p><div class="review-actions"><button type="button" data-rider-review-like="${esc(review.id)}" data-liked="${Boolean(review.liked)}" aria-pressed="${Boolean(review.liked)}">${review.liked ? '已赞' : '赞'} ${Number(review.likes) || 0}</button><button type="button" data-rider-comments="${esc(review.id)}">评论</button></div><div class="review-comments" data-rider-comments-panel="${esc(review.id)}" hidden></div></article>`).join('') : '<p class="muted">暂无评价。</p>';
    $('#profileTipTotal').textContent = rider.tip_total_fen ? `¥${(Number(rider.tip_total_fen) / 100).toFixed(2)}` : '¥0.00';
    $('#profileTips').innerHTML = (rider.tips || []).length ? rider.tips.map(tip => `<div class="tip-row"><span>${esc(tip.author_label || '匿名用户')} · ${esc(tip.comment || '用户打赏')}</span><strong>¥${(Number(tip.amount_fen) / 100).toFixed(2)}</strong><time>${esc(tip.date || '')}</time></div>`).join('') : '<p class="muted">暂无打赏记录。</p>';
    const report = $('#profileReport');
    report.disabled = !taskID;
    report.textContent = '举报骑手';
  }

  function riderCommentItems(items, feedbackID) {
    return items.map(item => `<div class="comment-item" data-comment="${esc(item.id)}"><div class="comment-head"><strong>${esc(item.author_label || '匿名用户')}</strong>${item.reply_to_label ? `<span>回复 ${esc(item.reply_to_label)}</span>` : ''}<time>${esc(item.date || '')}</time></div><p>${esc(item.content || '')}</p><div class="comment-actions"><button type="button" data-rider-comment-like="${esc(item.id)}" data-liked="${Boolean(item.liked)}" aria-pressed="${Boolean(item.liked)}">${item.liked ? '已赞' : '赞'} ${Number(item.likes) || 0}</button>${item.depth < 3 ? `<button type="button" data-rider-reply="${esc(item.id)}">回复</button>` : ''}${item.depth === 1 && item.reply_count ? `<button type="button" data-rider-thread="${esc(item.id)}" data-feedback="${esc(feedbackID)}">查看 ${item.reply_count} 条回复</button>` : ''}</div><form class="comment-form" data-rider-comment-form data-feedback="${esc(feedbackID)}" data-parent="${esc(item.id)}" hidden><textarea name="content" maxlength="500" rows="2" placeholder="回复 ${esc(item.author_label || '匿名用户')}" required></textarea><button type="submit">发送回复</button></form></div>`).join('') || '<p class="muted">暂无评论。</p>';
  }

  async function loadRiderComments(feedbackID, panel, rootID) {
    panel.hidden = false;
    panel.dataset.loaded = '1';
    panel.dataset.feedback = feedbackID;
    panel.dataset.root = rootID || '';
    panel.innerHTML = '<p class="muted">正在加载评论…</p>';
    try {
      const url = rootID
        ? `/api/v1/rider-feedback/${encodeURIComponent(feedbackID)}/comments/${encodeURIComponent(rootID)}/thread`
        : `/api/v1/rider-feedback/${encodeURIComponent(feedbackID)}/comments`;
      const data = await api(url);
      const items = data.items || [];
      panel.innerHTML = (rootID ? '<button type="button" data-rider-back-thread>返回全部评论</button>' : '') + riderCommentItems(items, feedbackID) + (rootID ? '' : `<form class="comment-form" data-rider-comment-form data-feedback="${esc(feedbackID)}"><textarea name="content" maxlength="500" rows="2" placeholder="写下你的评论" required></textarea><button type="submit">发表评论</button></form>`);
    } catch (error) {
      panel.innerHTML = '<p class="muted">评论加载失败，请稍后重试。</p>';
    }
  }

  async function load() {
    try {
      rider = await api(`/api/v1/rider-profiles/${encodeURIComponent(riderID)}`);
      render();
      notice('');
    } catch (error) { notice('骑手资料加载失败，请稍后重试。'); }
  }

  $('#profileReviews').addEventListener('click', async event => {
    const reviewLike = event.target.closest('[data-rider-review-like]');
    const comments = event.target.closest('[data-rider-comments]');
    const thread = event.target.closest('[data-rider-thread]');
    const reply = event.target.closest('[data-rider-reply]');
    const back = event.target.closest('[data-rider-back-thread]');
    const like = event.target.closest('[data-rider-comment-like]');
    if (reviewLike) {
      const button = reviewLike;
      button.disabled = true;
      try {
        const result = await api('/api/v1/reactions', {
          method: 'POST',
          body: JSON.stringify({ target_type: 'RIDER_REVIEW', target_id: button.dataset.riderReviewLike, kind: 'LIKE', active: button.dataset.liked !== 'true' }),
        });
        button.dataset.liked = String(Boolean(result.active));
        button.setAttribute('aria-pressed', String(Boolean(result.active)));
        button.textContent = `${result.active ? '已赞' : '赞'} ${Number(result.count) || 0}`;
      } catch (error) { notice('点赞失败，请稍后重试。'); }
      finally { button.disabled = false; }
      return;
    }
    if (comments) {
      const panel = comments.closest('.review-item')?.querySelector('[data-rider-comments-panel]');
      if (!panel) return;
      if (panel.dataset.loaded) panel.hidden = !panel.hidden;
      else await loadRiderComments(comments.dataset.riderComments, panel);
      return;
    }
    if (thread) {
      const panel = thread.closest('[data-rider-comments-panel]');
      if (panel && thread.dataset.riderThread) await loadRiderComments(thread.dataset.feedback, panel, thread.dataset.riderThread);
      return;
    }
    if (reply) {
      const form = reply.closest('.comment-item')?.querySelector('[data-rider-comment-form]');
      if (form) form.hidden = false;
      return;
    }
    if (back) {
      const panel = back.closest('[data-rider-comments-panel]');
      if (panel && panel.dataset.feedback) await loadRiderComments(panel.dataset.feedback, panel);
      return;
    }
    if (like) {
      const button = like;
      button.disabled = true;
      try {
        const result = await api('/api/v1/reactions', {
          method: 'POST',
          body: JSON.stringify({ target_type: 'RIDER_COMMENT', target_id: button.dataset.riderCommentLike, kind: 'LIKE', active: button.dataset.liked !== 'true' }),
        });
        button.dataset.liked = String(Boolean(result.active));
        button.setAttribute('aria-pressed', String(Boolean(result.active)));
        button.textContent = `${result.active ? '已赞' : '赞'} ${Number(result.count) || 0}`;
      } catch (error) { notice('点赞失败，请稍后重试。'); }
      finally { button.disabled = false; }
    }
  });

  $('#profileReviews').addEventListener('submit', async event => {
    const form = event.target.closest('[data-rider-comment-form]');
    if (!form) return;
    event.preventDefault();
    const button = form.querySelector('button[type="submit"]');
    button.disabled = true;
    try {
      await api(`/api/v1/rider-feedback/${encodeURIComponent(form.dataset.feedback)}/comments`, {
        method: 'POST',
        body: JSON.stringify({ content: form.elements.content.value.trim(), parent_id: form.dataset.parent || '' }),
      });
      const panel = form.closest('[data-rider-comments-panel]');
      if (panel) await loadRiderComments(form.dataset.feedback, panel, panel.dataset.root || '');
    } catch (error) { notice('评论提交失败，请稍后重试。'); button.disabled = false; }
  });

  $('#profileReport').addEventListener('click', () => {
    if (!taskID) return;
    $('#profileReportDialog').showModal();
  });
  document.querySelectorAll('[data-close-profile-report]').forEach(button => button.addEventListener('click', () => $('#profileReportDialog').close()));
  $('#profileReportImages').addEventListener('change', async event => {
    const preview = $('#profileReportPreview');
    preview.replaceChildren();
    for (const file of [...(event.currentTarget.files || [])].slice(0, 6)) {
      if (!file.type.startsWith('image/')) continue;
      const data = await readFile(file);
      const image = document.createElement('img'); image.src = data; image.alt = file.name; preview.append(image);
    }
    if (!preview.children.length) preview.innerHTML = '<span class="muted">暂无图片。</span>';
  });
  $('#profileReportForm').addEventListener('submit', async event => {
    event.preventDefault();
    const form = event.currentTarget;
    const button = form.querySelector('button[type="submit"]');
    const status = $('#profileReportStatus');
    const files = [...($('#profileReportImages').files || [])].slice(0, 6);
    button.disabled = true; status.style.color = ''; status.textContent = '正在提交举报…';
    try {
      const images = [];
      for (const file of files) {
        if (!file.type.startsWith('image/')) continue;
        if (file.size > 3 * 1024 * 1024) throw new Error(`图片 ${file.name} 超过 3MB。`);
        images.push(await readFile(file));
      }
      const body = new FormData(form);
      await api(`/api/v1/tasks/${encodeURIComponent(taskID)}/rider/reports`, { method: 'POST', body: JSON.stringify({ reason: String(body.get('reason') || ''), details: String(body.get('details') || ''), images }) });
      status.style.color = '#267151'; status.textContent = '举报已提交。';
      setTimeout(() => $('#profileReportDialog').close(), 900);
    } catch (error) { status.style.color = '#a04b3e'; status.textContent = error.message; }
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

  load();
})();
