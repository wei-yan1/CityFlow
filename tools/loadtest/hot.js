import http from 'k6/http';
import { check } from 'k6';

const BASE_URL = (__ENV.BASE_URL || 'http://127.0.0.1:8087').replace(/\/+$/, '');
const CATEGORY = __ENV.CATEGORY || 'FOOD';
const MONTH = (__ENV.MONTH || '').trim();
const LIMIT = __ENV.LIMIT || '20';
const RATE = Number(__ENV.RATE || 50);
const DURATION = __ENV.DURATION || '30s';
const PRE_VUS = Number(__ENV.PRE_VUS || Math.max(10, Math.ceil(RATE / 2)));
const MAX_VUS = Number(__ENV.MAX_VUS || Math.max(50, RATE * 2));

export const options = {
  discardResponseBodies: true,
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
  scenarios: {
    hot_qps: {
      executor: 'constant-arrival-rate',
      rate: RATE,
      timeUnit: '1s',
      duration: DURATION,
      preAllocatedVUs: PRE_VUS,
      maxVUs: MAX_VUS,
      gracefulStop: '10s',
    },
  },
  thresholds: {
    http_req_failed: ['rate<0.01'],
    http_req_duration: ['p(95)<500', 'p(99)<1000'],
  },
};

export default function () {
  const query = [
    `category=${encodeURIComponent(CATEGORY)}`,
    `limit=${encodeURIComponent(LIMIT)}`,
    MONTH ? `month=${encodeURIComponent(MONTH)}` : '',
  ].filter(Boolean).join('&');
  const response = http.get(`${BASE_URL}/api/v1/merchants/hot?${query}`, {
    tags: { endpoint: 'merchants_hot' },
  });
  check(response, {
    'hot status is 200': r => r.status === 200,
  });
}
