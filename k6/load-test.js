// k6/load-test.js — ramp to 100 VUs over 5min, sustain 10min
// Run: k6 run k6/load-test.js -e BASE_URL=https://mxhook.dev -e API_KEY=<key>

import http from "k6/http";
import { check, sleep } from "k6";
import { Rate, Trend } from "k6/metrics";

const errorRate      = new Rate("errors");
const msgListLatency = new Trend("msg_list_latency");
const domListLatency = new Trend("dom_list_latency");

export const options = {
  stages: [
    { duration: "1m",  target: 20  }, // warm-up
    { duration: "2m",  target: 60  }, // ramp
    { duration: "2m",  target: 100 }, // ramp to peak
    { duration: "10m", target: 100 }, // sustain
    { duration: "1m",  target: 0   }, // cool-down
  ],
  thresholds: {
    http_req_duration:  ["p(95)<800", "p(99)<1500"],
    errors:             ["rate<0.02"],
    msg_list_latency:   ["p(95)<600"],
    dom_list_latency:   ["p(95)<400"],
  },
};

const BASE_URL = __ENV.BASE_URL || "http://localhost:8787";
const API_KEY  = __ENV.API_KEY  || "test-key";

const headers = { "X-API-Key": API_KEY, "Content-Type": "application/json" };

export function setup() {
  const res = http.get(`${BASE_URL}/health`);
  if (res.status !== 200) throw new Error(`Health check failed: ${res.status}`);
}

export default function () {
  const scenario = Math.random();

  if (scenario < 0.4) {
    // Message list — most common read path
    const res = http.get(`${BASE_URL}/api/v1/messages?limit=50`, { headers });
    msgListLatency.add(res.timings.duration);
    check(res, { "messages ok": r => r.status === 200 || r.status === 401 }) || errorRate.add(1);

  } else if (scenario < 0.6) {
    // Domain list
    const res = http.get(`${BASE_URL}/api/v1/domains`, { headers });
    domListLatency.add(res.timings.duration);
    check(res, { "domains ok": r => r.status === 200 || r.status === 401 }) || errorRate.add(1);

  } else if (scenario < 0.75) {
    // Billing usage
    const res = http.get(`${BASE_URL}/api/v1/billing/usage`, { headers });
    check(res, { "billing ok": r => r.status === 200 || r.status === 401 }) || errorRate.add(1);

  } else if (scenario < 0.85) {
    // Public status page
    const res = http.get(`${BASE_URL}/status`);
    check(res, { "status ok": r => r.status === 200 || r.status === 503 }) || errorRate.add(1);

  } else {
    // Health check
    const res = http.get(`${BASE_URL}/health`);
    check(res, { "health ok": r => r.status === 200 }) || errorRate.add(1);
  }

  // Think time: 0.1–0.5s randomized
  sleep(0.1 + Math.random() * 0.4);
}
