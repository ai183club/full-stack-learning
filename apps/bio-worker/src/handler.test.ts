import assert from "node:assert/strict";
import { test } from "node:test";

import { createWorkerHandler, type SQSEvent } from "./handler.js";

function sqsEvent(receiveCount = 1): SQSEvent {
	return {
		Records: [
			{
				messageId: "message-1",
				attributes: { ApproximateReceiveCount: String(receiveCount) },
				body: JSON.stringify({
					Type: "Notification",
					Message: JSON.stringify({
						eventId: "4dd5adf5-1501-4c87-a247-78b24bb0b21a",
						eventType: "profile.bio.generation.requested",
						eventVersion: 1,
						occurredAt: "2026-07-19T00:00:00.000Z",
						jobId: "e52f5f2d-c53c-4c2c-bb10-f378f82acc5d",
						payload: {
							username: "bio_1234567890123456789012345678",
							name: "Alice",
						},
					}),
				}),
			},
		],
	};
}

test("claims, generates, saves, and completes one SNS-wrapped SQS job", async () => {
	const paths: string[] = [];
	const handler = createWorkerHandler({
		getBaseUrl: () => "http://profile-api.app.internal:8080",
		getInternalKey: () => "internal-test-key",
		fetch: async (input, init) => {
			const url = new URL(input.toString());
			paths.push(`${init?.method} ${url.pathname}`);
			if (url.pathname.endsWith("/claim")) {
				assert.equal(
					(init?.headers as Record<string, string> | undefined)?.[
						"x-profile-internal-key"
					],
					"internal-test-key",
				);
				return new Response(
					JSON.stringify({ claimed: true, job: { status: "running" } }),
					{ status: 200 },
				);
			}
			if (url.pathname === "/api/profiles") {
				const body = JSON.parse(String(init?.body));
				assert.equal(body.bio, "Generated bio");
				assert.equal(body.password, "random-password");
				return new Response("{}", { status: 201 });
			}
			return new Response("{}", { status: 200 });
		},
		generateBio: async () => " Generated bio ",
		generatePassword: () => "random-password",
		requestTimeoutMs: 1_000,
		maxReceiveCount: 5,
		logError: () => undefined,
	});

	assert.deepEqual(await handler(sqsEvent()), { batchItemFailures: [] });
	assert.deepEqual(paths, [
		"POST /internal/bio-jobs/e52f5f2d-c53c-4c2c-bb10-f378f82acc5d/claim",
		"POST /api/profiles",
		"POST /internal/bio-jobs/e52f5f2d-c53c-4c2c-bb10-f378f82acc5d/complete",
	]);
});

test("acknowledges an already claimed or completed duplicate without generating", async () => {
	let generationCalls = 0;
	const handler = createWorkerHandler({
		getBaseUrl: () => "http://profile-api.app.internal:8080",
		getInternalKey: () => "internal-test-key",
		fetch: async () =>
			new Response(
				JSON.stringify({ claimed: false, job: { status: "completed" } }),
				{ status: 200 },
			),
		generateBio: async () => {
			generationCalls += 1;
			return "unused";
		},
		generatePassword: () => "unused",
		requestTimeoutMs: 1_000,
		maxReceiveCount: 5,
		logError: () => undefined,
	});

	assert.deepEqual(await handler(sqsEvent()), { batchItemFailures: [] });
	assert.equal(generationCalls, 0);
});

test("returns a partial batch failure and marks the fifth attempt final", async () => {
	const failureBodies: unknown[] = [];
	const handler = createWorkerHandler({
		getBaseUrl: () => "http://profile-api.app.internal:8080",
		getInternalKey: () => "internal-test-key",
		fetch: async (input, init) => {
			const url = new URL(input.toString());
			if (url.pathname.endsWith("/claim")) {
				return new Response(JSON.stringify({ claimed: true }), { status: 200 });
			}
			if (url.pathname.endsWith("/fail")) {
				failureBodies.push(JSON.parse(String(init?.body)));
				return new Response("{}", { status: 200 });
			}
			return new Response("{}", { status: 500 });
		},
		generateBio: async () => {
			throw new Error("provider unavailable with secret details");
		},
		generatePassword: () => "unused",
		requestTimeoutMs: 1_000,
		maxReceiveCount: 5,
		logError: () => undefined,
	});

	assert.deepEqual(await handler(sqsEvent(5)), {
		batchItemFailures: [{ itemIdentifier: "message-1" }],
	});
	assert.deepEqual(failureBodies, [
		{ errorCode: "worker_failure", final: true },
	]);
});

test("rejects a poison message without logging its body", async () => {
	const logs: Array<{ message: string; context: Record<string, unknown> }> = [];
	const handler = createWorkerHandler({
		getBaseUrl: () => "http://profile-api.app.internal:8080",
		getInternalKey: () => "internal-test-key",
		fetch: async () => new Response("{}", { status: 200 }),
		generateBio: async () => "unused",
		generatePassword: () => "unused",
		requestTimeoutMs: 1_000,
		maxReceiveCount: 5,
		logError: (message, context) => logs.push({ message, context }),
	});

	const body = "poison-secret-body";
	const result = await handler({ Records: [{ messageId: "poison-1", body }] });
	assert.deepEqual(result, {
		batchItemFailures: [{ itemIdentifier: "poison-1" }],
	});
	assert.equal(JSON.stringify(logs).includes(body), false);
});
