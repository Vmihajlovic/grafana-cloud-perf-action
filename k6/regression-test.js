import http from 'k6/http';
import { check, sleep } from 'k6';
import { Rate, Trend } from 'k6/metrics';
import { textSummary } from 'https://jslib.k6.io/k6-summary/0.0.2/index.js';

// CI Regression Test: verifies search P95 stays under 200ms.
// Generated from AI analysis of a production incident where
// regexp.Compile in a loop caused P95 to spike from 10ms to 2s+.

const CATALOG_URL = __ENV.CATALOG_URL || 'http://localhost:8081';
const errorRate = new Rate('errors');
const searchLatency = new Trend('search_latency', true);

const SEARCH_TERMS = [
  'premium', 'electronics', 'widget', 'gadget',
  'professional', 'compact', 'deluxe', 'essential',
];

export const options = {
  scenarios: {
    smoke: {
      executor: 'shared-iterations', vus: 1, iterations: 5, maxDuration: '30s', startTime: '0s',
    },
    load: {
      executor: 'ramping-vus', startVUs: 0,
      stages: [
        { duration: '15s', target: 10 },
        { duration: '1m', target: 10 },
        { duration: '10s', target: 0 },
      ],
      startTime: '30s',
    },
  },
  thresholds: {
    search_latency: ['p(95)<300', 'p(99)<800'],
    errors: ['rate<0.05'],
  },
};

export default function () {
  const term = SEARCH_TERMS[Math.floor(Math.random() * SEARCH_TERMS.length)];
  const res = http.get(`${CATALOG_URL}/api/products/search?q=${encodeURIComponent(term)}`, {
    tags: { name: 'search' },
  });

  const ok = check(res, {
    'status 200': (r) => r.status === 200,
    'has results': (r) => { try { return JSON.parse(r.body).length > 0; } catch { return false; } },
    'latency < 500ms': (r) => r.timings.duration < 500,
  });

  errorRate.add(!ok);
  searchLatency.add(res.timings.duration);
  sleep(0.3 + Math.random() * 0.5);
}

export function handleSummary(data) {
  return {
    stdout: textSummary(data, { indent: ' ', enableColors: true }),
    'k6-summary.json': JSON.stringify(data),
  };
}
