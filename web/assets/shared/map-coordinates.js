/* Map boundary only: existing CityFlow persistence and position reports use WGS84. */
window.CityFlowCoordinates = {
  toAmap(point) { return coordtransform.wgs84togcj02(point.longitude, point.latitude); },
  fromAmap(longitude, latitude) {
    // Refine the upstream inverse against its forward transform to avoid accumulating
    // error when a point is selected, persisted, and displayed again.
    let [lng, lat] = coordtransform.gcj02towgs84(longitude, latitude);
    for (let i = 0; i < 5; i++) {
      const projected = coordtransform.wgs84togcj02(lng, lat);
      lng -= projected[0] - longitude; lat -= projected[1] - latitude;
    }
    return { longitude: lng, latitude: lat };
  },
};
