/* Provider requests are explicit actions; a five-second tracking refresh never
 * triggers paid POI/routing queries. Persistent task points stay WGS84. */
let routeRequestAt = 0;
async function paceRouteRequest() {
  const now = Date.now();
  const wait = Math.max(0, routeRequestAt - now);
  routeRequestAt = Math.max(now, routeRequestAt) + 450;
  if (wait) await new Promise(resolve => setTimeout(resolve, wait));
}
window.CityFlowRidingRoute = async (points, signal, mode = 'bicycling') => {
  const plans = await CityFlowRoutePlans(points, mode, signal);
  return plans[0] || { coordinates: [], distance_meters: 0, duration_seconds: 0, instructions: [] };
};

window.CityFlowRoutePlansForMode = async (points, mode = 'bicycling', signal) => {
  const safePoints = (points || []).filter(point => point && Number.isFinite(Number(point.latitude)) && Number.isFinite(Number(point.longitude)));
  if (safePoints.length < 2) return [];
  const converted = point => {
    const [longitude, latitude] = CityFlowCoordinates.toAmap(point);
    return { longitude, latitude };
  };
  const legs = [];
  for (let index = 1; index < safePoints.length; index++) {
    await paceRouteRequest();
    const response = await api('/api/v1/map/riding', {
      method: 'POST',
      signal,
      body: JSON.stringify({ origin: converted(safePoints[index - 1]), destination: converted(safePoints[index]), coordinate_system: 'GCJ02', mode }),
    });
    const paths = Array.isArray(response.alternatives) && response.alternatives.length
      ? response.alternatives
      : [{ mode: response.mode || mode, index: 0, label: '主路线', coordinates: response.coordinates, distance_meters: response.distance_meters, duration_seconds: response.duration_seconds, instructions: response.instructions }];
    if (!paths.length) throw new Error('暂无可用路线');
    legs.push(paths);
  }
  const planCount = Math.min(3, Math.max(...legs.map(paths => paths.length)));
  const plans = [];
  for (let index = 0; index < planCount; index++) {
    const coordinates = [];
    const instructions = [];
    let distance = 0, duration = 0;
    for (const paths of legs) {
      const path = paths[index] || paths[0];
      for (const point of path.coordinates || []) {
        const previous = coordinates[coordinates.length - 1];
        if (!previous || previous.longitude !== point.longitude || previous.latitude !== point.latitude) coordinates.push(point);
      }
      distance += Number(path.distance_meters) || 0;
      duration += Number(path.duration_seconds) || 0;
      instructions.push(...(path.instructions || []));
    }
    const label = index === 0 ? '推荐方案' : `备选方案 ${index + 1}`;
    plans.push({ mode, index, label, coordinates, distance_meters: distance, duration_seconds: duration, instructions, legs: legs.length });
  }
  return plans;
};

window.CityFlowRoutePlans = async (points, mode = 'bicycling', signal) => {
  if (mode !== 'all') return window.CityFlowRoutePlansForMode(points, mode, signal);
  const modes = ['bicycling', 'electrobike', 'driving'];
  const plans = [];
  const failures = [];
  let lastError = null;
  const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
  for (const routeMode of modes) {
    if (plans.length || failures.length) await wait(220);
    let attempt = 0;
    while (attempt < 2) {
      try {
        plans.push(...await window.CityFlowRoutePlansForMode(points, routeMode, signal));
        break;
      } catch (error) {
        if (error?.name === 'AbortError') throw error;
        lastError = error;
        if (attempt === 0 && (error?.status === 429 || error?.status >= 500 || error?.name === 'TypeError')) {
          attempt++;
          await wait(1200);
          continue;
        }
        failures.push(`${routeMode}: ${error.message || '请求失败'}`);
        break;
      }
    }
  }
  if (!plans.length && lastError && !failures.length) throw lastError;
  plans.failures = failures;
  return plans;
};

window.CityFlowMapServices = ({ host, config, drawRoute, clearRoute, onPlace, allowRiderOrigin = false, includeRider = false, getRiderOrigin = () => null }) => {
  const modeMeta = {
    all: { label: '全部车型', vehicle: '骑行 / 电单车 / 小车' },
    bicycling: { label: '骑行', vehicle: '自行车' },
    electrobike: { label: '电动车', vehicle: '电单车' },
    driving: { label: '驾车', vehicle: '小车' },
  };
  const panel = document.createElement('div'); panel.className = 'map-services';
  panel.innerHTML = `<form class="map-search-form"><label>搜索地点<input name="keywords" minlength="2" maxlength="100" placeholder="输入地点名称" required></label><label>城市<input name="city" maxlength="50" placeholder="输入城市"></label><label>选用地点<select name="target"><option value="">仅查看</option></select></label><button class="button outline" type="submit">搜索地点</button></form><p class="map-service-status" role="status" aria-live="polite"></p><div class="map-search-results"></div><div class="map-route-controls"><label>路线任务<select aria-label="选择路线任务"><option value="draft">新任务选点</option></select></label><label>出行方式<select data-route-mode aria-label="选择出行方式"><option value="all">全部车型 · 骑行 / 电单车 / 小车</option><option value="bicycling">骑行 · 自行车</option><option value="electrobike">电动车 · 电单车</option><option value="driving">驾车 · 小车</option></select></label><label class="map-route-origin" ${allowRiderOrigin ? '' : 'hidden'}><input type="checkbox" data-route-origin> 从骑手位置到取件点</label><button class="button outline" type="button" data-route>规划路线</button><button class="button outline" type="button" data-clear-route>清除路线</button></div><div class="map-route-options" aria-live="polite"></div><div class="map-route-summary" role="status" aria-live="polite"></div>`;
  host.before(panel);
  const form = panel.querySelector('form'), status = panel.querySelector('.map-service-status'), results = panel.querySelector('.map-search-results');
  const routeSelector = panel.querySelector('.map-route-controls select:not([data-route-mode])');
  const modeSelect = panel.querySelector('[data-route-mode]');
  const originToggle = panel.querySelector('[data-route-origin]');
  const routeButton = panel.querySelector('[data-route]'), summary = panel.querySelector('.map-route-summary'), optionsHost = panel.querySelector('.map-route-options');
  if (includeRider && allowRiderOrigin) originToggle.checked = true;
  let tasks = [], generation = 0, searchGeneration = 0, destroyed = false, routeController, searchController, routeSignature = null, pendingSignature = null, selectedPlan = 0, currentPlans = [];
  const notify = text => { status.textContent = text; };
  form.querySelector('button').disabled = !config.search_configured;
  form.elements.city.value = config.default_city || '';
  routeButton.disabled = !config.riding_configured;
  if (!config.search_configured) notify('地点搜索暂不可用。');
  const selectedTask = () => tasks.find(task => task.id === routeSelector.value);
  const baseWaypoints = () => {
    if (routeSelector.value === 'draft') return [CityFlowTaskPoints.pickup, CityFlowTaskPoints.destination].filter(Boolean);
    const task = selectedTask();
    if (!task) return [];
    const pickups = (task.pickup_stops || []).map(stop => stop.location).filter(Boolean);
    if (!pickups.length && task.pickup_location) pickups.push(task.pickup_location);
    if (task.destination_location) pickups.push(task.destination_location);
    return pickups;
  };
  const waypoints = () => {
    const base = baseWaypoints();
    const rider = originToggle.checked ? getRiderOrigin() : null;
    return rider ? [rider, ...base] : base;
  };
  const signature = () => JSON.stringify([routeSelector.value, modeSelect.value, Boolean(originToggle.checked && getRiderOrigin()), ...waypoints()]);
  const segmentNames = () => {
    const names = [];
    const task = selectedTask();
    const base = baseWaypoints();
    if (originToggle.checked && getRiderOrigin()) names.push('骑手当前位置 → 取件点');
    const pickups = task?.pickup_stops?.length ? task.pickup_stops : [{ address: task?.pickup || '取件点' }];
    pickups.forEach((stop, index) => {
      const next = base[Math.min(index + 1, base.length - 1)];
      if (next) names.push(`取件点 ${pickups.length > 1 ? index + 1 : ''} → ${index + 1 === pickups.length ? '送达点' : `取件点 ${index + 2}`}`.trim());
    });
    return names;
  };
  function invalidateRoute() {
    generation++; routeController?.abort(); clearRoute(); routeSignature = null; pendingSignature = null; summary.replaceChildren(); optionsHost.replaceChildren(); currentPlans = []; selectedPlan = 0; routeButton.disabled = !config.riding_configured;
  }
  function renderPlans(plans, expected) {
    currentPlans = plans;
    selectedPlan = 0;
    optionsHost.replaceChildren();
    plans.forEach((plan, index) => {
      const item = document.createElement('button'); item.type = 'button'; item.className = `map-route-option${index === 0 ? ' is-selected' : ''}`; item.dataset.planIndex = String(index);
      const meta = modeMeta[plan.mode] || modeMeta.bicycling;
      item.innerHTML = `<span class="map-route-type ${esc(plan.mode)}">${esc(meta.label)}</span><strong>${esc(plan.label)}</strong><span class="map-route-number">${(plan.distance_meters / 1000).toFixed(2)} 公里 · ${Math.max(1, Math.ceil(plan.duration_seconds / 60))} 分钟</span><small>${plan.legs} 段路线 · ${meta.vehicle}</small>`;
      item.addEventListener('click', () => selectPlan(index, expected));
      optionsHost.append(item);
    });
    if (plans.failures?.length) {
      const note = document.createElement('p'); note.className = 'map-route-failure';
      note.textContent = '部分车型路线暂不可用。';
      optionsHost.append(note);
    }
    drawPlan(plans[0], expected);
  }
  function selectPlan(index, expected) {
    if (!currentPlans[index] || expected !== signature()) return;
    selectedPlan = index;
    optionsHost.querySelectorAll('.map-route-option').forEach((item, itemIndex) => {
      const active = itemIndex === index;
      item.classList.toggle('is-selected', active);
      item.setAttribute('aria-pressed', String(active));
    });
    drawPlan(currentPlans[index], expected);
  }
  function drawPlan(plan, expected) {
    if (!plan || expected !== signature()) return;
    routeSignature = expected;
    drawRoute(plan.coordinates.map(point => CityFlowCoordinates.fromAmap(point.longitude, point.latitude)));
    summary.replaceChildren();
    const meta = modeMeta[plan.mode] || modeMeta.bicycling;
    const title = document.createElement('p'); title.textContent = `当前路线：${meta.label} · ${plan.label} · ${(plan.distance_meters / 1000).toFixed(2)} 公里 · 预计 ${Math.max(1, Math.ceil(plan.duration_seconds / 60))} 分钟`;
    const segments = document.createElement('p'); segments.className = 'map-route-segments'; segments.textContent = `路线：${segmentNames().join(' → ')}`;
    summary.append(title, segments);
    if (plan.instructions?.length) {
      const details = document.createElement('details'), heading = document.createElement('summary'), list = document.createElement('ol'); heading.textContent = '查看路线指引';
      plan.instructions.slice(0, 60).forEach(instruction => { const li = document.createElement('li'); li.textContent = instruction; list.append(li); });
      details.append(heading, list); summary.append(details);
    }
  }
  async function planRoute() {
    if (!config.riding_configured) return;
    const points = waypoints();
    if (points.length < 2) { summary.textContent = '请先选好取件点和送达点，或选择带坐标的任务。'; return; }
    const expected = signature();
    if (routeSignature === expected || pendingSignature === expected) return;
    const riderMissing = originToggle.checked && !getRiderOrigin();
    if (riderMissing) summary.textContent = '骑手位置暂不可用，当前仅规划取送点路线。';
    invalidateRoute(); const current = generation; routeController = new AbortController(); pendingSignature = expected;
    routeButton.disabled = true; summary.textContent = `正在规划${modeMeta[modeSelect.value]?.label || ''}路线…`;
    try {
      const plans = await CityFlowRoutePlans(points, modeSelect.value, routeController.signal);
      if (destroyed || current !== generation || expected !== signature()) return;
      if (!plans.length) throw new Error('没有找到可用路线');
      renderPlans(plans, expected);
    } catch (error) {
      if (!destroyed && current === generation && error.name !== 'AbortError') summary.textContent = '路线规划失败，请稍后重试。';
    } finally {
      if (!destroyed && current === generation) { pendingSignature = null; routeButton.disabled = !config.riding_configured; }
    }
  }
  form.addEventListener('submit', async event => {
    event.preventDefault(); if (!config.search_configured) return;
    const current = ++searchGeneration; searchController?.abort(); searchController = new AbortController();
    const target = form.elements.target.value;
    const query = new URLSearchParams({ keywords: form.elements.keywords.value.trim(), city: form.elements.city.value.trim() });
    results.replaceChildren(); notify('正在搜索地点…');
    try {
      const body = await api(`/api/v1/map/search?${query}`, { signal: searchController.signal });
      if (destroyed || current !== searchGeneration) return;
      notify(body.data.length ? `找到 ${body.data.length} 个地点，点击结果选用。` : '未找到地点，请调整城市或关键词。');
      for (const place of body.data) {
        const button = document.createElement('button'); button.type = 'button'; button.className = 'map-place-result';
        const name = document.createElement('strong'), address = document.createElement('span'); name.textContent = place.name; address.textContent = place.address;
        button.append(name, address); results.append(button);
        button.addEventListener('click', () => {
          const point = CityFlowCoordinates.fromAmap(place.location.longitude, place.location.latitude);
          onPlace(point, place, target); results.replaceChildren(); notify(`已选用：${place.name}`);
        });
      }
    } catch (error) { if (!destroyed && current === searchGeneration && error.name !== 'AbortError') notify(error.message); }
  });
  routeSelector.addEventListener('change', invalidateRoute);
  modeSelect.addEventListener('change', invalidateRoute);
  originToggle?.addEventListener('change', invalidateRoute);
  panel.querySelector('[data-clear-route]').addEventListener('click', invalidateRoute);
  routeButton.addEventListener('click', planRoute);
  return {
    setCity(name) { if (document.activeElement !== form.elements.city) form.elements.city.value = name; },
    selectTask(id) { if ([...routeSelector.options].some(option => option.value === id)) { routeSelector.value = id; invalidateRoute(); } },
    pointsChanged() { if (routeSelector.value === 'draft') invalidateRoute(); },
    updateTasks(values) {
      const previous = routeSelector.value; tasks = values;
      routeSelector.replaceChildren();
      routeSelector.add(new Option('选择带坐标的任务', ''));
      for (const task of tasks.filter(task => task.pickup_location && task.destination_location)) routeSelector.add(new Option(task.title, task.id));
      if ([...routeSelector.options].some(option => option.value === previous)) routeSelector.value = previous;
      if ((routeSignature && routeSignature !== signature()) || (pendingSignature && pendingSignature !== signature())) invalidateRoute();
    },
    planRoute,
    destroy() { destroyed = true; generation++; searchGeneration++; routeController?.abort(); searchController?.abort(); clearRoute(); },
  };
};
