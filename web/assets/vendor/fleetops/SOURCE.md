# Fleet-Ops map components adapted for CityFlow

Upstream: https://github.com/fleetbase/fleetops

Revision: `811aa711a68833dfd8390d8ddac582d4ed75f3c6` (retrieved 2026-11-08).

Copyright Fleetbase Pte Ltd and contributors. SPDX: AGPL-3.0-or-later.
The complete license is available in [LICENSE.md](LICENSE.md).

## Copied and adapted source

| CityFlow source | Original source at the recorded revision |
|---|---|
| [leaflet-tracking-marker.js](leaflet-tracking-marker.js) | https://github.com/fleetbase/fleetops/blob/811aa711a68833dfd8390d8ddac582d4ed75f3c6/addon/components/leaflet-tracking-marker.js |
| [leaflet-adapter.js](leaflet-adapter.js) | Marker methods from https://github.com/fleetbase/fleetops/blob/811aa711a68833dfd8390d8ddac582d4ed75f3c6/addon/services/map-adapter/leaflet.js |

These are actual source adaptations. No Ember runtime is required. The JavaScript
files served here are the editable, unminified source used by the application.

## CityFlow changes, 2026-11-08

- Extract Leaflet marker animation and adapter marker methods from the Ember
  components; remove framework imports, draw compatibility and legacy IE paths.
- Use the namespace `L.CityFlowTrackingMarker` to avoid changing `L.Marker` globally.
- Cancel superseded animation frames, external drag animations and removed markers.
- Preserve existing map interaction settings; following uses marker move events.
- Rotate a separate heading arrow, including a return to zero degrees, while keeping
  the label upright. Compute spherical bearing for cardinal and wrapped longitude moves.
- Support CityFlow layer groups and correctly remove markers from hidden groups.
- Update hidden markers without animation and honor prefers-reduced-motion.
- Explicitly dispose the marker registry; replace duplicate IDs without leaking layers.

CityFlow integration is in [map.js](/assets/shared/map.js) and
[map.css](/assets/shared/map.css). The project-level `THIRD_PARTY_NOTICES.md` lists
the integration, upstream license and redistribution/source obligations.

The local Fleetbase main ZIP supplied for this task has empty submodule directories;
the Fleet-Ops source came from the independently retrieved revision above, rather
than from those empty directories. Order allocation, company models and other
Fleet-Ops business modules were not imported.
