// Regression: an active tracked task must survive falling out of the first page.
// Use a disposable seeded memory server: creates 101 tasks and cancels them afterward.
const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const base = process.env.CITYFLOW_TEST_URL;
if (!base) throw new Error('Set CITYFLOW_TEST_URL to a disposable seeded CityFlow server.');
(async () => {
  const browser = await chromium.launch();
  let user;
  const created = [];
  try {
    const context = await browser.newContext();
    const api = async (token, path, method = 'GET', data) => {
      const response = await context.request.fetch(base + path, { method, headers: token ? { Authorization: `Bearer ${token}` } : {}, data });
      assert(response.ok(), `${method} ${path}: ${response.status()} ${await response.text()}`);
      return response.json();
    };
    const login = async role => (await api(null, '/api/v1/auth/login', 'POST', { email: `${role}@cityflow.local`, password: 'cityflow123' })).access_token;
    user = await login('user'); const rider = await login('rider');
    const create = async title => {
      const task = await api(user, '/api/v1/tasks', 'POST', { title, type: 'DELIVERY', pickup: 'A', destination: 'B', pickup_location: { latitude: 31.23, longitude: 121.47 }, destination_location: { latitude: 31.24, longitude: 121.48 } });
      created.push(task.id); return task;
    };
    const target = await create('分页回归：活动任务');
    await api(rider, `/api/v1/tasks/${target.id}/claim`, 'POST', {});
    const page = await context.newPage();
    const errors = []; page.on('pageerror', error => errors.push(error.message));
    await page.goto(base + '/login');
    await page.evaluate(token => sessionStorage.setItem('cityflow_token', token), user);
    await page.goto(base + '/user'); await page.evaluate(() => CityFlowMapReady);
    await page.selectOption('#trackedTask', target.id);
    for (let i = 0; i < 10; i++) await Promise.all(Array.from({ length: 10 }, (_, j) => create(`分页回归：新任务 ${i * 10 + j}`)));
    const firstPage = await api(user, '/api/v1/tasks?limit=100&offset=0');
    assert(!firstPage.data.some(task => task.id === target.id), 'test target must be outside the first page');
    await page.evaluate(async () => (await CityFlowMapReady).refresh());
    assert.equal(await page.locator('#trackedTask').inputValue(), target.id);
    assert.equal((await api(user, `/api/v1/tasks/${target.id}`)).status, 'CLAIMED');
    assert(await page.evaluate(async taskID => (await CityFlowMapReady).adapter.markers.has(`task:${taskID}:pickup`), target.id));
    // A transient status request failure must also preserve the selected task.
    await page.route(`**/api/v1/tasks/${target.id}`, route => route.fulfill({ status: 503, contentType: 'application/json', body: '{"error":"temporary outage"}' }));
    await page.evaluate(async () => (await CityFlowMapReady).refresh());
    assert.equal(await page.locator('#trackedTask').inputValue(), target.id);
    await page.unroute(`**/api/v1/tasks/${target.id}`);
    await api(user, `/api/v1/tasks/${target.id}`, 'DELETE');
    created.splice(created.indexOf(target.id), 1);
    await page.waitForTimeout(100);
    await page.evaluate(async () => (await CityFlowMapReady).refresh());
    await page.waitForFunction(() => document.querySelector('#trackedTask').value === '');
    assert.deepEqual(errors, []);
    console.log('PASS: 100 newer tasks preserve active tracking and points, transient failure preserves selection, confirmed cancellation stops tracking.');
    await page.close();
    for (let i = 0; i < created.length; i += 10) {
      await Promise.all(created.slice(i, i + 10).map(id => api(user, `/api/v1/tasks/${id}`, 'DELETE')));
    }
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
