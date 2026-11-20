/*
 * Adapted from Fleetbase Fleet-Ops, copyright Fleetbase Pte Ltd and contributors.
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Upstream: fleetbase/fleetops@811aa711a68833dfd8390d8ddac582d4ed75f3c6
 * Original: addon/services/map-adapter/leaflet.js (marker methods).
 * CityFlow modifications (2026-11-08): standalone class, supplied map and layer
 * groups, TrackingMarker construction, replacement/removal cancellation,
 * reduced-motion support and explicit disposal. See SOURCE.md and LICENSE.md.
 */
(function () {
  'use strict';
  const L = window.L;
  window.CityFlowLeafletAdapter = class {
    constructor(map) { this._map = map; this._markers = new Map(); }
    get markers() { return this._markers; }
    getMarker(id) { return this._markers.get(id); }
    destroy() {
      for (const id of this._markers.keys()) this.removeMarker(id);
      this._map = null;
    }
    addMarker(id, lat, lng, options = {}) {
        if (!this._map) return null;

        const iconOptions = options.iconUrl
            ? L.icon({
                  iconUrl: options.iconUrl,
                  iconSize: options.iconSize ?? [24, 24],
                  iconAnchor: options.iconAnchor ?? [12, 12],
                  ...options.iconOptions,
              })
            : (options.icon ?? undefined);

        const markerOptions = {
            title: options.title,
            alt: options.alt,
            draggable: options.draggable ?? false,
            rotationAngle: options.rotationAngle ?? 0,
            zIndexOffset: options.zIndexOffset ?? 0,
        };

        if (iconOptions) {
            markerOptions.icon = iconOptions;
        }

        this.removeMarker(id);
        const Marker = options.animated ? L.CityFlowTrackingMarker : L.Marker;
        const marker = new Marker([lat, lng], markerOptions).addTo(options.layer || this._map);
        marker.cityFlowLayer = options.layer || this._map;

        if (options.popup) {
            marker.bindPopup(options.popup);
        }

        if (options.tooltip) {
            marker.bindTooltip(options.tooltip, options.tooltipOptions ?? {});
        }

        if (typeof options.onClick === 'function') {
            marker.on('click', options.onClick);
        }

        this._markers.set(id, marker);
        return marker;
    }

    updateMarkerPosition(id, lat, lng, animated = true, duration = 500) {
        const marker = this._markers.get(id);
        if (!marker) return;

        if (animated && marker._map && !window.matchMedia('(prefers-reduced-motion: reduce)').matches && typeof marker.slideTo === 'function') {
            marker.slideTo([lat, lng], { duration });
        } else {
            marker.slideCancel?.();
            marker.setLatLng([lat, lng]);
        }
    }

    setMarkerRotation(id, degrees) {
        const marker = this._markers.get(id);
        if (!marker) return;
        if (typeof marker.setRotationAngle === 'function') {
            marker.setRotationAngle(degrees);
        }
    }

    removeMarker(id) {
        const marker = this._markers.get(id);
        if (marker) {
            marker.slideCancel?.();
            marker.cityFlowLayer.removeLayer(marker);
            this._markers.delete(id);
        }
    }

  };
})();
