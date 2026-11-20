/* Restrict map-center movement to the service polygon without geocoding outside points. */
(function(root){
  function contains(rings,point){return rings.some(ring=>{let inside=false;for(let i=0,j=ring.length-1;i<ring.length;j=i++){const a=ring[i],b=ring[j];if((a.latitude>point.latitude)!==(b.latitude>point.latitude)&&point.longitude<(b.longitude-a.longitude)*(point.latitude-a.latitude)/(b.latitude-a.latitude)+a.longitude)inside=!inside}return inside})}
  function clamp(rings,from,to){
    if(contains(rings,to))return to;
    if(!contains(rings,from))return from;
    const dx=to.longitude-from.longitude,dy=to.latitude-from.latitude;let crossing=1;
    for(const ring of rings)for(let i=0,j=ring.length-1;i<ring.length;j=i++){
      const a=ring[j],b=ring[i],ex=b.longitude-a.longitude,ey=b.latitude-a.latitude,den=dx*ey-dy*ex;
      if(Math.abs(den)<1e-15)continue;
      const ax=a.longitude-from.longitude,ay=a.latitude-from.latitude,t=(ax*ey-ay*ex)/den,u=(ax*dy-ay*dx)/den;
      if(t>=0&&t<=crossing&&u>=0&&u<=1)crossing=t;
    }
    // A tiny inward offset avoids numerical drift placing the marker just outside.
    const t=Math.max(0,crossing-1e-7);const point={latitude:from.latitude+dy*t,longitude:from.longitude+dx*t};
    return contains(rings,point)?point:from;
  }
  const result={contains,clamp};if(typeof module!=='undefined'&&module.exports)module.exports=result;else root.CityFlowCityBoundary=result;
})(typeof window!=='undefined'?window:globalThis);
