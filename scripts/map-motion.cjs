// Actual Leaflet + adapted Fleet-Ops components, no tiles/network service required.
// Requires Playwright/Chromium and CITYFLOW_TEST_URL pointing to a local server.
const { chromium } = require('playwright');
const assert = require('node:assert/strict');
const base = process.env.CITYFLOW_TEST_URL;
if (!base) throw new Error('Set CITYFLOW_TEST_URL to a local CityFlow server.');
(async () => {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage();
    const errors = [];
    page.on('pageerror', error => errors.push(error.message));
    await page.goto(base + '/login');
    await page.addStyleTag({ url: base + '/assets/vendor/leaflet/leaflet.css' });
    for (const file of ['leaflet/leaflet.js', 'fleetops/leaflet-tracking-marker.js', 'fleetops/leaflet-adapter.js']) {
      await page.addScriptTag({ url: base + '/assets/vendor/' + file });
    }
    const result = await page.evaluate(async () => {
      const check = (value, message) => { if (!value) throw new Error(message); };
      const host = document.createElement('div'); host.style.cssText = 'width:600px;height:400px'; document.body.append(host);
      const map = L.map(host, { scrollWheelZoom: false }).setView([31.23, 121.47], 14);
      const adapter = new CityFlowLeafletAdapter(map);
      const layer = L.layerGroup().addTo(map);
      const icon = L.divIcon({ html: '<i class="cf-heading"></i><span>骑</span>' });
      const marker = adapter.addMarker('rider', 31.23, 121.47, { animated: true, layer, icon });
      const initialHandlers = [map.dragging.enabled(), map.doubleClickZoom.enabled(), map.scrollWheelZoom.enabled()];
      const wait = ms => new Promise(resolve => setTimeout(resolve, ms));
      adapter.updateMarkerPosition('rider', 31.24, 121.48, true, 300);
      await wait(100);
      check(marker.getLatLng().lat > 31.23 && marker.getLatLng().lat < 31.24, 'must interpolate between positions');
      adapter.updateMarkerPosition('rider', 31.22, 121.46, true, 160);
      await wait(240);
      check(marker.getLatLng().equals([31.22, 121.46]), 'superseding motion must land at latest position');
      check(marker._slideFrame === 0, 'completed animation must release its frame');
      check(JSON.stringify(initialHandlers) === JSON.stringify([map.dragging.enabled(), map.doubleClickZoom.enabled(), map.scrollWheelZoom.enabled()]), 'animation changed map interaction settings');
      adapter.setMarkerRotation('rider', 90); adapter.setMarkerRotation('rider', 0);
      check(marker.getElement().querySelector('.cf-heading').style.transform === 'rotateZ(0deg)', 'zero bearing must reset arrow');
      check(CityFlowMapBearing(L.latLng(31, 121), L.latLng(32, 121)) === 0, 'north bearing');
      check(Math.abs(CityFlowMapBearing(L.latLng(0, 179), L.latLng(0, -179)) - 90) < 0.001, 'wrapped longitude bearing');
      adapter.updateMarkerPosition('rider', 31.26, 121.50, true, 200);
      await wait(40);
      map.removeLayer(layer);
      layer.addTo(map);
      check(marker.getLatLng().equals([31.26, 121.50]), 'hiding during animation must retain destination');
      map.removeLayer(layer);
      adapter.updateMarkerPosition('rider', 31.25, 121.49, true);
      layer.addTo(map);
      check(marker.getLatLng().equals([31.25, 121.49]), 'hidden markers must retain latest position');
      map.removeLayer(layer);
      adapter.removeMarker('rider'); layer.addTo(map);
      check(layer.getLayers().length === 0 && !adapter.getMarker('rider'), 'hidden group removal must not resurrect markers');
      const removed = adapter.addMarker('remove', 31.23, 121.47, { animated: true, layer, icon });
      adapter.updateMarkerPosition('remove', 31.24, 121.48, true, 200);
      await wait(30); adapter.removeMarker('remove');
      check(removed._slideFrame === 0, 'removal must cancel animation');
      adapter.addMarker('duplicate', 31.23, 121.47, { layer, icon });
      adapter.addMarker('duplicate', 31.24, 121.48, { layer, icon });
      check(layer.getLayers().length === 1, 'replacing ID must not leak a marker');
      adapter.destroy(); map.remove();
      check(adapter.markers.size === 0, 'dispose must clear registry');
      return true;
    });
    assert.equal(result, true);
    await page.emulateMedia({ reducedMotion: 'reduce' });
    assert(await page.evaluate(() => {
      const host = document.createElement('div'); host.style.cssText = 'width:600px;height:400px'; document.body.append(host);
      const map = L.map(host).setView([31, 121], 12), adapter = new CityFlowLeafletAdapter(map);
      const marker = adapter.addMarker('r', 31, 121, { animated: true, icon: L.divIcon() });
      adapter.updateMarkerPosition('r', 32, 122, true);
      const valid = marker.getLatLng().equals([32, 122]) && marker._slideFrame === 0;
      adapter.destroy(); map.remove(); return valid;
    }), 'reduced motion must update immediately');
    assert.deepEqual(errors, []);
    console.log('PASS: interpolation, superseded motion, bearing, hidden layers, removal, duplicate IDs, disposal, reduced motion.');
  } finally { await browser.close(); }
})().catch(error => { console.error(error); process.exitCode = 1; });
