# Third-party notices

## Fleetbase Fleet-Ops map components

Copyright Fleetbase Pte Ltd and contributors.

License: GNU Affero General Public License, version 3 or later
(`AGPL-3.0-or-later`, as declared by the upstream package).

Source repository: https://github.com/fleetbase/fleetops

Revision: `811aa711a68833dfd8390d8ddac582d4ed75f3c6`.

| Upstream source | Adapted CityFlow source |
|---|---|
| `addon/components/leaflet-tracking-marker.js` | `web/assets/vendor/fleetops/leaflet-tracking-marker.js` |
| Marker methods in `addon/services/map-adapter/leaflet.js` | `web/assets/vendor/fleetops/leaflet-adapter.js` |

Adapted 2026-11-08. Modification details and original source links are recorded in
`web/assets/vendor/fleetops/SOURCE.md`; the full upstream license is preserved in
`web/assets/vendor/fleetops/LICENSE.md`. Source notices must remain with copied or
adapted code. These components are integrated by `web/assets/shared/map.js` and
the user, rider and platform HTML pages, with styles in `web/assets/shared/map.css`.

The adapted components remain under AGPL-3.0-or-later. Distribution and network use
of a modified covered work carry the corresponding AGPL source and license
obligations, including the combined work where applicable; this notice does not
grant a proprietary exception. The served JavaScript is unminified editable source,
with links to the modified component source and license in the map attribution.
When distributing or deploying a covered combined version, provide its complete
corresponding source and build/install materials as required by the license; the
upstream repository link alone does not substitute for modified source.

This entry does not import Fleet-Ops order/dispatch business modules or assert that
unrelated, independent Go source is automatically derived from Fleet-Ops.

## Leaflet

Leaflet 1.9.4 is distributed under its BSD 2-Clause license, preserved at
`web/assets/vendor/leaflet/LICENSE`. Its own copyright notice remains in that file.

## coordtransform

Copyright (c) 2015 记忆的残骸. MIT license, preserved at
`web/assets/vendor/coordtransform/LICENSE`.

Source: https://github.com/wandergis/coordtransform (upstream `index.js`, downloaded
unchanged on 2026-11-08 as `web/assets/vendor/coordtransform/coordtransform.js`).
SHA-256: `e941a1a5825c1a615eb3e986a0bfa95b5cba03f6aa89fb20cf3be33259fb897c`.
`map-coordinates.js` adds CityFlow's WGS84/GCJ02 boundary and iterative inverse
refinement. Amap JS API is loaded from the provider, not vendored; map attribution
and provider branding are retained.
