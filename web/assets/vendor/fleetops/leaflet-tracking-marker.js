/*
 * Adapted from Fleetbase Fleet-Ops, copyright Fleetbase Pte Ltd and contributors.
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Upstream: fleetbase/fleetops@811aa711a68833dfd8390d8ddac582d4ed75f3c6
 * Original: addon/components/leaflet-tracking-marker.js
 * CityFlow modifications (2026-11-08): remove Ember wrapper and legacy draw/IE
 * compatibility; cancel superseded/removed animations; preserve map interaction;
 * follow via move events; rotate a heading element; reset zero-degree rotation.
 * See LICENSE.md and SOURCE.md in this directory.
 */
(function () {
  'use strict';
const arrayFromLatLng = (latlng) => {
    if (Array.isArray(latlng)) {
        return latlng;
    }

    const latitude = latlng.lat;
    const longitude = latlng.lng;

    return [latitude, longitude];
};

const L = window.leaflet || window.L;
L.CityFlowTrackingMarker = L.Marker.extend({
    options: {
        bearingAngle: 0,
        rotationOrigin: '',
    },

    initialize: function (latlng, options = {}) {
        L.Marker.prototype.initialize.call(this, latlng, options);

        L.Util.setOptions(this, options);
        this._latlng = L.latLng(latlng);

        var duration = options.duration || 2000;
        var iconOptions = this.options.icon && this.options.icon.options;
        var iconAnchor = iconOptions && this.options.icon.options.iconAnchor;
        if (iconAnchor) {
            iconAnchor = iconAnchor[0] + 'px ' + iconAnchor[1] + 'px';
        }

        this.options.duration = duration;
        this.options.rotationOrigin = this.options.rotationOrigin || iconAnchor || 'center';
        this.options.bearingAngle = this.options.bearingAngle || 0;

        // Ensure marker keeps rotated during dragging
        this.on('drag', function (e) {
            e.target._applyRotation();
        });

        this.on('dragstart', this.slideCancel, this);

        this._slideToUntil = 0;
        this._slideToDuration = duration;
        this._slideToLatLng = [0, 0];
        this._slideFromLatLng = [0, 0];
        this._slideFrame = 0;
    },

    slideTo: function (latlng, options = {}) {
        if (!this._map) return this;
        this.slideCancel();

        // Convert latlng to L.LatLng
        latlng = L.latLng(latlng);

        const duration = options.duration ?? this.options.duration;
        if (!Number.isFinite(duration) || duration <= 0) { this.setLatLng(latlng); return this; }
        this._slideToDuration = duration;
        this._slideToUntil = performance.now() + duration;
        this._slideFromLatLng = this.getLatLng();
        this._previousPosition = arrayFromLatLng(this._slideFromLatLng);
        this._slideToLatLng = latlng;
        this._nextPosition = arrayFromLatLng(latlng);
        this.fire('movestart');
        this._slideTo();

        return this;
    },

    _slideTo: function () {
        if (!this._map) return;

        var remaining = this._slideToUntil - performance.now();

        if (remaining <= 0) {
            this._slideFrame = 0;
            this.setLatLng(this._slideToLatLng);
            this.fire('moveend');
            return this;
        }

        var startPoint = this._map.latLngToContainerPoint(this._slideFromLatLng);
        var endPoint = this._map.latLngToContainerPoint(this._slideToLatLng);
        var percentDone = (this._slideToDuration - remaining) / this._slideToDuration;

        var currPoint = endPoint.multiplyBy(percentDone).add(startPoint.multiplyBy(1 - percentDone));
        var currLatLng = this._map.containerPointToLatLng(currPoint);
        this.setLatLng(currLatLng);

        this._slideFrame = L.Util.requestAnimFrame(this._slideTo, this);
    },

    // 🍂method slideCancel(): this
    // Cancels the sliding animation from `slideTo`, if applicable.
    slideCancel: function () {
        L.Util.cancelAnimFrame(this._slideFrame);
        this._slideFrame = 0;
    },

    onRemove: function (map) {
        const pendingPosition = this._slideFrame ? this._slideToLatLng : null;
        this.slideCancel();
        L.Marker.prototype.onRemove.call(this, map);
        // A hidden layer must reappear at the received position, not halfway there.
        if (pendingPosition) this._latlng = L.latLng(pendingPosition);
    },

    _setPos: function (pos) {
        L.Marker.prototype._setPos.call(this, pos);
        this._applyRotation();
    },

    _applyRotation: function () {
        if (!this._icon) return;
        const target = this._icon.querySelector('.cf-heading') || this._icon;
        target.style[L.DomUtil.TRANSFORM + 'Origin'] = this.options.rotationOrigin;
        if (target === this._icon) {
            // Leaflet's _setPos resets the translation before this method runs.
            target.style[L.DomUtil.TRANSFORM] += ' rotateZ(' + this.options.bearingAngle + 'deg)';
        } else {
            target.style[L.DomUtil.TRANSFORM] = 'rotateZ(' + this.options.bearingAngle + 'deg)';
        }
    },

    setRotationAngle: function (angle) {
        this.options.bearingAngle = angle;
        this.update();
        return this;
    },

    setRotationOrigin: function (origin) {
        this.options.rotationOrigin = origin;
        this.update();
        return this;
    },
});

// Adapted from the upstream computeBearing helper. Use spherical bearing so
// north/south motion and longitude wrapping work as well as diagonal motion.
window.CityFlowMapBearing = (previous, next) => {
    const toRadians = value => value * Math.PI / 180;
    const latitude1 = toRadians(previous.lat), latitude2 = toRadians(next.lat);
    const longitude = toRadians(next.lng - previous.lng);
    const y = Math.sin(longitude) * Math.cos(latitude2);
    const x = Math.cos(latitude1) * Math.sin(latitude2) - Math.sin(latitude1) * Math.cos(latitude2) * Math.cos(longitude);
    return (Math.atan2(y, x) * 180 / Math.PI + 360) % 360;
};
})();
