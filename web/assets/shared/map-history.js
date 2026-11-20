/* Road geometry matches rider observations; it is not a newly planned route. */
window.CityFlowHistory = ({ host, draw, clear, play }) => {
  const panel = document.createElement('div'); panel.className = 'map-history';
  panel.innerHTML = '<label>骑手历史轨迹<select aria-label="选择历史轨迹任务"><option value="">选择任务</option></select></label><button class="button outline" type="button" data-history-load>查看轨迹</button><button class="button outline" type="button" data-history-play hidden>轨迹回放</button><button class="button outline" type="button" data-history-more hidden>加载后续轨迹</button><button class="button outline" type="button" data-history-clear>清除轨迹</button><p role="status" aria-live="polite"></p>';
  host.before(panel);
  const select = panel.querySelector('select'), load = panel.querySelector('[data-history-load]'), playButton = panel.querySelector('[data-history-play]'), more = panel.querySelector('[data-history-more]'), status = panel.querySelector('p'), clearButton = panel.querySelector('[data-history-clear]');
  let generation = 0, controller, cursor = 0, segments = [], points = [], destroyed = false, busy = false, autoTried = false, playing = false, stopPlayback = null, tasksById = new Map();
  const viewer = (() => {
    try {
      const claims = tokenClaims(getToken()) || {};
      return { ...claims, role: String(claims.role || '').toUpperCase() };
    } catch { return {}; }
  })();
  const sources = new Set();

  const invalidate = () => {
    generation++; controller?.abort(); busy = false; cursor = 0; segments = []; points = [];
    playing = false; stopPlayback = null; playButton.hidden = true; playButton.textContent = '轨迹回放';
    sources.clear(); more.hidden = true; load.disabled = false; status.textContent = ''; clear();
  };

  const EARTH_RADIUS = 6371000;
  function distanceBetween(a, b) {
    if (!a || !b) return 0;
    const toRadians = value => value * Math.PI / 180;
    const latitude1 = toRadians(a.latitude), latitude2 = toRadians(b.latitude);
    const latitudeDelta = toRadians(b.latitude - a.latitude), longitudeDelta = toRadians(b.longitude - a.longitude);
    const h = Math.sin(latitudeDelta / 2) ** 2 + Math.cos(latitude1) * Math.cos(latitude2) * Math.sin(longitudeDelta / 2) ** 2;
    return 2 * EARTH_RADIUS * Math.asin(Math.min(1, Math.sqrt(h)));
  }

  function summarize() {
    let distance = 0;
    for (const segment of segments) {
      const coordinates = segment.coordinates || [];
      for (let index = 1; index < coordinates.length; index++) distance += distanceBetween(coordinates[index - 1], coordinates[index]);
    }
    let start = points[0], end = points[points.length - 1], earliest = Infinity, latest = -Infinity;
    for (const point of points) {
      const observed = Date.parse(point.observed_at);
      if (observed < earliest) { earliest = observed; start = point; }
      if (observed > latest) { latest = observed; end = point; }
    }
    const duration = Number.isFinite(earliest) && Number.isFinite(latest) ? Math.max(0, latest - earliest) : 0;
    return { distance, duration, matched: segments.filter(segment => segment.mode === 'road_matched').length, start, end };
  }

  function formatDistance(meters) {
    if (meters >= 1000) return `${(meters / 1000).toFixed(meters >= 10000 ? 1 : 2)} 公里`;
    return `${Math.round(meters)} 米`;
  }

  function formatDuration(milliseconds) {
    const minutes = Math.round(milliseconds / 60000);
    if (minutes < 1) return '不足 1 分钟';
    if (minutes < 60) return `${minutes} 分钟`;
    return `${Math.floor(minutes / 60)} 小时 ${minutes % 60} 分钟`;
  }

  function historyStatus(task, summary, body) {
    if (!points.length) {
      if (task && !task.worker_id) return '该任务还没有骑手接单，因此没有骑手轨迹。骑手接单并上报位置后即可查看。';
      return '该任务还没有位置记录；骑手开始共享位置并移动后，这里会显示轨迹。';
    }
    const parts = [`已读取 ${points.length} 个定位点`];
    if (summary.distance >= 1) parts.push(`里程 ${formatDistance(summary.distance)}`);
    if (summary.duration) parts.push(`用时 ${formatDuration(summary.duration)}`);
    parts.push(summary.matched ? `道路匹配 ${summary.matched} 段` : '按原始定位点绘制');
    if (!segments.length) parts.push('服务未返回可绘制线段');
    if (sources.has('simulated')) parts.push('包含模拟位置');
    if (sources.has('unknown')) parts.push('部分旧记录来源未知');
    if (body.display?.gaps) parts.push(`断点 ${body.display.gaps} 处`);
    if (body.display?.rejected_points) parts.push(`过滤异常点 ${body.display.rejected_points} 个`);
    if (body.display?.notice) parts.push(body.display.notice);
    if (body.has_more) parts.push('可加载后续记录');
    if (body.display?.attribution) parts.push(body.display.attribution);
    return parts.join(' · ');
  }

  const query = async append => {
    if (busy || !select.value) { if (!select.value) status.textContent = '请先选择任务。'; return; }
    if (!append) invalidate();
    busy = true; const current = generation, id = select.value;
    controller = new AbortController();
    load.disabled = true; more.disabled = true;
    status.textContent = '正在读取骑手定位记录并匹配骑行道路…';
    try {
      const body = await api(`/api/v1/tasks/${encodeURIComponent(id)}/history?limit=500&after=${cursor}&display=true`, { signal: controller.signal });
      if (destroyed || generation !== current || select.value !== id) return;
      const incomingPoints = body.data || [];
      points.push(...incomingPoints);
      incomingPoints.forEach(point => sources.add(point.source || 'unknown'));
      cursor = body.next_cursor;
      const incoming = body.display ? body.display.segments : (incomingPoints.length ? [{ coordinates: incomingPoints.map(point => ({ latitude: point.latitude, longitude: point.longitude })), mode: 'observed' }] : []);
      if (incoming?.length) segments.push(...incoming);
      const summary = summarize();
      draw(segments, { points, start: summary.start, end: summary.end, taskTitle: tasksById.get(id)?.title || '' });
      more.hidden = !body.has_more;
      playButton.hidden = points.length < 2 || !play;
      status.textContent = historyStatus(tasksById.get(id), summary, body);
    } catch (error) {
      if (!destroyed && current === generation && error.name !== 'AbortError') {
        status.textContent = error.status === 403 ? '当前账号无权查看该任务的轨迹，请选择自己发布或自己接单的任务。' : `轨迹读取失败：${error.message}`;
      }
    } finally {
      if (!destroyed && current === generation) { busy = false; load.disabled = false; more.disabled = false; }
    }
  };

  load.addEventListener('click', () => query(false));
  more.addEventListener('click', () => query(true));
  playButton.addEventListener('click', () => {
    if (playing) { stopPlayback?.(); playing = false; stopPlayback = null; playButton.textContent = '轨迹回放'; return; }
    if (!play || points.length < 2) { status.textContent = '当前没有足够的轨迹点可供回放。'; return; }
    playing = true; playButton.textContent = '暂停回放';
    const finish = () => { if (!destroyed) { playing = false; stopPlayback = null; playButton.textContent = '轨迹回放'; } };
    stopPlayback = play(points, finish) || null;
    if (!stopPlayback) { playing = false; playButton.textContent = '轨迹回放'; }
  });
  select.addEventListener('change', () => { autoTried = true; invalidate(); if (select.value) query(false); else status.textContent = '请先选择任务。'; });
  clearButton.addEventListener('click', () => { invalidate(); status.textContent = '轨迹已清除，可重新点击「查看轨迹」。'; });

  return {
    updateTasks(tasks) {
      const readable = (tasks || []).filter(task => {
        if (!viewer.role) return true;
        if (viewer.role === 'RIDER') return Boolean(viewer.sub && task.worker_id === viewer.sub);
        if (viewer.role === 'USER') return !task.created_by || task.created_by === viewer.sub;
        if (viewer.role === 'ADMIN') return true;
        return false;
      });
      tasksById = new Map(readable.map(task => [task.id, task]));
      const previous = select.value, old = [...select.options].find(option => option.value === previous);
      select.replaceChildren(new Option('选择任务', ''));
      const ordered = [...readable].sort((a, b) => {
        const workerOrder = (b.worker_id ? 1 : 0) - (a.worker_id ? 1 : 0);
        if (workerOrder) return workerOrder;
        return (Date.parse(b.created_at || '') || 0) - (Date.parse(a.created_at || '') || 0);
      });
      for (const task of ordered) {
        const statusLabel = statusLabels[task.status] || task.status;
        select.add(new Option(`${task.title}（${statusLabel}${task.worker_id ? '' : ' · 等待接单'}）`, task.id));
      }
      if (previous && [...select.options].some(option => option.value === previous)) { select.value = previous; return; }
      if (previous && old && tasksById.has(previous)) { select.add(old); select.value = previous; return; }
      if (!previous && !autoTried && ordered.length) {
        autoTried = true;
        const preferred = ordered.find(task => task.worker_id && ['COMPLETED', 'CANCELLED'].includes(task.status))
          || ordered.find(task => task.worker_id) || ordered[0];
        select.value = preferred.id;
        query(false);
      } else if (!ordered.length) {
        status.textContent = viewer.role === 'RIDER'
          ? '还没有可查看的履约任务；接单后这里会显示你的历史轨迹。'
          : viewer.role === 'USER'
            ? '当前没有任务。发布任务后，这里会显示你的任务；骑手接单并上报位置后可查看轨迹。'
            : '当前账号没有可查看的历史轨迹。';
      }
    },
    destroy() { destroyed = true; invalidate(); panel.remove(); },
  };
};
