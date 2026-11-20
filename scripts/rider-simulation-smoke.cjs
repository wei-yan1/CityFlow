// Use only a disposable seeded server: creates a task and submits simulated observations.
const {chromium}=require('playwright'),assert=require('node:assert/strict');
const base=process.env.CITYFLOW_TEST_URL;if(!base)throw Error('Set CITYFLOW_TEST_URL');
(async()=>{
 const browser=await chromium.launch({headless:true});
 const context=await browser.newContext({permissions:['geolocation'],geolocation:{latitude:25.274,longitude:110.29},viewport:{width:1440,height:1100}});
 let task,user;const errors=[];
 context.on('page',p=>p.on('pageerror',e=>errors.push(e.message)));
 const api=async(token,url,method='GET',data)=>{const r=await context.request.fetch(base+url,{method,headers:token?{Authorization:`Bearer ${token}`}:{},data});assert(r.ok(),`${url}: ${r.status()} ${await r.text()}`);return r.json()};
 const open=async(token,path)=>{const p=await context.newPage();await p.goto(base+'/login');await p.evaluate(t=>sessionStorage.setItem('cityflow_token',t),token);await p.goto(base+path);if(path==='/rider'){await p.evaluate(()=>CityFlowMapReady);await p.locator('.rider-map-details > summary').click()}return p};
 try{
  user=(await api(null,'/api/v1/auth/login','POST',{email:'user@cityflow.local',password:'cityflow123'})).access_token;
  const rider=(await api(null,'/api/v1/auth/login','POST',{email:'rider@cityflow.local',password:'cityflow123'})).access_token;
  task=await api(user,'/api/v1/tasks','POST',{title:'匀速模拟骑手回归',type:'DELIVERY',pickup:'模拟取件点',destination:'模拟送达点',pickup_location:{latitude:25.274,longitude:110.29},destination_location:{latitude:25.277,longitude:110.294}});
  await api(rider,`/api/v1/tasks/${task.id}/claim`,'POST',{});
  const riderPage=await open(rider,'/rider');await riderPage.evaluate(()=>CityFlowSimulationReady);
  const userPage=await open(user,'/user');await userPage.locator(`[data-view-tracking="${task.id}"]`).click();await userPage.waitForFunction(id=>document.querySelector('#trackedTask').value===id,task.id);
  await riderPage.selectOption('#trackedTask',task.id);
  const reports=[];riderPage.on('response',async r=>{if(r.url().endsWith('/riders/me/location')&&r.request().method()==='POST'&&r.ok())reports.push(await r.json())});
  await riderPage.locator('[data-start]').click();
  await riderPage.waitForFunction(()=>document.querySelector('[data-simulation-status]').textContent.includes('模拟中'),{timeout:30000});
  assert(await riderPage.locator('#shareLocation').isDisabled(),'GPS and simulation must be exclusive');
  await riderPage.waitForTimeout(4800);assert(reports.length>=2,`reports=${reports.length}`);assert(reports.every(point=>point.source==='simulated'));
  await userPage.waitForFunction(()=>[...document.querySelectorAll('.map-live-status')].some(el=>el.textContent.includes('模拟骑手')));
  const {distance}=require('../web/assets/rider/route-simulator.js');
  const elapsed=(new Date(reports[1].observed_at)-new Date(reports[0].observed_at))/1000;
  assert(elapsed>=3.2);const meters=distance(reports[0],reports[1]);assert(meters>5&&meters/elapsed<7,`unexpected displacement/speed ${meters}/${elapsed}`);
  await userPage.waitForFunction(async()=>!!(await CityFlowMapReady).adapter.getMarker('tracked:rider'));
  const history=await api(user,`/api/v1/tasks/${task.id}/history?limit=500&after=0`);assert(history.data.length>=2);assert(history.data.some(point=>point.source==='simulated'));
  await riderPage.locator('[data-stop]').click();assert(await riderPage.locator('#shareLocation').isEnabled());
  const count=reports.length;await riderPage.waitForTimeout(4500);assert.equal(reports.length,count,'stop must prevent subsequent reports');
  await riderPage.bringToFront();
  // Chromium keeps the mock fix's original timestamp until its coordinates are refreshed.
  await context.setGeolocation({latitude:25.274001,longitude:110.290001});
  const gps= riderPage.waitForResponse(r=>r.url().endsWith('/riders/me/location')&&r.request().method()==='POST'&&r.ok());
  await riderPage.locator('#shareLocation').click();await gps.catch(async error=>{console.error('GPS status:',await riderPage.locator('.map-live-status').allInnerTexts());throw error;});
  assert.equal(await riderPage.locator('#shareLocation').getAttribute('aria-pressed'),'true');
  await riderPage.locator('[data-start]').click();
  await riderPage.waitForFunction(()=>document.querySelector('[data-simulation-status]').textContent.includes('模拟中'));
  assert.equal(await riderPage.locator('#shareLocation').getAttribute('aria-pressed'),'false');
  assert(await riderPage.locator('#shareLocation').isDisabled());
  await riderPage.locator('[data-stop]').click();
  // Reuse a short route fixture to test terminal delivery without a long real ride.
  await riderPage.route('**/api/v1/map/riding',async route=>{
   const points=await riderPage.evaluate(()=>[{latitude:25.274,longitude:110.29},{latitude:25.27403,longitude:110.29}].map(p=>{const[longitude,latitude]=CityFlowCoordinates.toAmap(p);return{longitude,latitude}}));
   await route.fulfill({json:{coordinates:points,distance_meters:4,duration_seconds:1,instructions:[]}});
  });
  await riderPage.locator('[data-start]').click();
  await riderPage.waitForFunction(()=>document.querySelector('[data-simulation-status]').textContent.includes('已到达路线终点'),null,{timeout:15000});
  assert(await riderPage.locator('#shareLocation').isEnabled());
  assert.equal((await api(user,`/api/v1/tasks/${task.id}`)).status,'CLAIMED','simulation must not mutate task state');
  // Task cancellation is observed before the next position write.
  await riderPage.unroute('**/api/v1/map/riding');await riderPage.locator('[data-start]').click();
  await riderPage.waitForFunction(()=>document.querySelector('[data-simulation-status]').textContent.includes('模拟中'));
  await api(user,`/api/v1/tasks/${task.id}`,'DELETE');
  await riderPage.waitForFunction(()=>document.querySelector('[data-simulation-status]').textContent.includes('任务已结束'),null,{timeout:12000});
  await riderPage.setViewportSize({width:390,height:844});assert(await riderPage.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1));
  const output=require('node:path').join(require('node:os').tmpdir(),'cityflow-rider-simulation');require('node:fs').mkdirSync(output,{recursive:true});
  await riderPage.locator('.map-simulation').screenshot({path:require('node:path').join(output,'mobile.png')});
  assert.deepEqual(errors,[]);console.log('PASS real cycling simulation: near-constant speed, user tracking, stored observations, stop, endpoint, unchanged task state, cancellation, GPS exclusion, mobile');
 }finally{if(task&&user)await api(user,`/api/v1/tasks/${task.id}`,'DELETE').catch(()=>{});await context.close();await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
