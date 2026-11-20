// Real Amap integration: run against a disposable seeded server configured with Amap keys.
const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const path = require('node:path');
const fs = require('node:fs');
const base = process.env.CITYFLOW_TEST_URL;
if (!base) throw new Error('Set CITYFLOW_TEST_URL');
const output = path.join(require('node:os').tmpdir(), 'cityflow-amap-smoke');
fs.mkdirSync(output, { recursive: true });
(async () => {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1100 } });
  const errors = [];
  context.on('page', page => page.on('pageerror', error => errors.push(error.message)));
  const api = async (token, endpoint, method = 'GET', data) => {
    const response = await context.request.fetch(base + endpoint, { method, headers: token ? { Authorization: `Bearer ${token}` } : {}, data });
    assert(response.ok(), `${endpoint}: ${response.status()} ${await response.text()}`);
    return response.json();
  };
  const login = async role => (await api(null, '/api/v1/auth/login', 'POST', { email: `${role}@cityflow.local`, password: 'cityflow123' })).access_token;
  let task, user;
  try {
    user = await login('user'); const rider = await login('rider');
    const config = await api(user, '/api/v1/map/config');
    assert.equal(config.provider, 'amap'); assert(!JSON.stringify(config).includes('amap_web_service_key'));
    const unauthorized = await context.request.get(base + '/api/v1/map/search?keywords=上海'); assert.equal(unauthorized.status(), 401);
    const invalid = await context.request.post(base + '/api/v1/map/riding', { headers: { Authorization: `Bearer ${user}` }, data: { origin: { latitude: 31, longitude: 121 }, destination: { latitude: 32, longitude: 121 } } }); assert.equal(invalid.status(), 400);
    const page = await context.newPage();
    page.on('console', m => { if (m.type() === 'error') console.log('browser:', m.text()); });
    await page.goto(base + '/login'); await page.evaluate(t => sessionStorage.setItem('cityflow_token', t), user);
    await page.goto(base + '/user');
    assert(await page.evaluate(async () => !!(await CityFlowMapReady)?.map.raw), 'Native Amap must initialize');
    await page.waitForSelector('.amap-layer');
    const search = async (keyword, target) => {
      await page.locator('.map-search-form input[name=keywords]').fill(keyword);
      await page.locator('.map-search-form input[name=city]').fill('上海');
      await page.locator('.map-search-form select').selectOption(target);
      await page.locator('.map-search-form button').click();
      await page.locator('.map-place-result').first().waitFor({ timeout: 30000 });
      await page.locator('.map-place-result').first().click();
    };
    await search('人民广场', 'pickup'); await search('南京东路地铁站', 'destination');
    const points = await page.evaluate(() => CityFlowTaskPoints);
    assert(points.pickup && points.destination);
    const roundtrip = await page.evaluate(() => {
      const point = { latitude: 31.2304, longitude: 121.4737 };
      const [lng, lat] = CityFlowCoordinates.toAmap(point); const back = CityFlowCoordinates.fromAmap(lng, lat);
      return Math.abs(back.latitude - point.latitude) + Math.abs(back.longitude - point.longitude);
    }); assert(roundtrip < 1e-8);
    await page.locator('[data-route]').click();
    await page.waitForFunction(() => document.querySelector('.map-route-summary').textContent.includes('高德骑行 ·'), { timeout: 30000 });
    assert(await page.locator('.map-route-summary li').count() > 0);
    console.log(await page.locator('.map-route-summary').innerText());
    await page.locator('#cityMap').scrollIntoViewIfNeeded(); await page.waitForTimeout(2500);
    await page.screenshot({ path: path.join(output, 'desktop.png'), fullPage: true });
    task = await api(user, '/api/v1/tasks', 'POST', { title: '高德地图回归', type: 'DELIVERY', pickup: '人民广场', destination: '南京东路', pickup_location: points.pickup, destination_location: points.destination });
    assert(Math.abs(task.pickup_location.latitude - points.pickup.latitude) < 1e-10);
    await api(rider, `/api/v1/tasks/${task.id}/claim`, 'POST', {});
    const report = latitude => api(rider, '/api/v1/riders/me/location', 'POST', { latitude, longitude: points.pickup.longitude, accuracy: 10, observed_at: new Date().toISOString() });
    await report(points.pickup.latitude); await page.getByRole('button', { name: '刷新地图', exact: true }).click();
    await page.locator(`#trackedTask option[value="${task.id}"]`).waitFor({ state: 'attached' }); await page.selectOption('#trackedTask', task.id);
    await page.waitForFunction(async () => !!(await CityFlowMapReady).adapter.getMarker('tracked:rider'));
    await page.getByRole('button', { name: '跟随骑手', exact: true }).click(); await page.waitForTimeout(3100);
    await report(points.pickup.latitude + 0.002);
    await page.waitForFunction(async lat => { const { adapter } = await CityFlowMapReady; return Math.abs(adapter.getMarker('tracked:rider').getLatLng().lat - lat) < 0.00001; }, points.pickup.latitude + 0.002);
    await page.waitForFunction(async lat => Math.abs((await CityFlowMapReady).map.getCenter().lat - lat) < 0.0001, points.pickup.latitude + 0.002);
    const layer = page.locator('.amap-layer-controls label').filter({ hasText: '骑手' }).locator('input'); await layer.uncheck(); await layer.check();
    await page.setViewportSize({ width: 390, height: 844 }); await page.waitForTimeout(1000);
    await page.evaluate(async () => (await CityFlowMapReady).map.invalidateSize());
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'Mobile overflow');
    await page.screenshot({ path: path.join(output, 'mobile.png'), fullPage: true });
    await api(user, `/api/v1/tasks/${task.id}`, 'DELETE'); task = null;
    await page.getByRole('button', { name: '刷新地图', exact: true }).click();
    await page.waitForFunction(async () => !(await CityFlowMapReady).adapter.getMarker('tracked:rider'));
    await page.getByRole('button', { name: '清除选点', exact: true }).click();
    assert.equal(await page.locator('.map-route-summary').innerText(), '');
    await page.selectOption('#pickMapPoint', 'pickup');
    await page.locator('#cityMap').click({ position: { x: 100, y: 180 } });
    assert(await page.evaluate(() => !!CityFlowTaskPoints.pickup && !CityFlowTaskPoints.destination));
    for (const [role, route] of [['rider', '/rider'], ['admin', '/admin']]) {
      const token = await login(role), other = await context.newPage();
      await other.goto(base + '/login'); await other.evaluate(t => sessionStorage.setItem('cityflow_token', t), token);
      await other.goto(base + route);
      assert(await other.evaluate(async () => !!(await CityFlowMapReady)?.map.raw), `${role} native map`);
      assert.equal(await other.locator('.map-search-form select option').count(), 1);
      await other.close();
    }
    assert.deepEqual(errors, []); console.log('Amap real search, cycling, WGS84 persistence, live movement, cancellation and mobile checks passed. Screenshots:', output);
  } finally {
    if (task) await api(user, `/api/v1/tasks/${task.id}`, 'DELETE').catch(() => {});
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
