/* High-level marker registry follows the Fleet-Ops adapter contract; AMap SDK
 * rendering/movement is native. No Fleet-Ops order or allocation models involved. */
window.CityFlowAmap = (() => {
  let loading;
  function load(config) {
    if (window.AMap) return Promise.resolve(window.AMap);
    if (loading) return loading;
    loading = new Promise((resolve, reject) => {
      window._AMapSecurityConfig = { securityJsCode: config.amap_security_code };
      const script = document.createElement('script');
      const fail = error => { clearTimeout(timer); script.remove(); reject(error); };
      const timer = setTimeout(() => fail(new Error('高德地图加载超时，请检查网络后重试。')), 20000);
      script.src = `https://webapi.amap.com/maps?v=2.0&key=${encodeURIComponent(config.amap_js_key)}&plugin=AMap.MoveAnimation,AMap.Scale,AMap.ToolBar`;
      script.onload = () => { clearTimeout(timer); window.AMap ? resolve(window.AMap) : fail(new Error('高德地图初始化失败，请检查 JS API Key。')); };
      script.onerror = () => fail(new Error('高德地图脚本加载失败。'));
      document.head.append(script);
    }).catch(error => { loading = undefined; throw error; });
    return loading;
  }
  async function create(host, config) {
    const A = await load(config), coords = CityFlowCoordinates;
    // Native wheel zoom stays off: the shared CityFlowWheelZoom component owns
    // wheel input so AMap and Leaflet maps zoom with the same pointer anchoring.
    const raw = new A.Map(host, { zoom: 13, center: coords.toAmap(config.default_center || { latitude: 25.274, longitude: 110.290 }), viewMode: '2D', scrollWheel: false, isHotspot: true });
    raw.addControl(new A.Scale()); raw.addControl(new A.ToolBar({ position: 'LT' }));
    let destroyed = false;
    const markers = new Map(), handlers = [];
    const layers = Object.fromEntries(['shop', 'rider', 'self', 'pickup', 'destination'].map(kind => [kind, { visible: true, kind }]));
    function fromPosition(position) { const p = coords.fromAmap(position.lng, position.lat); return L.latLng(p.latitude, p.longitude); }
    const map = {
      raw,
      setView(point, zoom) { if (!destroyed) raw.setZoomAndCenter(zoom, coords.toAmap({ latitude: point[0], longitude: point[1] }), true); return this; },
      panTo(point) { if (!destroyed) raw.setCenter(coords.toAmap({ latitude: point.lat ?? point[0], longitude: point.lng ?? point[1] }), true); },
      getCenter() { return fromPosition(raw.getCenter()); },
      getZoom() { return raw.getZoom(); },
      getBounds() {
        const b = raw.getBounds(), sw = fromPosition(b.getSouthWest()), ne = fromPosition(b.getNorthEast());
        return L.latLngBounds(sw, ne);
      },
      fitBounds(bounds, options = {}) {
        const b = L.latLngBounds(bounds), sw = b.getSouthWest(), ne = b.getNorthEast();
        raw.setBounds(new A.Bounds(coords.toAmap({latitude:sw.lat,longitude:sw.lng}),coords.toAmap({latitude:ne.lat,longitude:ne.lng})),true,[35,35,35,35]);
        if (options.maxZoom && raw.getZoom() > options.maxZoom) raw.setZoom(options.maxZoom);
      },
      hasLayer(marker) { return markers.has(marker.id) && marker.layer.visible; },
      on(name, callback) {
        const handler = event => {
          if (destroyed) return;
          if (name === 'click') { const point = fromPosition(event.lnglat); callback({ latlng: point }); }
          else callback(event);
        };
        raw.on(name, handler); handlers.push([name, handler]);
      },
      invalidateSize() { raw.resize(); },
      remove() { destroyed = true; handlers.forEach(([name, handler]) => raw.off(name, handler)); raw.destroy(); },
    };
    const adapter = {
      markers,
      addMarker(id, latitude, longitude, options) {
        this.removeMarker(id);
        const content = document.createElement('div'); content.innerHTML = options.icon.options.html;
        const native = new A.Marker({ position: coords.toAmap({ latitude, longitude }), content, anchor: 'center', title: options.title });
        const callbacks = new Map();
        const marker = {
          id, native, content, layer: options.layer, _slideFrame: 0,
          getLatLng() { return fromPosition(native.getPosition()); },
          on(name, fn) { if (!callbacks.has(name)) callbacks.set(name, []); callbacks.get(name).push(fn); return this; },
          bindPopup(text) { this.popup = text; return this; },
          emitMove() { if (destroyed) return; for (const fn of callbacks.get('move') || []) fn({ latlng: this.getLatLng() }); },
        };
        const moving = () => marker.emitMove();
        const moved = () => { marker._slideFrame = 0; marker.emitMove(); };
        native.on('moving', moving); native.on('moveend', moved);
        native.on('click', () => { options.onClick?.(); if (marker.popup) { new A.InfoWindow({ content: marker.popup }).open(raw, native.getPosition()); } });
        marker.dispose = () => { native.stopMove(); native.off('moving', moving); native.off('moveend', moved); native.setMap(null); marker._slideFrame = 0; };
        if (marker.layer.visible) native.setMap(raw);
        markers.set(id, marker); return marker;
      },
      getMarker(id) { return markers.get(id); },
      updateMarkerPosition(id, latitude, longitude, animated, duration) {
        const marker = markers.get(id); if (!marker || destroyed) return;
        marker.native.stopMove(); marker._slideFrame = 0;
        const point = coords.toAmap({ latitude, longitude });
        if (animated && marker.layer.visible && !matchMedia('(prefers-reduced-motion: reduce)').matches) {
          marker.target = point; marker._slideFrame = 1;
          marker.native.moveTo(point, { duration, autoRotation: false });
        } else { marker.native.setPosition(point); marker.emitMove(); }
      },
      setMarkerRotation(id, angle) { const arrow = markers.get(id)?.content.querySelector('.cf-heading'); if (arrow) arrow.style.transform = `rotate(${angle}deg)`; },
      removeMarker(id) { const marker = markers.get(id); if (marker) { marker.dispose(); markers.delete(id); } },
      destroy() { for (const id of markers.keys()) this.removeMarker(id); },
    };
    const controls = document.createElement('fieldset'); controls.className = 'amap-layer-controls';
    const legend = document.createElement('legend'); legend.textContent = '地图图层'; controls.append(legend);
    for (const [kind, title] of Object.entries({ shop: '商家', rider: '骑手', self: '我的位置', pickup: '取件点', destination: '送达点' })) {
      const label = document.createElement('label'), input = document.createElement('input'); input.type = 'checkbox'; input.checked = true;
      label.append(input, title); controls.append(label);
      input.addEventListener('change', () => {
        layers[kind].visible = input.checked;
        for (const marker of markers.values()) if (marker.layer === layers[kind]) {
          if (!input.checked) { const pending = marker._slideFrame ? marker.target : null; marker.native.stopMove(); marker._slideFrame = 0; if (pending) marker.native.setPosition(pending); }
          marker.native.setMap(input.checked ? raw : null);
        }
      });
    }
    if (!config.single_place) host.append(controls);
    const source = document.createElement('a'); source.href = '/assets/vendor/fleetops/SOURCE.md'; source.textContent = '地图组件来源'; source.className = 'amap-source-link'; source.target = '_blank'; source.rel = 'noopener'; host.append(source);
    let line, historyLines = [], boundaryLines = [];
    const fitMarkers = values => {
      const visible = values.filter(marker => map.hasLayer(marker));
      if (visible.length) raw.setFitView(visible.map(marker => marker.native), false, [50, 100, 50, 50], 16);
    };
    return {
      map, adapter, layers, fitMarkers,
      clearBoundary() { if (boundaryLines.length) raw.remove(boundaryLines); boundaryLines = []; },
      drawBoundary(rings) {
        this.clearBoundary();
        boundaryLines = rings.map(ring => new A.Polygon({path:ring.map(coords.toAmap),strokeColor:'#176c8b',strokeWeight:2,fillColor:'#176c8b',fillOpacity:0.06,zIndex:30,bubble:true}));
        raw.add(boundaryLines);
      },
      clearHistory() { if (historyLines.length) raw.remove(historyLines); historyLines = []; },
      drawHistory(segments) {
        this.clearHistory();
        for (const segment of segments) {
          const path = segment.coordinates.map(coords.toAmap);
          if (path.length === 1) historyLines.push(new A.CircleMarker({ center: path[0], radius: 4, strokeColor: '#147b68', fillColor: '#147b68', fillOpacity: 0.9 }));
          else if (path.length > 1) historyLines.push(new A.Polyline({ path, strokeColor: '#147b68', strokeWeight: 6, strokeOpacity: 0.9, lineJoin: 'round', lineCap: 'round', strokeStyle: segment.mode === 'road_matched' ? 'solid' : 'dashed', showDir: true, zIndex: 70 }));
        }
        if (historyLines.length) { raw.add(historyLines); raw.setFitView(historyLines, false, [45, 100, 45, 45], 17); }
      },
      clearRoute() { if (line) { raw.remove(line); line = null; } },
      drawRoute(points) {
        this.clearRoute();
        line = new A.Polyline({ path: points.map(coords.toAmap), strokeColor: '#176c8b', strokeWeight: 6, strokeOpacity: 0.9, showDir: true });
        raw.add(line); raw.setFitView([line], false, [45, 100, 45, 45], 16);
      },
    };
  }
  return { create };
})();
