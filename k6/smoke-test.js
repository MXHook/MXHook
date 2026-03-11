// k6/smoke-test.js — basic smoke test (10 VUs, 30s)
// Run: k6 run k6/smoke-test.js -e BASE_URL=https://mxhook.dev -e API_KEY=<key>

import http from "k6/http";
import { check, sleep } from "k6";
import { Rate } from "k6/metrics";

const errorRate = new Rate("errors");

export const options = {
  vus: 10,
  duration: "30s",
  thresholds: {
    http_req_duration: ["p(95)<500"],
    errors: ["rate<0.01"],
  },
};

const BASE_URL = __ENV.BASE_URL || "http://localhost:8787";
const API_KEY  = __ENV.API_KEY  || "test-key";

const headers = { "X-API-Key": API_KEY, "Content-Type": "application/json" };

export function setup() {
  // Verify the service is up before running
  const res = http.get(`${BASE_URL}/health`);
  if (res.status !== 200) {
    throw new Error(`Health check failed: ${res.status}`);
  }
}

export default function () {
  // GET /status
  const statusRes = http.get(`${BASE_URL}/status`);
  check(statusRes, {
    "status 200": r => r.status === 200,
    "has components": r => JSON.parse(r.body).components !== undefined,
  }) || errorRate.add(1);

  // GET /api/v1/messages (requires auth)
  const msgsRes = http.get(`${BASE_URL}/api/v1/messages?limit=10`, { headers });
  check(msgsRes, {
    "messages 200 or 401": r => r.status === 200 || r.status === 401,
  }) || errorRate.add(1);

  // GET /api/v1/domains
  const domsRes = http.get(`${BASE_URL}/api/v1/domains`, { headers });
  check(domsRes, {
    "domains 200 or 401": r => r.status === 200 || r.status === 401,
  }) || errorRate.add(1);

  sleep(0.5);
}
