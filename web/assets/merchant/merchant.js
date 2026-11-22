let merchantProfile = null;
let missing = false, busy = false, editing = false, disposed = false;
let view, marker, searchController, searchSequence = 0, selfMarker = null;
const form = document.querySelector('#merchantForm');
const mapStatus = text => { document.querySelector('#merchantMapStatus').textContent = text; };
function renderMerchant() {
  const edit = document.querySelector('#editProfile'); edit.disabled = busy || (!merchantProfile && !missing); edit.textContent = merchantProfile ? '编辑门店' : '新增门店';
  const host = document.querySelector('#merchantProfile'), p = merchantProfile;
  if (!p) { host.innerHTML = '<div class="empty-state">暂无门店资料。</div>'; return; }
  host.innerHTML = `<div class="card-heading"><div><h2>${esc(p.name)}</h2></div><span class="task-status ${p.business_status === 'OPEN' ? 'open' : ''}">${esc({ OPEN: '营业中', CLOSED: '已关闭', PAUSED: '暂停接单' }[p.business_status])}</span></div><div class="profile-details">${[['门店地址',p.address],['联系电话',p.phone || '未填写'],['服务类型',p.service_types.map(value=>({FOOD:'饮食',SUPPLIES:'用品',OTHER:'其它'})[value] || value).join('、')],['每小时服务容量',p.capacity_per_hour + ' 单'],['更新时间',new Date(p.updated_at).toLocaleString('zh-CN',{month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'})]].map(([label,value]) => `<div class="profile-detail"><small>${esc(label)}</small><strong>${esc(value)}</strong></div>`).join('')}</div><p class="task-description">${esc(p.description || '暂无门店介绍。')}</p><section class="merchant-reviews" aria-labelledby="merchantReviewsTitle"><div class="card-heading"><div><h3 id="merchantReviewsTitle">用户评价</h3></div><button id="loadMerchantReviews" class="button outline" type="button">刷新评价</button></div><div id="merchantReviews" class="merchant-review-list"><p class="task-description">正在加载评价…</p></div></section><div class="task-actions"><button id="archiveMerchant" class="button danger" ${busy ? 'disabled' : ''}>归档门店资料</button></div>`;
  document.querySelector('#archiveMerchant').addEventListener('click', archiveMerchant);
  document.querySelector('#loadMerchantReviews').addEventListener('click', () => loadMerchantReviews());
  loadMerchantReviews();
}

async function loadMerchantReviews() {
  const host = document.querySelector('#merchantReviews');
  const button = document.querySelector('#loadMerchantReviews');
  if (!host || !merchantProfile) return;
  if (button) button.disabled = true;
  host.innerHTML = '<p class="task-description">正在加载评价…</p>';
  try {
    const result = await api('/api/v1/merchants/me/reviews?limit=20&offset=0');
    const reviews = result.data || [];
    host.innerHTML = reviews.length ? reviews.map(review => `<article class="merchant-review"><div class="review-heading"><strong>${esc(review.author_label || '匿名用户')}</strong><span>${'★'.repeat(Math.max(0, Number(review.rating) || 0))}${'☆'.repeat(Math.max(0, 5 - (Number(review.rating) || 0)))}</span></div><p>${esc(review.comment || '未填写评价内容。')}</p><time>${esc(review.date || '')}</time></article>`).join('') : '<p class="task-description">暂无用户评价。用户完成关联任务后才会留下评价。</p>';
  } catch (error) {
    host.innerHTML = `<p class="task-description">评价加载失败：${esc(error.message || '请稍后重试。')}</p>`;
  } finally {
    if (button) button.disabled = false;
  }
}
function showPoint(point) {
  if (!view) return;
  const icon = L.divIcon({ className: '', html: '<span class="cf-pin shop">店</span>', iconSize: [28,28], iconAnchor: [14,14] });
  if (view.adapter) {
    view.adapter.removeMarker('own-shop');
    if (point) view.adapter.addMarker('own-shop',point.latitude,point.longitude,{ layer:view.layers.shop, icon, title:'我的门店' });
  } else {
    marker?.remove(); marker = null;
    if (point) marker = L.marker([point.latitude,point.longitude],{icon}).addTo(view.map);
  }
  if (point) view.map.setView([point.latitude,point.longitude],16);
}
function clearSelfPoint() {
  if (!view) return;
  if (view.adapter) view.adapter.removeMarker('merchant:self');
  else { selfMarker?.remove(); selfMarker = null; }
}
function showSelfPoint(point) {
  if (!view || !point) return;
  const icon = L.divIcon({ className: '', html: '<span class="cf-pin self">我</span>', iconSize: [28,28], iconAnchor: [14,14] });
  if (view.adapter) {
    view.adapter.removeMarker('merchant:self');
    view.adapter.addMarker('merchant:self', point.latitude, point.longitude, { layer: view.layers.self, icon, title: '当前位置' });
  } else {
    selfMarker?.remove();
    selfMarker = L.marker([point.latitude, point.longitude], { icon }).addTo(view.map);
  }
  view.map.setView([point.latitude, point.longitude], 16);
}
async function loadMerchant() {
  try { merchantProfile = await api('/api/v1/merchants/me'); missing = false; renderMerchant(); if (!editing) showPoint(merchantProfile); showNotice(''); }
  catch (error) {
    if (error.status === 404) { merchantProfile = null; missing = true; renderMerchant(); if (!editing) showPoint(null); showNotice(''); }
    else { showNotice(error.message); document.querySelector('#editProfile').disabled = !merchantProfile; }
  }
}
function clearSearch() { searchSequence++; searchController?.abort(); document.querySelector('#merchantSearchResults').replaceChildren(); }
function closeEditor() {
  if (busy) return;
  editing = false; document.querySelector('#merchantEditor').hidden = true; clearSearch(); clearSelfPoint(); showPoint(merchantProfile);
  document.querySelector('#merchantMap').classList.remove('is-picking'); mapStatus('');
}
function openEditor() {
  if (busy || (!merchantProfile && !missing)) return;
  form.reset(); editing = true; clearSearch();
  const values = merchantProfile || { business_status:'OPEN',capacity_per_hour:60,latitude:'',longitude:'' };
  for (const key of ['name','phone','address','latitude','longitude','business_status','capacity_per_hour','description','amap_poi_id']) form.elements[key].value = values[key] ?? '';
  form.elements.service_types.value = (merchantProfile?.service_types?.[0] || 'OTHER');
  document.querySelector('#merchantEditorTitle').textContent = merchantProfile ? '编辑门店资料' : '新增门店资料';
  document.querySelector('#merchantFormStatus').textContent = ''; document.querySelector('#merchantEditor').hidden = false;
  document.querySelector('#merchantMap').classList.add('is-picking'); mapStatus('点击地图选择门店位置。');
  document.querySelector('#merchantEditor').scrollIntoView({block:'start',behavior:'smooth'});
}
function choosePoint(point,poi) {
  if (!editing || busy) { mapStatus('请先点击“编辑门店”。'); return; }
  clearSelfPoint();
  form.elements.latitude.value = point.latitude; form.elements.longitude.value = point.longitude;
  if (poi) { form.elements.address.value = [poi.name,poi.address].filter(Boolean).join(' · '); if (!form.elements.name.value) form.elements.name.value = poi.name; }
  form.elements.amap_poi_id.value = poi?.id || '';
  showPoint(point); mapStatus('位置已选择。');
}
async function archiveMerchant() {
  if (busy || !(await cfConfirm('归档后门店将从地图移除，且无法继续接单。', { title: '归档门店资料', confirmText: '归档', danger: true }))) return;
  busy = true; renderMerchant();
  try { await api('/api/v1/merchants/me',{method:'DELETE'}); merchantProfile = null; missing = true; busy = false; closeEditor(); showNotice('门店已归档。','success'); }
  catch (error) { showNotice(error.message); }
  finally { busy = false; renderMerchant(); }
}
async function initializeMap() {
  const host = document.querySelector('#merchantMap');
  try {
    const config = await api('/api/v1/map/config'); if (disposed) return;
    if (config.provider === 'amap') view = await CityFlowAmap.create(host,{...config,single_place:true});
    else {
      const center = config.default_center || {latitude:25.274,longitude:110.290};
      const map = L.map(host,{scrollWheelZoom:false}).setView([center.latitude,center.longitude],13);
      L.tileLayer(config.tile_url,{attribution:esc(config.attribution),maxZoom:19}).on('tileerror',() => mapStatus('地图加载失败，请稍后重试。')).addTo(map); view = {map};
    }
    if (disposed) { view.adapter?.destroy(); view.map.remove(); return; }
    view.wheelZoom = CityFlowWheelZoom(host,view.map,{enabled:true});
    view.map.on('click',event => choosePoint({latitude:event.latlng.lat,longitude:event.latlng.lng}));
    showPoint(merchantProfile); mapStatus(editing ? '点击地图选择门店位置。' : '');
    document.querySelector('#merchantSearch button').disabled = !config.search_configured;
  } catch (error) { mapStatus('地图加载失败，请稍后重试。'); }
}
window.CityFlowMerchantReady = (async () => {
  if (!await requirePageRole('MERCHANT')) return;
  document.querySelector('#editProfile').disabled = true;
  document.querySelector('#editProfile').addEventListener('click',openEditor);
  document.querySelector('#refreshMerchant').addEventListener('click',() => { if (!busy) loadMerchant(); });
  document.querySelectorAll('[data-close]').forEach(button => button.addEventListener('click',closeEditor));
  form.addEventListener('submit',async event => {
    event.preventDefault(); if (busy) return;
    const creating = !merchantProfile, body = Object.fromEntries(new FormData(form));
    body.latitude = Number(body.latitude); body.longitude = Number(body.longitude); body.capacity_per_hour = Number(body.capacity_per_hour); body.service_types = body.service_types.split(',').map(s => s.trim()).filter(Boolean);
    if (!Number.isFinite(body.latitude) || !Number.isFinite(body.longitude)) { document.querySelector('#merchantFormStatus').textContent = '请在地图上选择门店位置。'; return; }
    busy = true; const save = form.querySelector('[type=submit]'); save.disabled = true;
    try { merchantProfile = await api('/api/v1/merchants/me',{method:creating?'POST':'PUT',body:JSON.stringify(body)}); missing = false; busy = false; closeEditor(); renderMerchant(); showNotice('门店资料已保存。','success'); }
    catch (error) { document.querySelector('#merchantFormStatus').textContent = error.message; }
    finally { busy = false; save.disabled = false; }
  });
  document.querySelector('#merchantSearch').addEventListener('submit',async event => {
    event.preventDefault(); if (!editing || busy) { mapStatus('请先点击“编辑门店”。'); return; }
    clearSearch(); const current = searchSequence; searchController = new AbortController(); const query = new URLSearchParams(new FormData(event.currentTarget)); mapStatus('正在搜索地点…');
    try {
      const body = await api(`/api/v1/map/search?${query}`,{signal:searchController.signal});
      if (disposed || current !== searchSequence || !editing) return;
      const host = document.querySelector('#merchantSearchResults');
      for (const poi of body.data.filter(poi => poi.source !== 'merchant')) {
        const button = document.createElement('button'); button.type = 'button'; button.className = 'map-place-result'; button.textContent = `${poi.name} · ${poi.address}`;
        button.addEventListener('click',() => { choosePoint(CityFlowCoordinates.fromAmap(poi.location.longitude,poi.location.latitude),poi); clearSearch(); }); host.append(button);
      }
      mapStatus(body.data.length ? '点击搜索结果选择门店位置。' : '未找到地点，请调整关键词或城市。');
    } catch (error) { if (!disposed && current === searchSequence && error.name !== 'AbortError') mapStatus('地点搜索失败，请稍后重试。'); }
  });
  document.querySelector('#merchantLocate').addEventListener('click',async () => {
    const generation = searchSequence;
    try {
      const point = await CityFlowLocation.getCurrentPosition();
      if (disposed || generation !== searchSequence) return;
      if (!editing) openEditor();
      if (editing) {
        choosePoint(point);
      } else {
        showSelfPoint(point);
        mapStatus('已定位到当前位置，请在地图中确认门店位置。');
      }
    } catch (error) {
      if (!disposed && generation === searchSequence) mapStatus('定位失败，请稍后重试。');
    }
  });
  window.addEventListener('pagehide',() => { disposed = true; clearSearch(); view?.adapter?.destroy(); view?.map.remove(); },{once:true});
  window.addEventListener('pageshow',event => { if (event.persisted) location.reload(); });
  await loadMerchant(); await initializeMap(); return view;
})();

// Media is managed separately from business profile saves.
const mediaSettings=document.querySelector('.merchant-media-settings'),mediaForm=document.querySelector('#merchantMediaForm'),mediaStatus=document.querySelector('#merchantMediaStatus');
const mediaPreview=document.querySelector('#merchantMediaPreview'),avatarFile=document.querySelector('#merchantAvatarFile'),imageFiles=document.querySelector('#merchantImageFiles');

function renderMediaPreview(){
  mediaPreview.replaceChildren();
  const urls=[mediaForm.elements.avatar_url.value.trim(),...mediaForm.elements.image_urls.value.split('\n').map(x=>x.trim()).filter(Boolean)].filter(Boolean).slice(0,7);
  for(const url of urls){const image=document.createElement('img');image.src=url;image.alt='门店图片预览';image.loading='lazy';image.addEventListener('error',()=>image.remove(),{once:true});mediaPreview.append(image)}
}

async function uploadMediaFile(file){
  const token=getToken();
  const data=new FormData();data.append('file',file);
  const response=await fetch('/api/v1/merchants/me/media/upload',{method:'POST',headers:token?{Authorization:`Bearer ${token}`}:{},body:data});
  const result=await response.json().catch(()=>({}));
  if(!response.ok)throw new Error(friendlyError(result.error,response.status));
  return result.url;
}

mediaSettings.addEventListener('toggle',async()=>{if(!mediaSettings.open)return;mediaStatus.textContent='正在加载…';try{const data=await api('/api/v1/merchants/me/media');mediaForm.elements.avatar_url.value=data.avatar_url||'';mediaForm.elements.image_urls.value=(data.image_urls||[]).join('\n');renderMediaPreview();mediaStatus.textContent=''}catch(error){mediaStatus.textContent='图片资料加载失败，请稍后重试。'}});
mediaForm.elements.avatar_url.addEventListener('input',renderMediaPreview);
mediaForm.elements.image_urls.addEventListener('input',renderMediaPreview);
avatarFile.addEventListener('change',async()=>{const file=avatarFile.files?.[0];if(!file)return;mediaStatus.textContent='正在上传头像…';try{mediaForm.elements.avatar_url.value=await uploadMediaFile(file);renderMediaPreview();mediaStatus.textContent='头像已上传，保存后生效。'}catch(error){mediaStatus.textContent='头像上传失败，请稍后重试。'}finally{avatarFile.value=''}});
imageFiles.addEventListener('change',async()=>{const files=[...imageFiles.files||[]];if(!files.length)return;const existing=mediaForm.elements.image_urls.value.split('\n').map(x=>x.trim()).filter(Boolean);mediaStatus.textContent=`正在上传 ${files.length} 张图片…`;try{for(const file of files){if(existing.length>=6){mediaStatus.textContent='门店图片最多 6 张。';break}existing.push(await uploadMediaFile(file))}mediaForm.elements.image_urls.value=existing.join('\n');renderMediaPreview();mediaStatus.textContent='图片已上传，保存后生效。'}catch(error){mediaStatus.textContent='图片上传失败，请稍后重试。'}finally{imageFiles.value=''}});
mediaForm.addEventListener('submit',async event=>{event.preventDefault();const button=mediaForm.querySelector('button[type="submit"]');button.disabled=true;mediaStatus.textContent='正在保存…';try{await api('/api/v1/merchants/me/media',{method:'PUT',body:JSON.stringify({avatar_url:mediaForm.elements.avatar_url.value.trim(),image_urls:mediaForm.elements.image_urls.value.split('\n').map(x=>x.trim()).filter(Boolean).slice(0,6)})});mediaStatus.textContent='图片资料已保存。'}catch(error){mediaStatus.textContent='图片资料保存失败，请稍后重试。'}finally{button.disabled=false}});
