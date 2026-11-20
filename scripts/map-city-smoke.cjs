// Disposable seeded server only. Uses real Amap city/boundary services.
const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const base = process.env.CITYFLOW_TEST_URL;
if (!base) throw Error('Set CITYFLOW_TEST_URL to a disposable seeded server');
(async () => {
  const browser = await chromium.launch({headless:true});
  const context = await browser.newContext({permissions:['geolocation'],geolocation:{latitude:25.274,longitude:110.290},viewport:{width:1440,height:1100}});
  try {
    const login = await context.request.post(base+'/api/v1/auth/login',{data:{email:'user@cityflow.local',password:'cityflow123'}});
    assert(login.ok());const token=(await login.json()).access_token;
    const page=await context.newPage(),errors=[],requests=[];
    page.on('pageerror',e=>errors.push(e.message));
    page.on('request',r=>{if(r.url().includes('/map/merchants?'))requests.push(r.url())});
    await page.goto(base+'/login');await page.evaluate(t=>sessionStorage.setItem('cityflow_token',t),token);await page.goto(base+'/user');
    assert(await page.evaluate(async()=>!!(await CityFlowMapReady).map.raw));
    await page.waitForFunction(()=>document.querySelector('[data-status]').textContent.includes('当前位置：桂林'));
    await page.locator('[data-all]').click();
    await page.waitForFunction(()=>document.querySelector('[data-status]').textContent.includes('行政区范围'),{timeout:30000});
    await page.locator('[data-boundary]').click();
    await page.waitForFunction(()=>document.querySelector('[data-boundary]').getAttribute('aria-pressed')==='true'&&!document.querySelector('[data-boundary]').disabled);
    assert(await page.evaluate(async()=>{const {map}=await CityFlowMapReady;return map.raw.getAllOverlays('polygon').length>0}));
    await page.locator('#mapLocate').click();
    await page.waitForTimeout(1200);
    const before=requests.length;
    await page.evaluate(async()=>{const{map}=await CityFlowMapReady;for(let i=0;i<10;i++)map.setView([25.274+i*.0002,110.290],14)});
    await page.waitForTimeout(1600);
    assert(requests.length-before<=2,'view changes must be debounced');
    assert(requests.every(url=>url.includes('west=')&&!url.includes('limit=100')));
    // Exercise native cluster rendering/expansion without inserting business fixtures.
    await page.route('**/api/v1/map/merchants?**',async route=>{
      const zoom=Number(new URL(route.request().url()).searchParams.get('zoom'));
      const data=zoom<13 ? [{id:'cluster:test',category:'CLUSTER',name:'2 家平台商家',count:2,location:{latitude:25.274,longitude:110.29},bounds:{west:110.28,south:25.27,east:110.30,north:25.28}}] : [{id:'detail-test',category:'MERCHANT',name:'测试点',address:'桂林',location:{latitude:25.274,longitude:110.29}}];
      await route.fulfill({json:{data,coordinate_system:'WGS84',truncated:false}});
    });
    await page.evaluate(async()=>{(await CityFlowMapReady).map.setView([25.274,110.29],10)});
    await page.locator('.cf-cluster').waitFor();
    await page.locator('.cf-cluster').click();
    await page.waitForFunction(async()=>!!(await CityFlowMapReady).adapter.getMarker('shop:detail-test'));
    await page.waitForFunction(async()=>Math.round((await CityFlowMapReady).map.getZoom())>=13);
    await page.waitForTimeout(1500);
    assert(await page.evaluate(async()=>Math.round((await CityFlowMapReady).map.getZoom())>=13),'cluster zoom must remain expanded');
    const output=require('node:path').join(require('node:os').tmpdir(),'cityflow-city-map');
    require('node:fs').mkdirSync(output,{recursive:true});
    await page.screenshot({path:require('node:path').join(output,'desktop.png'),fullPage:true});
    await page.setViewportSize({width:390,height:844});await page.waitForTimeout(500);
    assert(await page.evaluate(()=>document.documentElement.scrollWidth<=innerWidth+1),'mobile must not overflow');
    await page.screenshot({path:require('node:path').join(output,'mobile.png'),fullPage:true});
    assert.deepEqual(errors,[]);
    console.log('PASS real Amap: Guilin identification, city bounds, polygon, locate, viewport debounce; fixture cluster expansion');
  } finally {await context.close();await browser.close()}
})().catch(e=>{console.error(e);process.exitCode=1});
