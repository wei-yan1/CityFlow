// Creates a simulated rider observation sequence in a disposable seeded server.
// Real MySQL/Redis/RabbitMQ and external road matching are used when configured by that server.
const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const fs = require('node:fs'), path = require('node:path');
const base = process.env.CITYFLOW_TEST_URL;
if (!base) throw new Error('Set CITYFLOW_TEST_URL');
const output = path.join(require('node:os').tmpdir(), 'cityflow-history-smoke');
fs.mkdirSync(output, { recursive: true });
(async () => {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1440, height: 1000 } });
  const errors = []; context.on('page', p => p.on('pageerror', e => errors.push(e.message)));
  const api = async (token, endpoint, method = 'GET', data) => {
    const r = await context.request.fetch(base + endpoint, { method, headers: token ? { Authorization: `Bearer ${token}` } : {}, data });
    assert(r.ok(), `${endpoint}: ${r.status()} ${await r.text()}`); return r.json();
  };
  const login = async role => (await api(null, '/api/v1/auth/login', 'POST', { email: `${role}@cityflow.local`, password: 'cityflow123' })).access_token;
  let task, user, cancelled = false;
  try {
    user = await login('user'); const rider = await login('rider'), merchant = await login('merchant');
    const coords = [[121.469378,31.230442],[121.469195,31.230646],[121.469031,31.230906],[121.468930,31.230982],[121.469247,31.231202],[121.469668,31.231480],[121.469914,31.231642],[121.470160,31.231804],[121.470296,31.231894]];
    task = await api(user, '/api/v1/tasks', 'POST', { title: '骑手轨迹模拟回归', type: 'DELIVERY', pickup: '模拟起点', destination: '模拟终点' });
    await api(rider, `/api/v1/tasks/${task.id}/claim`, 'POST', {});
    for (const [longitude, latitude] of coords) {
      await new Promise(r => setTimeout(r, 3200));
      await api(rider, '/api/v1/riders/me/location', 'POST', { latitude, longitude, accuracy: 8, observed_at: new Date().toISOString() });
    }
    await api(user, `/api/v1/tasks/${task.id}`, 'DELETE'); cancelled = true;
    const raw = await api(user, `/api/v1/tasks/${task.id}/history?limit=500`);
    assert.equal(raw.data.length, coords.length); assert.equal(raw.coordinate_system, 'WGS84');
    const denied = await context.request.get(base + `/api/v1/tasks/${task.id}/history?display=true`, { headers: { Authorization: `Bearer ${merchant}` } }); assert.equal(denied.status(), 403);
    const page = await context.newPage(); await page.goto(base + '/login'); await page.evaluate(t => sessionStorage.setItem('cityflow_token', t), user); await page.goto(base + '/user');
    assert(await page.evaluate(async () => !!(await CityFlowMapReady)?.map));
    await page.selectOption('.map-history select', task.id);
    let display;
    page.on('response', async response => { if (response.url().includes(`/tasks/${task.id}/history?`) && response.ok()) display = await response.json(); });
    await page.locator('[data-history-load]').click();
    await page.waitForFunction(() => document.querySelector('.map-history p').textContent.includes('已读取 9 个'), { timeout: 30000 });
    assert(display?.display?.segments.length > 0);
    assert(display.display.segments.some(s => s.mode === 'road_matched'), JSON.stringify(display.display));
    console.log(JSON.stringify({ points: raw.data.length, segments: display.display.segments.map(s => ({ mode: s.mode, confidence: s.confidence, coordinates: s.coordinates.length })), notice: display.display.notice }));
    await page.locator('#cityMap').scrollIntoViewIfNeeded(); await page.waitForTimeout(1500);
    await page.locator('.map-workspace').screenshot({ path: path.join(output, 'desktop.png') });
    await page.setViewportSize({ width: 390, height: 844 }); await page.waitForTimeout(1000);
    assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1));
    await page.locator('.map-workspace').screenshot({ path: path.join(output, 'mobile.png') });
    await page.locator('[data-history-clear]').click(); assert.equal(await page.locator('.map-history p').innerText(), '已清除地图上的轨迹显示；服务端记录仍保留，可重新查看。');
    assert.deepEqual(errors, []); console.log('PASS: recorded rider observations, task scope, terminal history, real bicycle matching, desktop/mobile display. Task retained for restart verification:', task.id, 'Screenshots:', output);
  } finally {
    if (task && !cancelled) await api(user, `/api/v1/tasks/${task.id}`, 'DELETE').catch(() => {});
    await browser.close();
  }
})().catch(error => { console.error(error); process.exitCode = 1; });
