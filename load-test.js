// Filename: load-test.js
import http from 'k6/http';
import { check, group, sleep } from 'k6';

// --- Configuration ---
// Set the base URL of your API from an environment variable or use a default.
// Example: k6 run -e BASE_URL=http://your-api.com load-test.js
const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

// --- Test Profile (Load Shape) ---
export const options = {
  scenarios: {
    // A single scenario that ramps the request rate up and down.
    wilayah_api_ramp: {
      executor: 'ramping-arrival-rate',

      // Start the test with 10 requests per second (QPS).
      startRate: 10,

      // The 'timeUnit' for our rates. '1s' is standard for QPS/RPS.
      timeUnit: '1s',

      // Pre-allocate VUs to ensure k6 can handle the peak load. Adjust if needed.
      preAllocatedVUs: 10,
      
      // Set a high ceiling for VUs in case requests take longer than expected.
      maxVUs: 1000,

      stages: [
        // 1. Ramp up from 10 QPS to 10,000 QPS over 5 minutes.
        { duration: '3m', target: 100 },

        // 2. (Optional but recommended) Hold the peak load for 1 minute to check stability.
        { duration: '1m', target: 100 },
        
        // 3. (Optional but recommended) Ramp down gracefully to 0.
        { duration: '1m', target: 0 },
      ],
    },
  },
  // Define thresholds for success criteria.
  thresholds: {
    'http_req_failed': ['rate<0.01'], // Fail the test if less than 1% of requests succeed.
    'http_req_duration': ['p(95)<500'], // 95% of requests must complete below 500ms.
  },
};

// --- API Endpoints to Test ---
// An array of test cases translated from your curl script.
const apiEndpoints = [
  { name: 'General Search (Success)', path: '/v1/search?q=jakarta' },
  { name: 'General Search (No Result)', path: '/v1/search?q=nonexistentplace123' },
  { name: 'District Search (Success)', path: '/v1/search/district?q=bandung' },
  { name: 'District Search (No Result)', path: '/v1/search/district?q=nonexistentdistrict123' },
  { name: 'Subdistrict Search (Success)', path: '/v1/search/subdistrict?q=menteng' },
  { name: 'Subdistrict Search (No Result)', path: '/v1/search/subdistrict?q=nonexistentsubdistrict123' },
  { name: 'City Search (Success)', path: '/v1/search/city?q=surabaya' },
  { name: 'City Search (No Result)', path: '/v1/search/city?q=nonexistentcity123' },
  { name: 'Province Search (Success)', path: '/v1/search/province?q=jawa%20tengah' },
  { name: 'Province Search (No Result)', path: '/v1/search/province?q=nonexistentprovince123' },
  { name: 'Search (Missing Query Param)', path: '/v1/search', expectedStatus: 400 }, // Assuming 400 for bad request
];

// --- Virtual User (VU) Code ---
export default function () {
  // Select a random endpoint for this iteration to simulate varied user behavior.
  const endpoint = apiEndpoints[Math.floor(Math.random() * apiEndpoints.length)];

  // Group requests by their name for clearer results in the summary.
  group(endpoint.name, function () {
    const res = http.get(`${BASE_URL}${endpoint.path}`);

    // Check if the request was successful based on the expected status code.
    check(res, {
      [`status is ${endpoint.expectedStatus || 200}`]: (r) => r.status === (endpoint.expectedStatus || 200),
    });
  });

  // A small sleep is good practice, though the arrival-rate executor primarily controls the load.
  sleep(1);
}