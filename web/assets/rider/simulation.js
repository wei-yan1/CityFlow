/* Demo composition: task + route API adapters are outside the simulation engine. */
window.CityFlowSimulationReady = (async () => {
  const workspace = await window.CityFlowMapReady; if (!workspace || workspace.account.role !== 'RIDER') return;
  const host=document.querySelector('#cityMap'), selection=document.querySelector('#trackedTask');
  const panel=document.createElement('section');panel.className='map-simulation';panel.setAttribute('aria-label','模拟骑手');
  panel.innerHTML='<strong>模拟骑手</strong><p>在上方选择已接单任务，沿高德骑行路线匀速行驶。模拟位置会同步给用户，并进入该骑手的任务历史。</p><label>骑行速度 <select data-speed><option value="10">10 km/h</option><option value="18" selected>18 km/h</option><option value="25">25 km/h</option><option value="36">36 km/h</option></select></label><button type="button" class="button primary" data-start>开始模拟骑行</button><button type="button" class="button outline" data-stop disabled>停止模拟</button><p data-simulation-status role="status" aria-live="polite">每约 4 秒上报；到达终点后停止，任务状态由你手动更新。</p>';
  host.before(panel);
  const start=panel.querySelector('[data-start]'),stop=panel.querySelector('[data-stop]'),speed=panel.querySelector('[data-speed]'),status=panel.querySelector('[data-simulation-status]');
  let generation=0,controller,simulation,lease,taskID,destroyed=false;
  const active=task=>task.worker_id===workspace.account.id&&['CLAIMED','ARRIVED','PROCESSING'].includes(task.status);
  function finish(message) {
    generation++;controller?.abort();simulation?.stop();lease?.release();simulation=null;lease=null;taskID=null;
    start.disabled=destroyed;stop.disabled=true;speed.disabled=false;status.textContent=message;
  }
  start.addEventListener('click',async()=>{
    if(!selection.value){status.textContent='请先在“跟踪任务”中选择自己已接单且带取送坐标的任务。';return;}
    finish('正在读取任务和骑行路线…');const current=generation;taskID=selection.value;
    controller=new AbortController();const signal=controller.signal;start.disabled=true;stop.disabled=false;speed.disabled=true;
    try {
      const task=await api(`/api/v1/tasks/${encodeURIComponent(taskID)}`,{signal});
      if(!active(task)||!task.pickup_location||!task.destination_location)throw Error('请选择自己履约中、带取送坐标的任务。');
      const convert=p=>{const[longitude,latitude]=CityFlowCoordinates.toAmap(p);return{longitude,latitude}};
      const waypoints=task.pickup_stops?.length ? [...task.pickup_stops.map(stop=>stop.location),task.destination_location] : [task.pickup_location,task.destination_location];
      if(waypoints.some(point=>!point))throw Error('取件或送达地点缺少坐标，请联系用户重新选址。');
      const route=await CityFlowRidingRoute(waypoints,signal);
      if(destroyed||current!==generation)return;
      const points=route.coordinates.map(p=>CityFlowCoordinates.fromAmap(p.longitude,p.latitude));
      workspace.drawRoute(points);lease=workspace.locationFeed.acquire('simulation');
      const source=lease,id=taskID;
      simulation=CityFlowRouteSimulator.create({points,speedKmh:Number(speed.value),async publish(point){
        const latest=await api(`/api/v1/tasks/${encodeURIComponent(id)}`,{signal});
        if(!active(latest))throw Error('任务已结束或已无权限，模拟已停止。');
        if(JSON.stringify(latest.pickup_stops?.map(stop=>stop.location) || latest.pickup_location)!==JSON.stringify(task.pickup_stops?.map(stop=>stop.location) || task.pickup_location)||JSON.stringify(latest.destination_location)!==JSON.stringify(task.destination_location))throw Error('任务路线已变更，请重新开始模拟。');
        await source.publish({...point,accuracy:8});
      },onProgress({meters,total,speedKmh}){status.textContent=`模拟中 · ${speedKmh} km/h · 已行驶 ${Math.round(meters)} / ${Math.round(total)} 米`;},onFinish(){finish('已到达路线终点，模拟已停止；请按实际演示需要更新任务状态。');},onError(error){finish(`模拟已停止：${error.message}`);}});
      simulation.start();
    } catch(error) {if(current===generation&&!destroyed)finish(`无法开始模拟：${error.message}`)}
  });
  stop.addEventListener('click',()=>finish('已停止模拟，不再上报新位置。'));
  selection.addEventListener('change',()=>{if(taskID)finish('已切换任务，模拟已停止。')});
  window.addEventListener('pagehide',()=>{destroyed=true;finish('页面已离开，模拟已停止。')},{once:true});
  return {stop:()=>finish('已停止模拟，不再上报新位置。')};
})();
