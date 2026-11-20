const {test}=require('node:test');
const assert=require('node:assert/strict');
const {createRoute,create,distance}=require('../web/assets/rider/route-simulator.js');
const points=[{latitude:25,longitude:110},{latitude:25.0001,longitude:110},{latitude:25.0001,longitude:110.001}];
test('constant distance follows unequal segments and preserves corners/endpoints',()=>{
  const route=createRoute(points),corner=distance(points[0],points[1]);
  assert.deepEqual(route.at(0),points[0]);assert.deepEqual(route.at(route.total+100),points[2]);
  assert.deepEqual(route.at(corner),points[1]);
  const after=route.at(corner+20);assert.equal(after.latitude,points[1].latitude);
  assert(Math.abs(distance(points[1],after)-20)<0.02);
  assert(Math.abs(distance(points[0],route.at(5))-5)<0.01);
  assert.throws(()=>createRoute([points[0],points[0]]));
  assert.throws(()=>createRoute([points[0],{latitude:NaN,longitude:0}]));
});
test('elapsed time drives speed, stop prevents further reports, input is copied',async()=>{
  let clock=0,next,finished=0;const reports=[];
  const simulator=create({points,speedKmh:18,now:()=>clock,schedule:fn=>(next=fn),unschedule:()=>{next=null},publish:async point=>reports.push(point),onFinish:()=>finished++});
  simulator.start();await new Promise(setImmediate);
  assert.equal(reports.length,1);
  clock=4000;await next();assert(Math.abs(distance(points[0],points[1])+distance(points[1],reports[1])-20)<0.02);
  clock=8000;await next();assert(Math.abs(distance(reports[1],reports[2])-20)<0.02);
  clock=100000;await next();assert.equal(finished,1);assert.deepEqual(reports.at(-1),points.at(-1));
  simulator.stop();simulator.start();assert.equal(reports.length,4);
  let called=0;const stopped=create({points,publish:async()=>called++});stopped.stop();stopped.start();assert.equal(called,0);
});
test('report failure stops scheduling without pretending arrival',async()=>{
  let scheduled=0,error,finished=false;
  create({points,publish:async()=>{throw Error('offline')},schedule:()=>scheduled++,onError:e=>error=e,onFinish:()=>finished=true}).start();
  await new Promise(setImmediate);assert.equal(error.message,'offline');assert.equal(scheduled,0);assert(!finished);
});
test('location source replacement serializes requests and excludes stale writers',async()=>{
  const fs=require('node:fs'),vm=require('node:vm'),sandbox={window:{},performance,setTimeout,clearTimeout,AbortController,DOMException};
  vm.runInNewContext(fs.readFileSync(require.resolve('../web/assets/shared/location-feed.js'),'utf8'),sandbox);
  const sent=[],positions=[];let release;
  const feed=sandbox.window.CityFlowLocationFeed({interval:5,send:async point=>{sent.push(point);if(point===1)await new Promise(r=>release=r);return point},onPosition:p=>positions.push(p)});
  const gps=feed.acquire('gps'),first=gps.publish(1).catch(e=>e.name);
  while(!release)await new Promise(r=>setTimeout(r,1));
  const simulation=feed.acquire('simulation'),second=simulation.publish(2);release();
  assert.equal(await first,'AbortError');assert.equal(await second,2);
  assert.deepEqual(sent,[1,2]);assert.deepEqual(positions,[2]);
  await assert.rejects(gps.publish(3),{name:'AbortError'});simulation.release();feed.destroy();
});
