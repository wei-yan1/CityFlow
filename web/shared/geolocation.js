// Shared browser location API. Coordinates stay in page memory until explicitly used.
window.CityFlowLocation = (() => {
  let lastPosition = null;

  function locationError(code, message) {
    return Object.assign(new Error(message), { code });
  }

  function getCurrentPosition(options = {}) {
    if (!window.isSecureContext) {
      return Promise.reject(locationError('INSECURE_CONTEXT', '当前环境不支持定位，请手动选择地点。'));
    }
    if (!navigator.geolocation) {
      return Promise.reject(locationError('UNSUPPORTED', '当前设备不支持定位，请手动选择地点。'));
    }

    return new Promise((resolve, reject) => {
      navigator.geolocation.getCurrentPosition(
        ({ coords, timestamp }) => {
          lastPosition = Object.freeze({
            latitude: coords.latitude,
            longitude: coords.longitude,
            accuracy: coords.accuracy,
            timestamp,
          });
          resolve(lastPosition);
        },
        (error) => {
          const messages = {
            1: '未获得定位权限，请允许定位或手动选择地点。',
            2: '暂时无法获取位置，请确认定位服务已开启后重试。',
            3: '定位超时，请稍后重试，或手动选择地点。',
          };
          reject(locationError(error.code, messages[error.code] || '定位失败，请稍后重试。'));
        },
        { enableHighAccuracy: true, timeout: 10000, maximumAge: 30000, ...options }
      );
    });
  }

  // Browser coordinates are WGS84; convert explicitly before using a map provider
  // that expects another coordinate system. No reverse-geocoding is performed here.
  return Object.freeze({ getCurrentPosition, getLastPosition: () => lastPosition });
})();
