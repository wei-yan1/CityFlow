/* Pure WGS84 distance sampler; no SDK, DOM, task or network dependency. */
(function(root) {
  const distance = (a,b) => {
    const radians = n => n*Math.PI/180, lat = radians(b.latitude-a.latitude), lon = radians(b.longitude-a.longitude);
    const h = Math.sin(lat/2)**2 + Math.cos(radians(a.latitude))*Math.cos(radians(b.latitude))*Math.sin(lon/2)**2;
    return 6371000*2*Math.asin(Math.sqrt(Math.min(1,h)));
  };
  function createRoute(points) {
    if (!Array.isArray(points) || points.length < 2 || points.length > 100000) throw Error('路线至少需要两个有效坐标。');
    const path = [], lengths = [0];
    for (const point of points) {
      if (!Number.isFinite(point.latitude) || !Number.isFinite(point.longitude) || Math.abs(point.latitude)>85 || Math.abs(point.longitude)>180) throw Error('路线坐标无效。');
      const copy = {latitude:point.latitude,longitude:point.longitude};
      if (path.length) { const step=distance(path[path.length-1],copy); if (step<0.01) continue; lengths.push(lengths[lengths.length-1]+step); }
      path.push(copy);
    }
    const total=lengths[lengths.length-1]; if (total<1) throw Error('路线太短，无法模拟骑行。');
    return { total, at(meters) {
      const target=Math.max(0,Math.min(total,meters));let lo=1,hi=lengths.length-1;
      while(lo<hi) { const mid=(lo+hi)>>1; if(lengths[mid]<target)lo=mid+1;else hi=mid; }
      const a=path[lo-1],b=path[lo],ratio=(target-lengths[lo-1])/(lengths[lo]-lengths[lo-1]);
      return {latitude:a.latitude+(b.latitude-a.latitude)*ratio,longitude:a.longitude+(b.longitude-a.longitude)*ratio};
    }};
  }
  function create({points,speedKmh=18,publish,onProgress=()=>{},onFinish=()=>{},onError=()=>{},interval=4000,now=()=>performance.now(),schedule=setTimeout,unschedule=clearTimeout}) {
    if(!Number.isFinite(speedKmh)||speedKmh<5||speedKmh>40||interval<3200)throw Error('模拟速度或上报间隔无效。');
    const route=createRoute(points);let stopped=false,started=false,timer,previous,meters=0;
    async function tick() {
      if(stopped)return;
      const time=now(),first=previous===undefined;
      if(!first){meters=Math.min(route.total,meters+Math.max(0,time-previous)/1000*speedKmh/3.6);previous=time;}
      try {
        await publish(route.at(meters)); if(stopped)return;if(first)previous=now();
        onProgress({meters,total:route.total,speedKmh});
        if(meters>=route.total) { stopped=true;onFinish();return; }
        timer=schedule(tick,interval);
      } catch(error) {if(!stopped){stopped=true;onError(error)}}
    }
    return {start(){if(!started&&!stopped){started=true;tick()}},stop(){stopped=true;unschedule(timer)},total:route.total};
  }
  const exports={createRoute,create,distance};
  if(typeof module!=='undefined'&&module.exports)module.exports=exports;else root.CityFlowRouteSimulator=exports;
})(typeof window!=='undefined'?window:globalThis);
