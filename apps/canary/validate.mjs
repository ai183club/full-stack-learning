import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const configUrl = new URL("./blueprint-config.json", import.meta.url);
const config = JSON.parse(await readFile(configUrl, "utf8"));

const expectedVariables = {
  webUrl: "https://full-stack.seebyte.xyz",
  apiBaseUrl:
    "https://f7a52mymmg.execute-api.ap-northeast-1.amazonaws.com",
};

const expectedSteps = {
  "1": { name: "web-home", url: "${webUrl}" },
  "2": {
    name: "api-health",
    url: "${apiBaseUrl}/health",
    status: "ok",
  },
  "3": {
    name: "api-ready",
    url: "${apiBaseUrl}/ready",
    status: "ready",
  },
};

assert.deepEqual(
  Object.keys(config).sort(),
  ["globalSettings", "steps", "variables"],
  "Only Multi-checks root fields are allowed",
);
assert.equal(config.globalSettings.stepTimeout, 30_000);
assert.deepEqual(config.variables, expectedVariables);
assert.deepEqual(Object.keys(config.steps), Object.keys(expectedSteps));

const forbiddenHeaderNames = new Set([
  "authorization",
  "cookie",
  "proxy-authorization",
  "x-api-key",
]);

for (const [key, expected] of Object.entries(expectedSteps)) {
  const step = config.steps[key];

  assert.equal(step.stepName, expected.name);
  assert.equal(step.checkerType, "HTTP");
  assert.equal(step.url, expected.url);
  assert.equal(step.httpMethod, "GET", `${step.stepName} must be read-only`);
  assert.ok(Array.isArray(step.assertions) && step.assertions.length >= 2);
  assert.ok(
    step.assertions.some(
      (assertion) =>
        assertion.type === "STATUS_CODE" &&
        assertion.operator === "EQUALS" &&
        assertion.value === 200,
    ),
    `${step.stepName} must assert status 200`,
  );

  for (const headerName of Object.keys(step.headers ?? {})) {
    assert.ok(
      !forbiddenHeaderNames.has(headerName.toLowerCase()),
      `${step.stepName} contains forbidden sensitive header ${headerName}`,
    );
  }

  if (expected.status) {
    assert.ok(
      step.assertions.some(
        (assertion) =>
          assertion.type === "BODY" &&
          assertion.target === "JSON" &&
          assertion.path === "$.status" &&
          assertion.operator === "EQUALS" &&
          assertion.value === expected.status,
      ),
      `${step.stepName} must assert $.status=${expected.status}`,
    );
  }
}

const serialized = JSON.stringify(config).toLowerCase();
for (const forbiddenText of [
  "openrouter_api_key",
  "secretaccesskey",
  "sessiontoken",
]) {
  assert.ok(!serialized.includes(forbiddenText), `Config contains ${forbiddenText}`);
}

console.log(
  `Canary config valid: ${Object.keys(config.steps).length} read-only HTTPS checks`,
);
