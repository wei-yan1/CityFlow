/* Session-only user position; boundaries are provider data, merchant points
 * are CityFlow data. No tile download, offline map or browser disk cache. */
window.CityFlowCity = ({host,config,map,nativeView,located,onViewChange,onCityChanged,acceptLocation}) => {
  const panel = document.createElement('div'); panel.className = 'map-city-controls';
  panel.innerHTML = '<span data-city></span><button class="button outline" type="button" data-all>查看全市</button><button class="button outline" type="button" data-boundary aria-pressed="false">显示行政区边界</button><span data-status role="status" aria-live="polite"></span>';
  host.before(panel);
  let city = {name:config.default_city || '桂林市',adcode:config.default_adcode || '450300'}, district, boundary = false, lines, generation = 0, destroyed = false, cityController, boundaryController;
  const name = panel.querySelector('[data-city]'), status = panel.querySelector('[data-status]'), all = panel.querySelector('[data-all]'), toggle = panel.querySelector('[data-boundary]');
  const label = () => { name.textContent = `浏览 ${city.name}`; all.textContent = `查看${city.name.replace(/市$/, '')}全市`; };
  label(); all.disabled = toggle.disabled = !config.city_configured;
  const clear = () => { if (nativeView) nativeView.clearBoundary(); else { lines?.remove(); lines = null; } };
  const renderBoundary = () => {
    clear(); if (!boundary || !district) return;
    const rings = district.boundaries.map(ring => ring.map(p => CityFlowCoordinates.fromAmap(p.longitude,p.latitude)));
    if (nativeView) nativeView.drawBoundary(rings);
    else lines = L.featureGroup(rings.map(ring => L.polygon(ring.map(p => [p.latitude,p.longitude]),{color:'#176c8b',weight:2,fillOpacity:0.06,interactive:false}))).addTo(map);
  };
  async function loadDistrict(fit) {
    const current = ++generation, code = city.adcode; boundaryController?.abort(); boundaryController = new AbortController();
    all.disabled = toggle.disabled = true; status.textContent = '正在加载城市边界…';
    try {
      const data = district || await api(`/api/v1/map/district?adcode=${encodeURIComponent(code)}`,{signal:boundaryController.signal});
      if (destroyed || current !== generation || code !== city.adcode) return;
      district = data; renderBoundary();
      if (fit) {
        const points = data.boundaries.flat().map(p => CityFlowCoordinates.fromAmap(p.longitude,p.latitude));
        map.fitBounds(L.latLngBounds(points.map(p => [p.latitude,p.longitude])),{maxZoom:12}); onViewChange();
      }
      status.textContent = `${city.name}行政区范围`;
    } catch (error) { if (!destroyed && current === generation && error.name !== 'AbortError') status.textContent = '城市边界加载失败，请稍后重试。'; }
    finally { if (!destroyed && current === generation) all.disabled = toggle.disabled = !config.city_configured; }
  }
  all.addEventListener('click',() => loadDistrict(true));
  toggle.addEventListener('click',() => { boundary = !boundary; toggle.setAttribute('aria-pressed',String(boundary)); toggle.textContent = boundary ? '隐藏行政区边界' : '显示行政区边界'; if (boundary) loadDistrict(false); else clear(); });
  async function identify(point) {
    if (!config.city_configured) return;
    const current = ++generation; cityController?.abort(); boundaryController?.abort(); cityController = new AbortController();
    const [longitude,latitude] = CityFlowCoordinates.toAmap(point);
    try {
      const data = await api(`/api/v1/map/city?${new URLSearchParams({longitude,latitude,coordinate_system:'GCJ02'})}`,{signal:cityController.signal});
      if (destroyed || current !== generation) return;
      if (data.adcode !== city.adcode) { district = null; clear(); }
      city = data; label(); onCityChanged?.(city); status.textContent = `当前位置：${city.name}`; if (boundary) loadDistrict(false);
    } catch(error) { if (!destroyed && current === generation && error.name !== 'AbortError') status.textContent = '城市识别失败，请稍后重试。'; }
    finally { if (!destroyed) all.disabled = toggle.disabled = !config.city_configured; }
  }
  async function locate() {
    const current = ++generation;
    try { const point = await CityFlowLocation.getCurrentPosition({maximumAge:0}); if (destroyed || current !== generation) return; if(acceptLocation && !await acceptLocation(point)){if(!destroyed && current===generation)status.textContent='当前位置不在桂林市服务范围内。';return} if(destroyed || current!==generation)return; located(point); identify(point); }
    catch(error) { if (!destroyed && current === generation) status.textContent = '定位失败，请稍后重试。'; }
  }
  const button = document.querySelector('#mapLocate'); button?.addEventListener('click',locate);
  return { locate, identify, destroy() { destroyed = true; generation++; cityController?.abort(); boundaryController?.abort(); button?.removeEventListener('click',locate); clear(); } };
};

