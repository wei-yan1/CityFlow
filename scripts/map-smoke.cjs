// Run against a disposable, seeded memory-mode server:
// CITYFLOW_TEST_URL=http://127.0.0.1:18081 node scripts/map-smoke.cjs
// Requires Playwright and its Chromium browser. Creates and cancels one test task.
const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const path = require('node:path');
const base = process.env.CITYFLOW_TEST_URL;
if (!base) throw new Error('Set CITYFLOW_TEST_URL to a disposable, seeded CityFlow server.');
const output = process.env.CITYFLOW_TEST_OUTPUT || path.join(require('node:os').tmpdir(), 'cityflow-map-smoke');
fs.mkdirSync(output, { recursive: true });

(async () => {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const errors = [];
  context.on('page', page => page.on('pageerror', error => errors.push(error.message)));
  const api = async (token, endpoint, method = 'GET', data) => {
    const response = await context.request.fetch(base + endpoint, {
      method, headers: token ? { Authorization: `Bearer ${token}` } : {}, data,
    });
    assert(response.ok(), `${method} ${endpoint}: ${response.status()} ${await response.text()}`);
    return response.json();
  };
  const login = async role => (await api(null, '/api/v1/auth/login', 'POST', { email: `${role}@cityflow.local`, password: 'cityflow123' })).access_token;
  let task, user;
  try {
    user = await login('user');
    const rider = await login('rider');
    const admin = await login('admin');
    const open = async (token, route) => {
      const page = await context.newPage();
      await page.goto(base + '/login');
      await page.evaluate(token => sessionStorage.setItem('cityflow_token', token), token);
      await page.goto(base + route);
      await page.evaluate(() => window.CityFlowMapReady);
      return page;
    };
    let failTiles = true;
    await context.route('https://tile.openstreetmap.org/**', route => failTiles ? route.abort() : route.continue());
    const page = await open(user, '/user');
    await page.waitForFunction(() => document.querySelector('.map-tile-notice')?.hidden === false);
    failTiles = false;
    await page.getByRole('button', { name: '重试底图' }).click();
    await page.waitForFunction(() => [...document.querySelectorAll('.leaflet-tile')].some(tile => tile.complete && tile.naturalWidth > 0), { timeout: 30000 });
    await page.waitForFunction(() => document.querySelector('.map-tile-notice').hidden, { timeout: 30000 });

    await page.selectOption('#pickMapPoint', 'pickup');
    await page.locator('#cityMap').click({ position: { x: 180, y: 150 } });
    await page.selectOption('#pickMapPoint', 'destination');
    await page.locator('#cityMap').click({ position: { x: 280, y: 200 } });
    assert.equal(await page.locator('.map-point-summary').innerText().then(text => text.includes('未选点')), false);
    await page.getByRole('button', { name: '清除选点' }).click();
    assert.equal(await page.evaluate(() => CityFlowTaskPoints.pickup), null);
    await page.selectOption('#pickMapPoint', 'pickup');
    await page.locator('#cityMap').click({ position: { x: 180, y: 150 } });
    await page.selectOption('#pickMapPoint', 'destination');
    await page.locator('#cityMap').click({ position: { x: 280, y: 200 } });
    await page.selectOption('#pickMapPoint', '');
    const points = await page.evaluate(() => CityFlowTaskPoints);
    task = await api(user, '/api/v1/tasks', 'POST', { title: '地图浏览器回归', type: 'DELIVERY', pickup: '测试取件点', destination: '测试送达点', pickup_location: points.pickup, destination_location: points.destination });
    await api(rider, `/api/v1/tasks/${task.id}/claim`, 'POST', {});
    const report = async latitude => api(rider, '/api/v1/riders/me/location', 'POST', { latitude, longitude: points.pickup.longitude, accuracy: 10, observed_at: new Date().toISOString() });
    await report(points.pickup.latitude);
    await page.getByRole('button', { name: '刷新地图', exact: true }).click();
    await page.locator(`#trackedTask option[value="${task.id}"]`).waitFor({ state: 'attached' });
    await page.selectOption('#trackedTask', task.id);
    await page.waitForFunction(() => [...document.querySelectorAll('.map-live-status')].some(el=>el.textContent.includes('骑手位置更新于')));
    const follow = page.getByRole('button', { name: '跟随骑手', exact: true });
    await follow.click();
    assert.equal(await follow.getAttribute('aria-pressed'), 'true');
    await new Promise(resolve => setTimeout(resolve, 3100));
    const next = await report(points.pickup.latitude + 0.002);
    await page.waitForFunction(async latitude => {
      const { map } = await window.CityFlowMapReady;
      return Math.abs(map.getCenter().lat - latitude) < 0.0001;
    }, next.latitude);
    await page.getByRole('button', { name: '查看全部点位' }).click();
    assert.equal(await follow.getAttribute('aria-pressed'), 'false');
    await page.locator('.leaflet-control-layers').hover();
    const shops = page.locator('.leaflet-control-layers label').filter({ hasText: '商家' }).locator('input');
    await shops.uncheck(); await shops.check();

    const platform = await open(admin, '/admin');
    const countRiders = () => platform.evaluate(async () => {
      let count = 0;
      const { map } = await window.CityFlowMapReady;
      map.eachLayer(layer => { if (layer.cityFlowKind === 'rider') count++; });
      return count;
    });
    assert(await countRiders() >= 1, 'platform must show nearby rider');
    await platform.selectOption('#trackedTask', task.id);
    await platform.waitForFunction(() => [...document.querySelectorAll('.map-live-status')].some(el=>el.textContent.includes('骑手位置更新于')));
    await platform.selectOption('#trackedTask', '');
    assert(await countRiders() >= 1, 'stopping tracking must preserve platform riders');
    await page.screenshot({ path: path.join(output, 'map-desktop.png'), fullPage: true });
    await page.setViewportSize({ width: 390, height: 844 });
    await page.evaluate(async () => (await window.CityFlowMapReady).map.invalidateSize());
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth), 'mobile layout overflows');
    await page.screenshot({ path: path.join(output, 'map-mobile.png'), fullPage: true });
    await api(user, `/api/v1/tasks/${task.id}`, 'DELETE'); task = null;
    await page.waitForFunction(() => !document.querySelector('#trackedTask').value);
    assert.equal(await follow.isDisabled(), true);
    assert.deepEqual(errors, []);
    console.log(`PASS: real tiles, retry, picking/reset, REST/WebSocket tracking, following, layers, platform isolation, cancellation, mobile. Screenshots: ${output}`);
  } finally {
    if (task && user) await api(user, `/api/v1/tasks/${task.id}`, 'DELETE').catch(console.error);
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
