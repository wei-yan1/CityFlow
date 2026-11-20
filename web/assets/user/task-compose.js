/* Task composition owns draft stops; the map picker returns an address and WGS84 point. */
window.CityFlowTaskCompose = (() => {
  const form=document.querySelector('#taskForm'), list=document.querySelector('#pickupStops');
  const placeDialog=document.querySelector('#placeDialog'), timeDialog=document.querySelector('#timeDialog');
  const destinationButton=document.querySelector('#destinationPicker'), deliveryButton=document.querySelector('#deliveryTime');
  const date=document.querySelector('#timeDate'), hour=document.querySelector('#timeHour'), minute=document.querySelector('#timeMinute');
  let stops=[],destination=null,deliveryAt=null,choosePlace,chooseTime,pickerMap,pickerAdapter,pickerLayer,pickerWheel,config,searchGeneration=0,searchController;
  const format=value=>new Date(value).toLocaleString('zh-CN',{year:'numeric',month:'2-digit',day:'2-digit',hour:'2-digit',minute:'2-digit'});
  function render() {
    list.replaceChildren();
    stops.forEach((stop,index)=>{
      const row=document.createElement('div');row.className='pickup-stop';
      const head=document.createElement('div');head.className='pickup-stop-head';
      const title=document.createElement('span');title.textContent=stops.length>1?`取件 ${index+1}`:'取件地址';head.append(title);
      if(stops.length>1){for(const [label,delta] of [['上移',-1],['下移',1]]){const move=document.createElement('button');move.type='button';move.className='remove-stop';move.textContent=label;move.disabled=index+delta<0||index+delta>=stops.length;move.addEventListener('click',()=>{[stops[index],stops[index+delta]]=[stops[index+delta],stops[index]];render()});head.append(move)}const remove=document.createElement('button');remove.type='button';remove.className='remove-stop';remove.textContent='删除';remove.addEventListener('click',()=>{if(stop.address&&!confirm('删除这个取件地点？'))return;stops.splice(index,1);render()});head.append(remove)}
      const place=document.createElement('button');place.type='button';place.className='place-button';place.textContent=stop.address || '搜索并选择取件地点';place.addEventListener('click',()=>openPlace(`选择第 ${index+1} 个取件地点`,stop,value=>{stops[index]={...stops[index],...value};render()}));
      const when=document.createElement('button');when.type='button';when.className='time-button';when.textContent=stop.pickup_at ? `取件时间　${format(stop.pickup_at)}` : '取件时间：立即';when.addEventListener('click',()=>openTime('选择取件时间',stop.pickup_at,value=>{stops[index].pickup_at=value;render()},when,true));
      row.append(head,place,when);list.append(row);
    });
    destinationButton.textContent=destination?.address || '搜索并选择送达地点';
    deliveryButton.textContent=deliveryAt?`期望送达　${format(deliveryAt)}`:'期望送达：尽快';
    form.dispatchEvent(new Event('cityflow-draft-change'));
    document.querySelector('#addPickup').disabled=stops.length>=50;document.querySelector('#addPickup').title=stops.length>=50?'最多添加 50 个取件地点':'';
  }
  function reset(){stops=[{address:'',location:null,pickup_at:null}];destination=null;deliveryAt=null;for(const name of ['contact_name','contact_phone','handover_note','description']){const field=form.elements[name];if(field)field.value=''}render()}
  document.querySelector('#addPickup').addEventListener('click',()=>{stops.push({address:'',location:null,pickup_at:null});render()});
  destinationButton.addEventListener('click',()=>openPlace('选择送达地点',destination,value=>{destination=value;render()}));
  deliveryButton.addEventListener('click',()=>openTime('选择期望送达时间',deliveryAt,value=>{deliveryAt=value;render()},deliveryButton));
  form.addEventListener('reset',reset);
  function option(value,label){return new Option(label,value)}
  function openTime(title,value,callback,anchor,immediateIsNow=false){
    chooseTime=callback;document.querySelector('#timeTitle').textContent=title;document.querySelector('#timeError').textContent='';
    const immediate=document.querySelector('#clearTime');immediate.textContent=immediateIsNow?'立即':'尽快';immediate.dataset.currentTime=String(immediateIsNow);
    const now=new Date(), start=new Date(now.getFullYear(),now.getMonth(),now.getDate());
    const current=value?new Date(Math.ceil(new Date(value).getTime()/300000)*300000):new Date(Math.ceil(now.getTime()/300000)*300000);
    date.replaceChildren();hour.replaceChildren();minute.replaceChildren();
    for(let i=0;i<366;i++){const d=new Date(start.getFullYear(),start.getMonth(),start.getDate()+i);const key=`${d.getFullYear()}-${String(d.getMonth()+1).padStart(2,'0')}-${String(d.getDate()).padStart(2,'0')}`;date.add(option(key,`${d.getFullYear()}年${d.getMonth()+1}月${d.getDate()}日 ${i===0?'今天':d.toLocaleDateString('zh-CN',{weekday:'long'})}`))}
    for(let i=0;i<24;i++)hour.add(option(String(i),`${String(i).padStart(2,'0')}点`));
    for(let i=0;i<60;i+=5)minute.add(option(String(i),`${String(i).padStart(2,'0')}分`));
    const key=`${current.getFullYear()}-${String(current.getMonth()+1).padStart(2,'0')}-${String(current.getDate()).padStart(2,'0')}`;date.value=key;if(!date.value)date.selectedIndex=0;hour.value=String(current.getHours());minute.value=String(Math.floor(current.getMinutes()/5)*5);
    if(document.querySelector('#customDialog').open || document.querySelector('#taskDialog').open)timeDialog.showModal();else timeDialog.show();const rect=anchor.getBoundingClientRect(),width=Math.min(520,innerWidth-24),left=rect.left>=width+16?rect.left-width-12:Math.min(innerWidth-width-12,rect.right+12);timeDialog.style.left=`${Math.max(12,left)}px`;timeDialog.style.top=`${Math.max(12,Math.min(rect.top,innerHeight-timeDialog.offsetHeight-12))}px`;for(const select of [date,hour,minute]) select.options[select.selectedIndex]?.scrollIntoView({block:'nearest'});date.focus();
  }
  document.querySelector('#confirmTime').addEventListener('click',()=>{
    const [y,m,d]=date.value.split('-').map(Number),selected=new Date(y,m-1,d,Number(hour.value),Number(minute.value));
    if(selected.getTime()<Date.now()-60000){document.querySelector('#timeError').textContent='请选择当前或之后的时间。';return}
    chooseTime(selected.toISOString());timeDialog.close();
  });
  document.querySelector('[data-close-time]').addEventListener('click',()=>timeDialog.close());
  document.addEventListener('keydown',event=>{if(event.key==='Escape'&&timeDialog.open)timeDialog.close()});
  document.querySelector('#clearTime').addEventListener('click',event=>{chooseTime(null);timeDialog.close()});
  let retryPlace=null,boundaries=null,selected=null,reverseController,reverseGeneration=0,viewportTimer,dragging=false,dragBlocked=false,adjusting=false,lastAllowed={latitude:25.274,longitude:110.290},originalPlace=null;
  const state=document.querySelector('#placeStatus'),results=document.querySelector('#placeResults'),confirm=document.querySelector('#confirmPlace');
  const detail=document.querySelector('#placeDetail'),nameBox=document.querySelector('#selectedName'),addressBox=document.querySelector('#selectedAddress');
  const within=point=>Boolean(boundaries && CityFlowCityBoundary.contains(boundaries,point));
  function setSelected(value){selected=value;confirm.disabled=!value?.poi_id;nameBox.textContent=value?.name || '请选择桂林市内地点';addressBox.textContent=value?.baseAddress || '';}
  function setView(point,zoom=16){if(within(point))lastAllowed=point;pickerMap.setView([point.latitude,point.longitude],zoom)}
  function stopAtBoundary(point){clearTimeout(viewportTimer);reverseGeneration++;reverseController?.abort();dragBlocked=true;setSelected(null);state.textContent='已到桂林市服务范围边界，请在范围内选择地点。';const edge=CityFlowCityBoundary.clamp(boundaries,lastAllowed,point);adjusting=true;setView(edge,pickerMap.getZoom());adjusting=false;}
  function renderPlaces(data) {
    if(pickerAdapter) for(const id of pickerAdapter.markers.keys()) pickerAdapter.removeMarker(id);
    results.replaceChildren();
    const matches=data.filter(place=>within(CityFlowCoordinates.fromAmap(place.location.longitude,place.location.latitude)));state.textContent=matches.length?'选择地点后确认地址。':'没有找到桂林市内地点，请更换关键词。';
      for(const place of matches){const point=CityFlowCoordinates.fromAmap(place.location.longitude,place.location.latitude);const button=document.createElement('button');button.type='button';button.className='place-result';const name=document.createElement('strong'),description=document.createElement('span');name.textContent=place.name;description.textContent=place.address;button.append(name,description);const select=()=>{clearTimeout(viewportTimer);dragging=false;reverseGeneration++;reverseController?.abort();setSelected({name:place.name,baseAddress:place.address,location:point,poi_id:place.id});detail.value='';setView(point);placeDialog.classList.remove('results-expanded');document.activeElement?.blur();state.textContent='地点已选中，请确认地址。';};button.addEventListener('click',select);results.append(button);const marker=pickerAdapter.addMarker('result:'+place.id,point.latitude,point.longitude,{layer:pickerLayer,title:place.name,icon:L.divIcon({className:'',html:'<span class="cf-pin shop">●</span>',iconSize:[28,28],iconAnchor:[14,14]}),onClick:select});if(!pickerMap.raw)marker.on('click',select)}
  }
  async function reverse(point){
    clearTimeout(viewportTimer);const generation=++reverseGeneration;reverseController?.abort();reverseController=new AbortController();
    if(!within(point)){stopAtBoundary(point);return}
    setSelected(null);state.textContent='正在读取地址…';const [longitude,latitude]=CityFlowCoordinates.toAmap(point);
    try{const result=await api(`/api/v1/map/nearby?${new URLSearchParams({longitude,latitude,coordinate_system:'GCJ02'})}`,{signal:reverseController.signal});if(generation!==reverseGeneration||!placeDialog.open)return;renderPlaces(result.data);state.textContent=result.data.length?'请选择附近地点并确认。':'附近没有找到可选地点，请搜索其他地址。';placeDialog.classList.add('results-expanded')}catch(error){if(error.name!=='AbortError'&&generation===reverseGeneration){state.textContent=`附近地点读取失败：${error.message}`;retryPlace=()=>reverse(point);document.querySelector('#placeRetry').hidden=false}}
  }
  async function openPlace(title,existing,callback){
    choosePlace=callback;originalPlace=existing;selected=null;retryPlace=null;document.querySelector('#placeRetry').hidden=true;document.querySelector('#customPlaceList').hidden=true;document.querySelector('#placeMyCustom').setAttribute('aria-expanded','false');document.querySelector('#placeSearch button').disabled=true;document.querySelector('#placeLocate').disabled=true;detail.value=existing?.detail||'';setSelected(null);document.querySelector('#placeTitle').textContent=title;state.textContent='';results.replaceChildren();document.querySelector('#placeSearch').reset();placeDialog.classList.remove('results-expanded');placeDialog.showModal();
    try {
      if(!config)config=await api('/api/v1/map/config');
      if(!boundaries){const district=await api('/api/v1/map/district?adcode=450300');boundaries=district.boundaries.map(ring=>ring.map(p=>CityFlowCoordinates.fromAmap(p.longitude,p.latitude)))}
      if(!placeDialog.open)return;
      if(!pickerMap){const host=document.querySelector('#placeMap');if(config.provider==='amap'){const view=await CityFlowAmap.create(host,{...config,single_place:true});pickerMap=view.map;pickerAdapter=view.adapter;pickerLayer=view.layers.shop}else{pickerMap=L.map(host,{scrollWheelZoom:false}).setView([25.274,110.290],13);L.tileLayer(config.tile_url,{attribution:config.attribution,maxZoom:19}).addTo(pickerMap);pickerAdapter=new CityFlowLeafletAdapter(pickerMap)}
        pickerWheel?.destroy();pickerWheel=CityFlowWheelZoom(host,pickerMap,{minZoom:3,enabled:true});
        const bounds=L.latLngBounds(boundaries.flat().map(p=>[p.latitude,p.longitude]));if(pickerMap.raw){const sw=bounds.getSouthWest(),ne=bounds.getNorthEast();pickerMap.raw.setLimitBounds(new AMap.Bounds(CityFlowCoordinates.toAmap({latitude:sw.lat,longitude:sw.lng}),CityFlowCoordinates.toAmap({latitude:ne.lat,longitude:ne.lng})))}else pickerMap.setMaxBounds(bounds.pad(0.05));
        host.addEventListener('keydown',event=>{const direction={ArrowUp:[1,0],ArrowDown:[-1,0],ArrowLeft:[0,-1],ArrowRight:[0,1]}[event.key];if(!direction)return;event.preventDefault();const center=pickerMap.getCenter(),step=event.shiftKey?0.001:0.0001;const point={latitude:center.lat+direction[0]*step,longitude:center.lng+direction[1]*step};if(!within(point)){state.textContent='已到桂林市服务范围边界。';return}setView(point,pickerMap.getZoom());clearTimeout(viewportTimer);viewportTimer=setTimeout(()=>reverse(point),500)});
        pickerMap.on('click',event=>{const point={latitude:event.latlng.lat,longitude:event.latlng.lng};if(!within(point)){stopAtBoundary(point);return}setView(point);reverse(point)});
        pickerMap.on('dragstart',()=>{dragging=true;dragBlocked=false});
        pickerMap.on(pickerMap.raw?'mapmove':'move',()=>{if(!dragging||adjusting||!placeDialog.open)return;const center=pickerMap.getCenter(),point={latitude:center.lat,longitude:center.lng};if(!within(point))stopAtBoundary(point);else lastAllowed=point});
        pickerMap.on('moveend',()=>{if(!dragging||!placeDialog.open)return;dragging=false;if(dragBlocked)return;const center=pickerMap.getCenter();viewportTimer=setTimeout(()=>reverse({latitude:center.lat,longitude:center.lng}),500)});
      }
      document.querySelector('#placeSearch button').disabled=!config.search_configured;document.querySelector('#placeLocate').disabled=false;pickerMap.invalidateSize();for(const id of pickerAdapter.markers.keys())pickerAdapter.removeMarker(id);if(existing?.location){setView(existing.location);setSelected({name:existing.name||existing.address,baseAddress:existing.baseAddress||existing.address,location:existing.location,poi_id:existing.poi_id})}else{setView({latitude:25.274,longitude:110.290},13);state.textContent='搜索或在地图上选择桂林市内地点。'}
      if(!config.search_configured)state.textContent='地点搜索尚未配置，请联系管理员。';
    }catch(error){state.textContent=`无法加载桂林服务范围：${error.message}`;confirm.disabled=true;retryPlace=()=>{placeDialog.close();setTimeout(()=>openPlace(title,existing,callback),0)};document.querySelector('#placeRetry').hidden=false}
  }
  document.querySelector('#placeRetry').addEventListener('click',()=>{document.querySelector('#placeRetry').hidden=true;retryPlace?.()});
  document.querySelector('[data-close-place]').addEventListener('click',()=>placeDialog.close());
  placeDialog.addEventListener('close',()=>{if(placeDialog.open)return;dragging=false;searchGeneration++;reverseGeneration++;searchController?.abort();reverseController?.abort();clearTimeout(viewportTimer);choosePlace=null});
  document.querySelector('#confirmPlace').addEventListener('click',()=>{if(!selected?.poi_id||!within(selected.location))return;choosePlace?.({name:selected.name,baseAddress:selected.baseAddress,poi_id:selected.poi_id,address:[selected.name,selected.baseAddress!==selected.name?selected.baseAddress:''].filter(Boolean).join(' · '),location:selected.location});placeDialog.close()});
  document.querySelector('#placeMyLocation').addEventListener('click',()=>document.querySelector('#placeLocate').click());
  document.querySelector('#placeLocate').addEventListener('click',async()=>{try{state.textContent='正在定位…';const point=await CityFlowLocation.getCurrentPosition({maximumAge:0});if(!within(point)){state.textContent=`您当前的位置不在服务地点桂林市以内（${point.latitude.toFixed(5)}, ${point.longitude.toFixed(5)}），请搜索并选择桂林市内地址。`;const [longitude,latitude]=CityFlowCoordinates.toAmap(point);try{const nearby=await api(`/api/v1/map/reverse-address?${new URLSearchParams({longitude,latitude,coordinate_system:'GCJ02'})}`);if(placeDialog.open)state.textContent=`您当前的位置：${nearby.address}。不在服务地点桂林市以内，请搜索并选择桂林市内地址。`}catch{}return}setView(point);reverse(point)}catch(error){state.textContent=error.message}});
  const sheetToggle=document.querySelector('#sheetToggle');
  function setSheet(expanded){placeDialog.classList.toggle('results-expanded',expanded);sheetToggle.setAttribute('aria-expanded',String(expanded))}
  sheetToggle.addEventListener('click',()=>setSheet(!placeDialog.classList.contains('results-expanded')));
  let touchStart=null;
  sheetToggle.addEventListener('touchstart',event=>{touchStart=event.touches[0].clientY},{passive:true});
  sheetToggle.addEventListener('touchend',event=>{if(touchStart===null)return;const delta=event.changedTouches[0].clientY-touchStart;touchStart=null;if(Math.abs(delta)>25){setSheet(delta<0);event.preventDefault()}},{passive:false});
  document.querySelector('#placeSearch input').addEventListener('focus',()=>placeDialog.classList.add('results-expanded'));
  document.querySelector('#placeSearch').addEventListener('submit',async event=>{
    event.preventDefault();if(!config?.search_configured||!boundaries)return;const current=++searchGeneration;searchController?.abort();searchController=new AbortController();
    if(pickerAdapter) for(const id of pickerAdapter.markers.keys()) pickerAdapter.removeMarker(id);results.replaceChildren();state.textContent='正在搜索…';
    try{const query=new URLSearchParams({keywords:event.currentTarget.elements.keywords.value.trim(),city:'桂林市'});const response=await api(`/api/v1/map/search?${query}`,{signal:searchController.signal});if(current!==searchGeneration||!placeDialog.open)return;
      renderPlaces(response.data);
    }catch(error){if(current===searchGeneration&&error.name!=='AbortError'){state.textContent=`搜索失败：${error.message}`;retryPlace=()=>document.querySelector('#placeSearch').requestSubmit();document.querySelector('#placeRetry').hidden=false}}
  });
  const tracking=document.querySelector('#trackingDialog');let mapLoading;
  window.CityFlowEnsureMap=async target=>{target.append(document.querySelector('#mapWorkspace'));if(!window.CityFlowMapReady){mapLoading ||= new Promise((resolve,reject)=>{const script=document.createElement('script');script.src='/assets/shared/map.js?v=20261120-track1';script.onload=resolve;script.onerror=()=>{mapLoading=null;script.remove();reject(new Error('地图加载失败'))};document.head.append(script)});await mapLoading}const ready=await window.CityFlowMapReady;if(!ready){window.CityFlowMapReady=null;mapLoading=null;throw Error('地图组件暂不可用')}ready.map.invalidateSize();return ready};
  window.CityFlowOpenTracking=async id=>{tracking.append(document.querySelector('#mapWorkspace'));if(!tracking.open)tracking.showModal();try{const ready=await CityFlowEnsureMap(tracking);if(!ready)throw Error('地图组件暂不可用');ready.setScrollWheelZoom?.(true);if(!boundaries){const district=await api('/api/v1/map/district?adcode=450300');boundaries=district.boundaries.map(ring=>ring.map(p=>CityFlowCoordinates.fromAmap(p.longitude,p.latitude)))}if(ready){const bounds=L.latLngBounds(boundaries.flat().map(p=>[p.latitude,p.longitude]));if(ready.map.raw){const sw=bounds.getSouthWest(),ne=bounds.getNorthEast();ready.map.raw.setLimitBounds(new AMap.Bounds(CityFlowCoordinates.toAmap({latitude:sw.lat,longitude:sw.lng}),CityFlowCoordinates.toAmap({latitude:ne.lat,longitude:ne.lng})))}else ready.map.setMaxBounds(bounds.pad(.05));ready.map.invalidateSize();await ready.refresh();}if(id){const task=window.CityFlowUserTasks?.().find(t=>t.id===id) || await api(`/api/v1/tasks/${encodeURIComponent(id)}`);ready?.selectTask(id,task);const box=document.querySelector('#trackingDetails');if(task){box.replaceChildren();const line=document.createElement('p');line.textContent=`${task.title} · ${statusLabels[task.status]||task.status}`;box.append(line);for(const stop of (task.pickup_stops?.length?task.pickup_stops:[{address:task.pickup}])){const p=document.createElement('p');p.textContent=`取件：${stop.address}${stop.status==='PICKED_UP'?'（已取件）':stop.status==='ARRIVED'?'（已到达）':'（待取件）'}`;box.append(p)}const end=document.createElement('p');end.textContent=`送达：${task.destination}`;box.append(end)}}}catch(error){mapLoading=null;window.CityFlowMapReady=null;document.querySelector('#mapStatus').textContent='地图加载失败，请关闭窗口后重试。'}};
  window.CityFlowRenderTrackingDetails=(id,current)=>{const task=current||window.CityFlowUserTasks?.().find(t=>t.id===id),box=document.querySelector('#trackingDetails');if(!task){box.replaceChildren();return}box.replaceChildren();for(const text of [`${task.title} · ${statusLabels[task.status]||task.status}`,...(task.pickup_stops?.length?task.pickup_stops:[{address:task.pickup}]).map(stop=>`取件：${stop.address}${stop.status==='PICKED_UP'?'（已取件）':stop.status==='ARRIVED'?'（已到达）':'（待取件）'}`),`送达：${task.destination}`]){const p=document.createElement('p');p.textContent=text;box.append(p)}};
  document.querySelector('#trackedTask').addEventListener('change',event=>CityFlowRenderTrackingDetails(event.target.value));
  document.querySelector('#openTracking').addEventListener('click',()=>CityFlowOpenTracking());document.querySelector('[data-close-tracking]').addEventListener('click',()=>tracking.close());

  document.querySelector('#placeMyCustom').addEventListener('click',()=>{
    const list=document.querySelector('#customPlaceList'),button=document.querySelector('#placeMyCustom');list.hidden=!list.hidden;button.setAttribute('aria-expanded',String(!list.hidden));if(list.hidden)return;
    list.replaceChildren();const places=window.CityFlowCustom?.places()||[];
    if(!places.length){list.textContent='还没有定制地址，可在首页的个人定制中添加。';return}
    for(const place of places){const item=document.createElement('button');item.type='button';item.className='place-result';item.textContent=place.address;item.addEventListener('click',()=>{if(!within(place.location)){state.textContent='该地址不在桂林市服务范围内。';return}reverseGeneration++;reverseController?.abort();clearTimeout(viewportTimer);setSelected({name:place.name||place.address,baseAddress:place.baseAddress||place.address,location:place.location,poi_id:place.poi_id});detail.value=place.detail||'';setView(place.location);list.hidden=true;button.setAttribute('aria-expanded','false');state.textContent='地点已选中，可确认地址。'});list.append(item)}
  });
  reset();
  return {
    draft(){return {stops:structuredClone(stops),destination:structuredClone(destination),deliveryAt,contact:{name:form.elements.contact_name.value,phone:form.elements.contact_phone.value,note:form.elements.handover_note.value,description:form.elements.description.value}}},
    setDraft(value){stops=structuredClone(value.stops);destination=structuredClone(value.destination);deliveryAt=value.deliveryAt||null;if(value.contact){form.elements.contact_name.value=value.contact.name||'';form.elements.contact_phone.value=value.contact.phone||'';form.elements.handover_note.value=value.contact.note||'';form.elements.description.value=value.contact.description||''}render()},
    reset,
    value(){
    const fail=message=>{document.querySelector('#composeNotice').textContent=message;showNotice(message)};document.querySelector('#composeNotice').textContent='';
    const missing=stops.findIndex(stop=>!stop.address||!stop.location||!stop.poi_id);if(missing>=0){fail(`请选择第 ${missing+1} 个取件地点。`);list.querySelectorAll('.place-button')[missing]?.focus();return null}
    if(!destination?.location||!destination.poi_id){fail('请选择送达地点。');destinationButton.focus();return null}
    if(boundaries && (stops.some(stop=>!within(stop.location)) || !within(destination.location))){fail('取送地点必须位于桂林市服务范围内。');return null}
    if(deliveryAt&&stops.some(stop=>stop.pickup_at&&new Date(stop.pickup_at)>new Date(deliveryAt))){fail('期望送达时间不能早于取件时间。');return null}
    return {pickup:stops[0].address,pickup_location:stops[0].location,pickup_stops:stops.map(stop=>({poi_id:stop.poi_id,address:stop.address,location:stop.location,pickup_at:stop.pickup_at||null})),destination_poi_id:destination.poi_id,destination:destination.address,destination_location:destination.location,delivery_at:deliveryAt,contact_name:form.elements.contact_name.value.trim(),contact_phone:form.elements.contact_phone.value.trim(),handover_note:form.elements.handover_note.value.trim(),description:form.elements.description.value.trim()};
  }};
})();
