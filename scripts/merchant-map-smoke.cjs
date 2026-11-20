// Disposable seeded server; creates a merchant account and archives its profile.
const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const path = require('node:path'), fs = require('node:fs');
const base = process.env.CITYFLOW_TEST_URL;
if (!base) throw new Error('Set CITYFLOW_TEST_URL');
const output = path.join(require('node:os').tmpdir(), 'cityflow-merchant-map'); fs.mkdirSync(output,{recursive:true});
(async () => {
  const browser = await chromium.launch({headless:true});
  const context = await browser.newContext({viewport:{width:1440,height:1000}});
  const errors = []; context.on('page',p => p.on('pageerror',e => errors.push(e.message)));
  const api = async (token,endpoint,method='GET',data) => {
    const r = await context.request.fetch(base+endpoint,{method,headers:token?{Authorization:`Bearer ${token}`}:{},data});
    assert(r.ok(),`${endpoint}: ${r.status()} ${await r.text()}`); return r.status() === 204 ? null : r.json();
  };
  const login = async email => (await api(null,'/api/v1/auth/login','POST',{email,password:'cityflow123'})).access_token;
  let account,admin,merchant;
  try {
    admin = await login('admin@cityflow.local');
    account = await api(admin,'/api/v1/platform/users','POST',{name:'地图门店回归',email:`merchant-map-${Date.now()}@example.test`,password:'cityflow123',role:'MERCHANT'});
    merchant = await login(account.email); const user = await login('user@cityflow.local');
    const open = async (token,route) => {
      const p = await context.newPage(); await p.goto(base+'/login'); await p.evaluate(t => sessionStorage.setItem('cityflow_token',t),token); await p.goto(base+route); return p;
    };
    const page = await open(merchant,'/merchant'); await page.evaluate(() => CityFlowMerchantReady);
    assert(await page.getByRole('button',{name:'新增门店',exact:true}).isEnabled());
    await page.getByRole('button',{name:'新增门店',exact:true}).click();
    const form = page.locator('#merchantForm');
    const config = await api(merchant,'/api/v1/map/config');
    if (config.search_enabled) {
      await page.locator('#merchantSearch input[name=keywords]').fill('人民广场'); await page.locator('#merchantSearch input[name=city]').fill('上海');
      await page.locator('#merchantSearch button').click(); await page.locator('#merchantSearchResults button').first().waitFor({timeout:20000}); await page.locator('#merchantSearchResults button').first().click();
    } else {
      await page.locator('#merchantMap').click({position:{x:170,y:180}}); await form.locator('[name=address]').fill('测试门店地址');
    }
    await form.locator('[name=name]').fill('地图商家新增回归');
    await form.locator('[type=submit]').click(); await page.locator('#merchantEditor').waitFor({state:'hidden'});
    const original = await api(merchant,'/api/v1/merchants/me');
    assert.equal(original.name,'地图商家新增回归'); assert.equal(original.user_id,account.id);
    const userPage = await open(user,'/user'); await userPage.evaluate(() => CityFlowMapReady);
    await userPage.evaluate(async p => { const {map}=await CityFlowMapReady;map.setView([p.latitude,p.longitude],15); },original);
    const checkMarker = async (exists, latitude) => {
      await userPage.getByRole('button',{name:'刷新地图',exact:true}).click();
      await userPage.waitForFunction(async ({id,exists,latitude}) => {
        const {adapter} = await CityFlowMapReady, marker = adapter.getMarker(`shop:${id}`);
        return !!marker === exists && (!exists || latitude === undefined || Math.abs(marker.getLatLng().lat - latitude) < 0.00001);
      },{id:original.id,exists,latitude});
    };
    await checkMarker(true,original.latitude);
    await page.getByRole('button',{name:'编辑门店',exact:true}).click();
    await page.locator('#merchantMap').click({position:{x:210,y:210}});
    const draftLat = Number(await form.locator('[name=latitude]').inputValue());
    assert.notEqual(draftLat,original.latitude);
    await form.getByRole('button',{name:'取消',exact:true}).click();
    assert.equal((await api(merchant,'/api/v1/merchants/me')).latitude,original.latitude);
    await page.getByRole('button',{name:'编辑门店',exact:true}).click();
    await form.locator('[name=name]').fill('地图商家编辑回归'); await form.locator('[name=latitude]').fill(String(original.latitude+0.001));
    await form.locator('[name=business_status]').selectOption('PAUSED'); await form.locator('[type=submit]').click(); await page.locator('#merchantEditor').waitFor({state:'hidden'});
    await checkMarker(false);
    await page.getByRole('button',{name:'编辑门店',exact:true}).click(); await form.locator('[name=business_status]').selectOption('OPEN'); await form.locator('[type=submit]').click(); await page.locator('#merchantEditor').waitFor({state:'hidden'});
    await checkMarker(true,original.latitude+0.001);
    await page.locator('#merchantMap').scrollIntoViewIfNeeded(); await page.screenshot({path:path.join(output,`desktop-${config.provider}.png`),fullPage:true});
    await page.setViewportSize({width:390,height:844}); await page.waitForTimeout(1000); assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth+1));
    await page.screenshot({path:path.join(output,`mobile-${config.provider}.png`),fullPage:true});
    page.on('dialog',d => d.accept()); await page.getByRole('button',{name:'归档门店资料'}).click(); await page.getByRole('button',{name:'新增门店',exact:true}).waitFor();
    await checkMarker(false);
    await page.getByRole('button',{name:'新增门店',exact:true}).click();
    await form.locator('[name=name]').fill('重新建立门店'); await form.locator('[name=address]').fill('重建地址'); await form.locator('[name=latitude]').fill(String(original.latitude)); await form.locator('[name=longitude]').fill(String(original.longitude)); await form.locator('[type=submit]').click(); await page.locator('#merchantEditor').waitFor({state:'hidden'});
    assert.equal((await api(merchant,'/api/v1/merchants/me')).id,original.id);
    await checkMarker(true,original.latitude);
    const forbidden = await context.request.post(base+'/api/v1/merchants/me',{headers:{Authorization:`Bearer ${user}`},data:{}}); assert.equal(forbidden.status(),403);
    assert.equal(await userPage.locator('.map-merchant-manager').count(),0);
    assert.deepEqual(errors,[]); console.log(`PASS: merchant self CRUD, map picking/search, cancel draft, visibility, archive/recreate, role checks and mobile (${config.provider}). Screenshots: ${output}`);
  } finally {
    if (merchant) await api(merchant,'/api/v1/merchants/me','DELETE').catch(()=>{});
    if (account) await api(admin,`/api/v1/platform/users/${account.id}`,'DELETE').catch(()=>{});
    await browser.close();
  }
})().catch(error => {console.error(error);process.exitCode=1});
