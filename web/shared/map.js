/* Shared map workspace: markers are reused by entity ID. */
window.CityFlowTaskPoints = { pickup: null, destination: null };
window.CityFlowMapReady = (async () => {
  const host = document.querySelector('#cityMap');
  if (!host || !getToken()) return;
  const status = document.querySelector('#mapStatus');
  const message = text => { status.textContent = text; };
  const connectionStatus=document.createElement('span'),positionStatus=document.createElement('span');connectionStatus.className='map-live-status';positionStatus.className='map-live-status';connectionStatus.setAttribute('role','status');positionStatus.setAttribute('role','status');status.after(connectionStatus,positionStatus);
  const connectionMessage=text=>{connectionStatus.textContent=text},positionMessage=text=>{positionStatus.textContent=text};
  if (!window.L || !window.CityFlowLeafletAdapter) { message('地图加载失败，请刷新页面。'); return; }
  let config, account;
  try { [config, account] = await Promise.all([api('/api/v1/map/config'), api('/api/v1/auth/me')]); }
  catch (error) { message(error.message); return; }
  let nativeView;
  if (config.provider === 'amap') {
    try { nativeView = await CityFlowAmap.create(host, config); }
    catch (error) { message(error.message); return; }
  }
  const center = config.default_center || { latitude: 25.274, longitude: 110.290 };
  const map = nativeView?.map || L.map(host, { scrollWheelZoom: false }).setView([center.latitude, center.longitude], 13);
  const wheelZoom = CityFlowWheelZoom(host, map, { enabled: true });
  let tileTimer;
  if (!nativeView) {
  map.attributionControl.addAttribution('<a href="/assets/vendor/fleetops/SOURCE.md" target="_blank" rel="noopener">Fleet-Ops 地图组件</a>');
  const attribution = esc(config.attribution).replace('OpenStreetMap contributors', '<a href="https://www.openstreetmap.org/copyright" target="_blank" rel="noopener">OpenStreetMap contributors</a>');
  const tiles = L.tileLayer(config.tile_url, { attribution, maxZoom: 19 });
  const tileNotice = document.createElement('div');
  tileNotice.className = 'map-tile-notice';
  tileNotice.hidden = true;
  tileNotice.setAttribute('role', 'status');
  tileNotice.append('地图加载失败，请稍后重试。');
  const retryTiles = document.createElement('button');
  retryTiles.type = 'button';
  retryTiles.className = 'button outline';
  retryTiles.textContent = '重试底图';
  tileNotice.append(retryTiles);
  host.after(tileNotice);
  let tileErrors = false;
  function waitForTiles() {
    clearTimeout(tileTimer);
    tileErrors = false;
    tileNotice.hidden = true;
    tileTimer = setTimeout(() => { tileNotice.hidden = false; }, 12000);
  }
  tiles.on('loading', waitForTiles);
  tiles.on('tileerror', () => { tileErrors = true; tileNotice.hidden = false; });
  tiles.on('load', () => { clearTimeout(tileTimer); tileNotice.hidden = !tileErrors; });
  retryTiles.addEventListener('click', () => { waitForTiles(); tiles.redraw(); });
  waitForTiles();
  tiles.addTo(map);
  }
  const layers = nativeView?.layers || Object.fromEntries(['shop', 'rider', 'self', 'pickup', 'destination'].map(kind => [kind, L.layerGroup().addTo(map)]));
  if (!nativeView) L.control.layers(null, { '商家': layers.shop, '骑手': layers.rider, '我的位置': layers.self, '取件点': layers.pickup, '送达点': layers.destination }, { collapsed: true }).addTo(map);
  const adapter = nativeView?.adapter || new CityFlowLeafletAdapter(map);
  const markers = adapter.markers;
  const versions = new Map();
  let socket = null, reconnectTimer = null, shareTimer = null, pollTimer = null;
  let selectedTask = '', streamGeneration = 0, streamRevision = 0, sharing = false, shareGeneration = 0, disposed = false, loading = false, fitted = false, followRider = false;
  let activeTasks = [];
  let trackedRiderPosition = null;
  let lastMerchantLoad = 0, viewportTimer, viewportController, viewportGeneration = 0;
  const selector = document.querySelector('#trackedTask');
  const actions = document.querySelector('.map-actions');
  const fitButton = document.createElement('button');
  fitButton.type = 'button'; fitButton.className = 'button outline'; fitButton.textContent = '查看全部点位';
  actions.append(fitButton);
  const followButton = document.createElement('button');
  followButton.type = 'button'; followButton.className = 'button outline'; followButton.textContent = '跟随骑手';
  followButton.setAttribute('aria-pressed', 'false'); followButton.disabled = true;
  actions.append(followButton);
  function setFollow(value) { followRider = value; followButton.setAttribute('aria-pressed', String(value)); }
  function fitMarkers(values) {
    if (nativeView) { nativeView.fitMarkers(values); return; }
    const visible = values.filter(marker => map.hasLayer(marker));
    if (visible.length) map.fitBounds(L.featureGroup(visible).getBounds().pad(0.2), { maxZoom: 16 });
  }
  fitButton.addEventListener('click', () => { setFollow(false); fitMarkers([...markers.values()]); });
  followButton.addEventListener('click', () => {
    setFollow(!followRider);
    const rider = markers.get('tracked:rider');
    if (followRider && rider) map.panTo(rider.getLatLng());
  });
  map.on('dragstart', () => setFollow(false));
  function remove(id) { adapter.removeMarker(id); }
  function upsert(id, point, kind, text) {
    if (!point || !Number.isFinite(point.latitude) || !Number.isFinite(point.longitude)) return;
    const latlng = [point.latitude, point.longitude];
    let marker = markers.get(id);
    if (!marker) {
      const label = { shop: '店', rider: '骑', self: '我', pickup: '取', destination: '送' }[kind];
      marker = adapter.addMarker(id, point.latitude, point.longitude, {
        title: text, animated: kind === 'rider', layer: layers[kind],
        onClick: id.startsWith('shop:') && account.role === 'USER' ? () => window.CityFlowBrowsePlace?.('merchant',id.slice(5)) : undefined,
        icon: L.divIcon({ className: '', html: `<span class="cf-pin ${kind}">${kind === 'rider' ? '<i class="cf-heading" aria-hidden="true"></i>' : ''}${label}</span>`, iconSize: [28, 28], iconAnchor: [14, 14] }),
      });
      marker.cityFlowKind = kind;
      if (id === 'tracked:rider') marker.on('move', event => { if (followRider && map.hasLayer(marker)) map.panTo(event.latlng, { animate: false }); });
    } else if (!marker.getLatLng().equals(latlng)) {
      if (kind === 'rider') {
        adapter.setMarkerRotation(id, CityFlowMapBearing(marker.getLatLng(), L.latLng(latlng)));
      }
      adapter.updateMarkerPosition(id, point.latitude, point.longitude, kind === 'rider', 800);
    }
    if (!(id.startsWith('shop:') && account.role === 'USER')) marker.bindPopup(esc(text));
  }
  let leafletRoute;
  let historyLines = [], historyArrows = [], historyMarkerIDs = [];
  const historyTime = value => value ? new Date(value).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' }) : '时间未知';
  const removeHistoryMarkers = () => { for (const id of historyMarkerIDs) adapter.removeMarker(id); historyMarkerIDs = []; };
  let historyPlayTimer = 0, historyPlayOnEnd = null;
  const stopHistoryPlay = keepMarker => {
    clearInterval(historyPlayTimer); historyPlayTimer = 0;
    if (!keepMarker) adapter.removeMarker('history:play');
    const callback = historyPlayOnEnd; historyPlayOnEnd = null; callback?.();
  };
  const clearHistory = () => {
    stopHistoryPlay();
    if (nativeView) nativeView.clearHistory();
    else { historyLines.forEach(line => line.remove()); historyLines = []; historyArrows.forEach(arrow => arrow.remove()); historyArrows = []; }
    removeHistoryMarkers();
  };
  const addHistoryMarker = (id, point, label, kind, text) => {
    if (!point || !Number.isFinite(point.latitude) || !Number.isFinite(point.longitude)) return;
    adapter.addMarker(id, point.latitude, point.longitude, {
      layer: layers.rider, title: text,
      icon: L.divIcon({ className: '', html: `<span class="cf-pin ${kind}">${label}</span>`, iconSize: [28, 28], iconAnchor: [14, 14] }),
    });
    markers.get(id)?.bindPopup(esc(text));
    historyMarkerIDs.push(id);
  };
  const playHistory = (values, onEnd) => {
    stopHistoryPlay();
    const path = [...values].filter(point => Number.isFinite(point.latitude) && Number.isFinite(point.longitude))
      .sort((a, b) => (Date.parse(a.observed_at) || 0) - (Date.parse(b.observed_at) || 0));
    if (path.length < 2) return null;
    historyPlayOnEnd = onEnd;
    const move = point => {
      const id = 'history:play';
      if (markers.has(id)) adapter.updateMarkerPosition(id, point.latitude, point.longitude, false);
      else {
        adapter.addMarker(id, point.latitude, point.longitude, {
          layer: layers.rider, title: '轨迹回放',
          icon: L.divIcon({ className: '', html: '<span class="cf-pin self">回</span>', iconSize: [28, 28], iconAnchor: [14, 14] }),
        });
      }
    };
    move(path[0]);
    let tick = 0;
    const ticks = Math.max(25, Math.min(200, path.length));
    historyPlayTimer = setInterval(() => {
      tick++;
      const ratio = Math.min(1, tick / ticks);
      const index = Math.min(path.length - 1, Math.round(ratio * (path.length - 1)));
      move(path[index]);
      if (ratio >= 1) stopHistoryPlay(true);
    }, 100);
    return stopHistoryPlay;
  };
  const trackDistance = (a, b) => {
    const radius = 6371000, rad = value => value * Math.PI / 180;
    const dLat = rad(b.latitude - a.latitude), dLng = rad(b.longitude - a.longitude);
    const h = Math.sin(dLat / 2) ** 2 + Math.cos(rad(a.latitude)) * Math.cos(rad(b.latitude)) * Math.sin(dLng / 2) ** 2;
    return 2 * radius * Math.asin(Math.min(1, Math.sqrt(h)));
  };
  const trackBearing = (a, b) => {
    const rad = value => value * Math.PI / 180, deg = value => value * 180 / Math.PI;
    const lat1 = rad(a.latitude), lat2 = rad(b.latitude), dLng = rad(b.longitude - a.longitude);
    return (deg(Math.atan2(Math.sin(dLng) * Math.cos(lat2), Math.cos(lat1) * Math.sin(lat2) - Math.sin(lat1) * Math.cos(lat2) * Math.cos(dLng))) + 360) % 360;
  };
  const addHistoryArrows = segment => {
    const points = (segment.coordinates || []).filter(point => Number.isFinite(point.latitude) && Number.isFinite(point.longitude));
    if (points.length < 2) return;
    const lengths = points.slice(1).map((point, index) => trackDistance(points[index], point));
    const total = lengths.reduce((sum, value) => sum + value, 0);
    if (total < 1) return;
    const count = Math.min(8, Math.max(1, Math.floor(total / 120)));
    for (let arrow = 1; arrow <= count; arrow++) {
      const target = total * arrow / (count + 1);
      let travelled = 0;
      for (let index = 0; index < lengths.length; index++) {
        const length = lengths[index];
        if (travelled + length < target) { travelled += length; continue; }
        const ratio = length ? (target - travelled) / length : 0;
        const from = points[index], to = points[index + 1];
        const latitude = from.latitude + (to.latitude - from.latitude) * ratio;
        const longitude = from.longitude + (to.longitude - from.longitude) * ratio;
        const bearing = trackBearing(from, to);
        historyArrows.push(L.marker([latitude, longitude], {
          interactive: false, keyboard: false, zIndexOffset: 20,
          icon: L.divIcon({ className: 'cf-track-arrow', html: `<span style="transform:rotate(${bearing}deg)">▲</span>`, iconSize: [22, 22], iconAnchor: [11, 11] }),
        }).addTo(map));
        break;
      }
    }
  };
  const history = CityFlowHistory({ host, clear: clearHistory, play: playHistory, draw(segments, meta) {
    setFollow(false); clearHistory();
    if (nativeView) nativeView.drawHistory(segments);
    else {
      for (const segment of segments) {
        const points = segment.coordinates.map(p => [p.latitude, p.longitude]);
        if (points.length === 1) historyLines.push(L.circleMarker(points[0], { radius: 4, color: '#147b68' }).addTo(map));
        else if (points.length > 1) {
          historyLines.push(L.polyline(points, { color: '#147b68', weight: 6, opacity: 0.9, lineCap: 'round', lineJoin: 'round', dashArray: segment.mode === 'road_matched' ? null : '6 8' }).addTo(map));
          addHistoryArrows(segment);
        }
      }
      if (historyLines.length) map.fitBounds(L.featureGroup(historyLines).getBounds().pad(0.15), { maxZoom: 17 });
    }
    const first = meta?.start, last = meta?.end;
    if (first) addHistoryMarker('history:start', first, '起', 'pickup', `${meta.taskTitle || '历史轨迹'} · 起点 · ${historyTime(first.observed_at)}`);
    if (last && (!first || last.latitude !== first.latitude || last.longitude !== first.longitude)) addHistoryMarker('history:end', last, '终', 'destination', `${meta.taskTitle || '历史轨迹'} · 终点 · ${historyTime(last.observed_at)}`);
    if (!segments.length && meta?.points?.length) map.fitBounds(L.latLngBounds(meta.points.map(point => [point.latitude, point.longitude])).pad(0.15), { maxZoom: 17 });
  } });
  const clearRoute = () => { if (nativeView) nativeView.clearRoute(); else if (leafletRoute) { leafletRoute.remove(); leafletRoute = null; } };
  const drawRoute = points => {
    setFollow(false); clearRoute();
    if (nativeView) nativeView.drawRoute(points);
    else { leafletRoute = L.polyline(points.map(p => [p.latitude, p.longitude]), { color: '#176c8b', weight: 6 }).addTo(map); map.fitBounds(leafletRoute.getBounds().pad(0.15)); }
  };
  const services = CityFlowMapServices({ host, config, map, drawRoute, clearRoute, allowRiderOrigin: true, includeRider: true, getRiderOrigin: () => trackedRiderPosition || (account.role === 'RIDER' ? CityFlowLocation?.getLastPosition?.() : null), onPlace(point, place, target) {
    if (target && document.querySelector('#taskForm')) {
      CityFlowTaskPoints[target] = point;
      upsert(`draft:${target}`, point, target, place.name);
      const input = document.querySelector('#taskForm').elements[target];
      if (input) input.value = [place.name, place.address].filter(Boolean).join(' · ');
      pointSummary?.update(); services.pointsChanged();
      message(`已将「${place.name}」设为${target === 'pickup' ? '取件' : '送达'}点。`);
    } else upsert('search:place', point, 'shop', place.name);
    map.setView([point.latitude, point.longitude], 16);
  } });
  function applyPosition(position, tracked = true) {
    if (!position) return;
    if (tracked) trackedRiderPosition = { latitude: Number(position.latitude), longitude: Number(position.longitude) };
    const markerID = tracked ? 'tracked:rider' : `rider:${position.rider_id}`;
    const previous = versions.get(position.rider_id) || 0;
    if (position.version < previous) return;
    if ((markers.get(markerID)?.cityFlowVersion || 0) >= position.version) return;
    versions.set(position.rider_id, position.version);
    upsert(markerID, position, 'rider', `骑手位置 · ${new Date(position.observed_at).toLocaleTimeString()}`);
    const marker = markers.get(markerID);
    if (marker) marker.cityFlowVersion = position.version;
    if (tracked) {
      if (followRider && !markers.get('tracked:rider')?._slideFrame) map.panTo([position.latitude, position.longitude]);
      positionMessage(`位置更新于 ${new Date(position.updated_at).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}`);
    }
  }
  function clearRiders() { trackedRiderPosition = null; remove('tracked:rider'); }
  function stopStream() {
    streamGeneration++;
    clearTimeout(reconnectTimer);
    if (socket) { socket.onclose = null; socket.close(); socket = null; }
    clearRiders();connectionMessage('');positionMessage('');
  }
  async function fetchSnapshot(taskId, generation, connection) {
    const revision = streamRevision;
    try {
      const p = await api(`/api/v1/tasks/${encodeURIComponent(taskId)}/location`);
      if (generation === streamGeneration && revision === streamRevision && (!connection || connection === socket && connection.readyState === WebSocket.OPEN)) applyPosition(p);
    } catch (error) {
      if (generation === streamGeneration && !markers.has('tracked:rider')) positionMessage('正在等待骑手位置。');
    }
  }
  function startStream(taskId) {
    stopStream();
    if (!taskId) return;
    const generation = streamGeneration;
    let retry = 1000;
    const connect = () => {
      if (disposed || generation !== streamGeneration) return;
      const token = getToken();
      if (!token) return;
      const url = new URL(`/api/v1/tasks/${encodeURIComponent(taskId)}/location/stream`, location.href);
      url.protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
      const connection = new WebSocket(url, ['cityflow', `bearer.${token}`]);
      socket = connection;
      connection.onopen = () => { retry = 1000; connectionMessage('位置已更新'); fetchSnapshot(taskId, generation, connection); };
      connection.onmessage = event => {
        if (generation !== streamGeneration || connection !== socket) return;
        try {
          const data = JSON.parse(event.data);
          if (data.task_id !== taskId) return;
          streamRevision++;
          if (data.type === 'rider.location_changed') applyPosition(data.position);
          if (data.type === 'rider.location_unavailable') { clearRiders(); positionMessage('骑手位置已过期。'); }
        } catch { connectionMessage('位置更新异常，正在重试。'); }
      };
      connection.onclose = () => {
        if (disposed || generation !== streamGeneration) return;
        streamRevision++;
        clearRiders(); positionMessage('');connectionMessage('位置更新已暂停，正在重连…');
        reconnectTimer = setTimeout(connect, retry); retry = Math.min(retry * 2, 10000);
      };
    };
    // REST works even when the socket is unavailable. A stream event invalidates an older REST read.
    fetchSnapshot(taskId, generation);
    connect();
  }
  selector.addEventListener('change', () => {
    selectedTask = selector.value;
    followButton.disabled = !selectedTask;
    setFollow(false);
    startStream(selectedTask);
    if (selectedTask) {
      fitMarkers([...markers.entries()].filter(([id]) => id.startsWith(`task:${selectedTask}:`)).map(([, marker]) => marker));
      connectionMessage('正在获取骑手位置…');
    } else message('已停止跟踪。');
  });
  async function refresh() {
    if (loading || disposed) return;
    loading = true;
    try {
      const result = await api('/api/v1/tasks?limit=100&offset=0');
      if (disposed) return;
      const tasks = [...(result.data || [])];
      // A task falling outside the first page is not evidence that it ended.
      const selectedID = selectedTask;
      if (selectedID && !tasks.some(task => task.id === selectedID)) {
        try {
          const current = await api(`/api/v1/tasks/${encodeURIComponent(selectedID)}`);
          if (disposed) return;
          if (selectedTask === selectedID) tasks.push(current);
        } catch (error) {
          if (![403, 404].includes(error.status)) {
            const previous = activeTasks.find(task => task.id === selectedID);
            if (previous && selectedTask === selectedID) tasks.push(previous);
            message('任务状态刷新失败，请稍后重试。');
          }
        }
      }
      if (disposed) return;
      const ids = new Set();
      for (const task of tasks) {
        for (const [index,stop] of (task.pickup_stops || []).entries()) { if (stop.location) { const id=`task:${task.id}:pickup:${index}`; ids.add(id); upsert(id,stop.location,'pickup',`${task.title} · ${stop.address}`); } }
        for (const [field, kind] of [['pickup_location', 'pickup'], ['destination_location', 'destination']]) {
          if (kind === 'pickup' && task.pickup_stops?.length) continue;
          if (task[field]) { const id = `task:${task.id}:${kind}`; ids.add(id); upsert(id, task[field], kind, `${task.title} · ${kind === 'pickup' ? task.pickup : task.destination}`); }
        }
      }
      for (const key of markers.keys()) if (key.startsWith('task:') && !ids.has(key)) remove(key);
      if(account.role==='USER'&&selectedTask){const selected=tasks.find(task=>task.id===selectedTask);if(selected)window.CityFlowRenderTrackingDetails?.(selectedTask,selected)}
      activeTasks = tasks.filter(task => task.worker_id && ['CLAIMED', 'ARRIVED', 'PROCESSING'].includes(task.status));
      services.updateTasks(tasks);
      history.updateTasks(tasks);
      selector.innerHTML = '<option value="">选择已接单的任务</option>' + activeTasks.map(task => `<option value="${esc(task.id)}">${esc(task.title)}</option>`).join('');
      if (selectedTask && !activeTasks.some(task => task.id === selectedTask)) { selectedTask = ''; setFollow(false); followButton.disabled = true; stopStream(); message('任务已结束，位置跟踪已停止。'); }
      selector.value = selectedTask;
      if (Date.now() - lastMerchantLoad > 30000) scheduleViewport();
      if (account.role === 'ADMIN') {
        const center = map.getCenter();
        const riders = await api(`/api/v1/platform/rider-locations?latitude=${center.lat}&longitude=${center.lng}&radius_meters=50000&limit=100`);
        if (disposed) return;
        const riderIDs = new Set((riders.data || []).map(p => `rider:${p.rider_id}`));
        for (const key of markers.keys()) if (key.startsWith('rider:') && !riderIDs.has(key)) remove(key);
        for (const p of riders.data || []) applyPosition(p, false);
      }
    } catch (error) { message(error.message); }
    finally { loading = false; }
  }
  const viewportStatus = document.createElement('p'); viewportStatus.className = 'map-viewport-status'; viewportStatus.setAttribute('role','status'); host.after(viewportStatus);
  async function refreshViewport() {
    if (disposed) return;
    const current = ++viewportGeneration; viewportController?.abort(); viewportController = new AbortController();
    const bounds = map.getBounds(), west = bounds.getWest(), east = bounds.getEast(), south = bounds.getSouth(), north = bounds.getNorth();
    if (east-west > 8 || north-south > 8 || east <= west) { viewportStatus.textContent = '请放大地图，查看平台商家。'; for (const id of markers.keys()) if (id.startsWith('shop:')) remove(id); return; }
    try {
      const query = new URLSearchParams({west,south,east,north,zoom:Math.round(map.getZoom())});
      const places = await api(`/api/v1/map/merchants?${query}`,{signal:viewportController.signal});
      if (disposed || current !== viewportGeneration) return;
      const shopIDs = new Set();
      for (const place of places.data || []) {
        const id = `shop:${place.id}`; shopIDs.add(id);
        if (place.category === 'CLUSTER') {
          remove(id);
          const marker = adapter.addMarker(id,place.location.latitude,place.location.longitude,{
            layer:layers.shop, title:place.name,
            icon:L.divIcon({className:'',html:`<span class="cf-cluster">${Number(place.count)}</span>`,iconSize:[42,42],iconAnchor:[21,21]}),
            onClick:() => { setFollow(false); map.setView([place.location.latitude,place.location.longitude],Math.min(19,Math.max(13,Math.round(map.getZoom())+2))); }
          });
          if (!nativeView) marker.on('click',() => { setFollow(false); map.setView([place.location.latitude,place.location.longitude],Math.min(19,Math.max(13,Math.round(map.getZoom())+2))); });
        } else upsert(id,place.location,'shop',`${place.name} · ${place.address}`);
      }
      for (const id of markers.keys()) if (id.startsWith('shop:') && !shopIDs.has(id)) remove(id);
      lastMerchantLoad = Date.now();
      viewportStatus.textContent = places.truncated ? '当前商家较多，已显示部分点位，请放大查看。' : `当前范围：${places.data.length} 个商家点位或聚合点`;
    } catch(error) { if (!disposed && current === viewportGeneration && error.name !== 'AbortError') viewportStatus.textContent = error.message; }
  }
  function scheduleViewport() { viewportGeneration++; viewportController?.abort(); clearTimeout(viewportTimer); viewportTimer = setTimeout(refreshViewport,300); }
  map.on('moveend',scheduleViewport); map.on('zoomend',scheduleViewport);
  let serviceRings=null, lastInside={latitude:25.274,longitude:110.290}, correctingBoundary=false;
  const restrictedCity = ['USER','RIDER'].includes(account.role);
  const areaReady = restrictedCity && window.CityFlowCityBoundary ? api('/api/v1/map/district?adcode=450300').then(district=>{
    serviceRings=district.boundaries.map(ring=>ring.map(p=>CityFlowCoordinates.fromAmap(p.longitude,p.latitude)));
    const bounds=L.latLngBounds(serviceRings.flat().map(p=>[p.latitude,p.longitude]));
    if(nativeView){const sw=bounds.getSouthWest(),ne=bounds.getNorthEast();nativeView.map.raw.setLimitBounds(new AMap.Bounds(CityFlowCoordinates.toAmap({latitude:sw.lat,longitude:sw.lng}),CityFlowCoordinates.toAmap({latitude:ne.lat,longitude:ne.lng})))}else map.setMaxBounds(bounds.pad(.05));
    const center=map.getCenter(),point={latitude:center.lat,longitude:center.lng};if(CityFlowCityBoundary.contains(serviceRings,point))lastInside=point;else map.setView([lastInside.latitude,lastInside.longitude],13);
    map.on(nativeView?'mapmove':'move',()=>{if(correctingBoundary||disposed)return;const current=map.getCenter(),target={latitude:current.lat,longitude:current.lng};if(CityFlowCityBoundary.contains(serviceRings,target)){lastInside=target;return}const edge=CityFlowCityBoundary.clamp(serviceRings,lastInside,target);correctingBoundary=true;map.setView([edge.latitude,edge.longitude],Math.round(map.getZoom()));correctingBoundary=false;});
    return true;
  }).catch(()=>{message('桂林服务范围加载失败，请稍后重试。');return false}) : Promise.resolve(!restrictedCity);
  const city = CityFlowCity({host,config,map,nativeView,async acceptLocation(point){if(!restrictedCity)return true;if(!await areaReady)throw new Error('桂林服务范围加载失败，请刷新页面后重试。');return CityFlowCityBoundary.contains(serviceRings,point)},located(point) {
    fitted = true; setFollow(false); upsert('self',point,'self','我的位置'); map.setView([point.latitude,point.longitude],14); scheduleViewport();
  },onCityChanged(value) { services.setCity(value.name); },onViewChange() { fitted = true; setFollow(false); scheduleViewport(); }});
  document.querySelector('#refreshMap').addEventListener('click', () => { refresh(); scheduleViewport(); });
  const picker = document.querySelector('#pickMapPoint');
  let pointSummary;
  if (picker) {
    pointSummary = document.createElement('div');
    pointSummary.className = 'map-point-summary';
    pointSummary.setAttribute('role', 'status');
    const pointText = document.createElement('span');
    const clearPoints = document.createElement('button');
    clearPoints.type = 'button'; clearPoints.className = 'button outline'; clearPoints.textContent = '清除选点';
    pointSummary.append(pointText, clearPoints);
    host.before(pointSummary);
    const updateSummary = () => {
      pointText.textContent = ['pickup', 'destination'].map(key => {
        const p = CityFlowTaskPoints[key];
        return `${key === 'pickup' ? '取件' : '送达'}：${p ? '已选择' : '未选点'}`;
      }).join(' · ');
      clearPoints.disabled = !CityFlowTaskPoints.pickup && !CityFlowTaskPoints.destination;
    };
    pointSummary.update = updateSummary;
    clearPoints.addEventListener('click', () => {
      CityFlowTaskPoints.pickup = null; CityFlowTaskPoints.destination = null;
      remove('draft:pickup'); remove('draft:destination'); updateSummary();
      services.pointsChanged();
      message('已清除新任务选点。');
    });
    picker.addEventListener('change', () => {
      host.classList.toggle('is-picking', Boolean(picker.value));
      message(picker.value ? '已进入地图选点。' : '已退出地图选点。');
    });
    updateSummary();
  }
  map.on('click', event => {
    if (!picker?.value) return;
    const key = picker.value;
    const point = { latitude: event.latlng.lat, longitude: event.latlng.lng };
    CityFlowTaskPoints[key] = point;
    upsert(`draft:${key}`, point, key, key === 'pickup' ? '新任务取件点' : '新任务送达点');
    pointSummary?.update();
    services.pointsChanged();
    message(`已选择${key === 'pickup' ? '取件' : '送达'}点。`);
  });
  document.querySelector('#taskForm')?.addEventListener('reset', () => { CityFlowTaskPoints.pickup = null; CityFlowTaskPoints.destination = null; remove('draft:pickup'); remove('draft:destination'); pointSummary?.update(); services.pointsChanged(); });
  const shareButton = document.querySelector('#shareLocation');
  let realLocationLease;
  const locationFeed = shareButton ? CityFlowLocationFeed({
    send(point,signal,source) { return api('/api/v1/riders/me/location',{method:'POST',signal,body:JSON.stringify({latitude:point.latitude,longitude:point.longitude,accuracy:point.accuracy,source:source==='simulation'?'simulated':'device',observed_at:new Date(point.timestamp ?? Date.now()).toISOString()})}); },
    onPosition(position,source) { upsert('self',position,'self','我的共享位置'); positionMessage(`位置更新于 ${new Date(position.updated_at).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })}`); },
    onSource(source) {
      shareButton.disabled = source === 'simulation';
      if(source !== 'gps') { shareGeneration++; sharing=false;clearTimeout(shareTimer);shareButton.textContent='开始共享位置';shareButton.setAttribute('aria-pressed','false'); }
    },
  }) : null;
  async function report(generation) {
    if (!sharing || disposed || generation !== shareGeneration) return;
    try {
      const point = await CityFlowLocation.getCurrentPosition({ maximumAge: 0 });
      if (!sharing || disposed || generation !== shareGeneration) return;
      await realLocationLease.publish(point);
    } catch (error) { if (generation === shareGeneration && !disposed) { positionMessage(`位置共享未成功：${error.message}`); if ([1,'INSECURE_CONTEXT','UNSUPPORTED'].includes(error.code)) { sharing=false;realLocationLease?.release();realLocationLease=null;shareButton.textContent='开始共享位置';shareButton.setAttribute('aria-pressed','false'); } } }
    finally { if (sharing && !disposed && generation === shareGeneration) shareTimer = setTimeout(() => report(generation), 10000); }
  }
  shareButton?.addEventListener('click', () => {
    shareGeneration++;
    sharing = !sharing; shareButton.textContent = sharing ? '停止共享位置' : '开始共享位置'; shareButton.setAttribute('aria-pressed', String(sharing));
    clearTimeout(shareTimer);
    if (sharing) { realLocationLease = locationFeed.acquire('gps');report(shareGeneration); } else {realLocationLease?.release();realLocationLease=null;positionMessage('已停止位置共享。');}
  });
  message('');
  window.addEventListener('pagehide', () => { disposed = true; sharing = false; clearTimeout(tileTimer); stopStream(); clearInterval(pollTimer); clearTimeout(shareTimer); clearTimeout(viewportTimer); viewportController?.abort(); wheelZoom.destroy(); locationFeed?.destroy(); city.destroy(); history.destroy(); services.destroy(); adapter.destroy(); map.remove(); }, { once: true });
  window.addEventListener('pageshow', event => { if (event.persisted) location.reload(); });
  scheduleViewport();
  await refresh();
  if (!disposed) pollTimer = setInterval(refresh, 5000);
  function selectTask(id,task) {if(!id||!task)return;services.selectTask?.(id);for(const [index,stop] of (task.pickup_stops||[]).entries())if(stop.location)upsert(`task:${id}:pickup:${index}`,stop.location,'pickup',`${task.title} · ${stop.address}`);if(!task.pickup_stops?.length&&task.pickup_location)upsert(`task:${id}:pickup`,task.pickup_location,'pickup',`${task.title} · ${task.pickup}`);if(task.destination_location)upsert(`task:${id}:destination`,task.destination_location,'destination',`${task.title} · ${task.destination}`);if(['COMPLETED','CANCELLED'].includes(task.status)){selector.value='';selector.dispatchEvent(new Event('change'));const hs=document.querySelector('.map-history select');if(hs){if(![...hs.options].some(option=>option.value===id))hs.add(new Option(task.title,id));hs.value=id;document.querySelector('[data-history-load]')?.click()}message('任务已结束。');return}if(![...selector.options].some(option=>option.value===id))selector.add(new Option(task.title,id));selector.value=id;selector.dispatchEvent(new Event('change'));}
  return { map, adapter, refresh, account, locationFeed, drawRoute, planRoute: services.planRoute, selectTask, setScrollWheelZoom: enabled => wheelZoom.setEnabled(enabled) };
})();
