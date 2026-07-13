import assert from "node:assert/strict";
import { test } from "node:test";

import {
	createHandler,
	deriveGeneratedUsername,
	type LambdaEvent,
} from "./handler.js";
import { BioGenerationError } from "./openrouter.js";

type FetchCall = {
	url: string;
	init: RequestInit | undefined;
};

function event(
	method: string,
	path: string,
	options: {
		body?: string;
		headers?: Record<string, string>;
		rawQueryString?: string;
		isBase64Encoded?: boolean;
	} = {},
): LambdaEvent {
	return {
		rawPath: path,
		rawQueryString: options.rawQueryString,
		headers: options.headers,
		body: options.body,
		isBase64Encoded: options.isBase64Encoded,
		requestContext: { http: { method } },
	};
}

function createSuccessfulHandler(
	status = 200,
	body = JSON.stringify({ status: "ok" }),
) {
	const calls: FetchCall[] = [];
	const handler = createHandler({
		getBaseUrl: () => "http://profile-api.app.internal:8080",
		fetch: async (input, init) => {
			calls.push({ url: input.toString(), init });
			return new Response(body, {
				status,
				headers: { "content-type": "application/json" },
			});
		},
		generateBio: async () => "Generated bio",
		generatePassword: () => "random-password-for-tests",
		requestTimeoutMs: 5_000,
		logError: () => undefined,
	});

	return { handler, calls };
}

test("forwards public profile reads to the private Go service", async () => {
	const { handler, calls } = createSuccessfulHandler(
		200,
		JSON.stringify({ username: "alice" }),
	);

	const result = await handler(
		event("GET", "/api/profiles/alice", { rawQueryString: "view=public" }),
	);

	assert.equal(result.statusCode, 200);
	assert.equal(calls.length, 1);
	assert.equal(
		calls[0]?.url,
		"http://profile-api.app.internal:8080/api/profiles/alice?view=public",
	);
	assert.equal(calls[0]?.init?.method, "GET");
	assert.equal(calls[0]?.init?.body, undefined);
});

test("forwards valid profile creation with a 500-character Unicode bio", async () => {
	const { handler, calls } = createSuccessfulHandler(201);
	const body = JSON.stringify({
		username: "alice",
		password: "password123",
		name: "Alice",
		bio: "界".repeat(500),
	});

	const result = await handler(event("POST", "/api/profiles", { body }));

	assert.equal(result.statusCode, 201);
	assert.equal(calls.length, 1);
	assert.equal(calls[0]?.init?.method, "POST");
	assert.equal(calls[0]?.init?.body, body);
	assert.deepEqual(calls[0]?.init?.headers, {
		accept: "application/json",
		"content-type": "application/json",
	});
});

test("accepts an omitted or empty bio", async (t) => {
	for (const body of [
		JSON.stringify({
			username: "alice",
			password: "password123",
			name: "Alice",
		}),
		JSON.stringify({
			username: "alice",
			password: "password123",
			name: "Alice",
			bio: "",
		}),
	]) {
		await t.test(body.includes('"bio"') ? "empty" : "omitted", async () => {
			const { handler, calls } = createSuccessfulHandler(201);
			const result = await handler(event("POST", "/api/profiles", { body }));

			assert.equal(result.statusCode, 201);
			assert.equal(calls.length, 1);
		});
	}
});

test("rejects a 501-character Unicode bio before calling Go", async () => {
	const { handler, calls } = createSuccessfulHandler(201);
	const body = JSON.stringify({
		username: "alice",
		password: "password123",
		name: "Alice",
		bio: "界".repeat(501),
	});

	const result = await handler(event("POST", "/api/profiles", { body }));

	assert.equal(result.statusCode, 400);
	assert.equal(calls.length, 0);
	assert.deepEqual(JSON.parse(result.body), {
		message: "bio must be at most 500 characters",
	});
});

test("requires Authorization for profile updates", async () => {
	const { handler, calls } = createSuccessfulHandler();

	const result = await handler(
		event("PATCH", "/api/profiles/alice", {
			body: JSON.stringify({ name: "Alice Chen" }),
		}),
	);

	assert.equal(result.statusCode, 401);
	assert.equal(calls.length, 0);
});

test("forwards the Authorization header unchanged for profile updates", async () => {
	const { handler, calls } = createSuccessfulHandler();
	const authorization = " Basic YWxpY2U6cGFzc3dvcmQxMjM= ";

	const result = await handler(
		event("PATCH", "/api/profiles/alice", {
			headers: { Authorization: authorization },
			body: JSON.stringify({ bio: "Updated" }),
		}),
	);

	assert.equal(result.statusCode, 200);
	assert.equal(calls.length, 1);
	assert.deepEqual(calls[0]?.init?.headers, {
		accept: "application/json",
		"content-type": "application/json",
		authorization,
	});
});

test("validates JSON structure and request size before forwarding", async (t) => {
	const cases = [
		{
			name: "missing body",
			event: event("POST", "/api/profiles"),
			status: 400,
		},
		{
			name: "invalid JSON",
			event: event("POST", "/api/profiles", { body: "{" }),
			status: 400,
		},
		{
			name: "array body",
			event: event("POST", "/api/profiles", { body: "[]" }),
			status: 400,
		},
		{
			name: "unknown field",
			event: event("POST", "/api/profiles", {
				body: JSON.stringify({
					username: "alice",
					password: "password123",
					name: "Alice",
					role: "admin",
				}),
			}),
			status: 400,
		},
		{
			name: "body larger than 16 KiB",
			event: event("POST", "/api/profiles", {
				body: "x".repeat(16 * 1024 + 1),
			}),
			status: 413,
		},
	];

	for (const testCase of cases) {
		await t.test(testCase.name, async () => {
			const { handler, calls } = createSuccessfulHandler();
			const result = await handler(testCase.event);

			assert.equal(result.statusCode, testCase.status);
			assert.equal(calls.length, 0);
		});
	}
});

test("decodes an API Gateway base64 request body", async () => {
	const { handler, calls } = createSuccessfulHandler(201);
	const decodedBody = JSON.stringify({
		username: "alice",
		password: "password123",
		name: "Alice",
	});

	const result = await handler(
		event("POST", "/api/profiles", {
			body: Buffer.from(decodedBody).toString("base64"),
			isBase64Encoded: true,
		}),
	);

	assert.equal(result.statusCode, 201);
	assert.equal(calls[0]?.init?.body, decodedBody);
});

test("passes through safe upstream status and response body", async () => {
	const upstreamBody = JSON.stringify({ error: "username is already taken" });
	const { handler } = createSuccessfulHandler(409, upstreamBody);

	const result = await handler(
		event("POST", "/api/profiles", {
			body: JSON.stringify({
				username: "alice",
				password: "password123",
				name: "Alice",
			}),
		}),
	);

	assert.equal(result.statusCode, 409);
	assert.equal(result.body, upstreamBody);
});

test("maps an upstream network error to 502 without logging secrets", async () => {
	const logs: Array<{
		message: string;
		context: { path: string; isTimeout: boolean };
	}> = [];
	const handler = createHandler({
		getBaseUrl: () => "http://profile-api.app.internal:8080",
		fetch: async () => {
			throw new Error("connection refused");
		},
		generateBio: async () => "Generated bio",
		generatePassword: () => "random-password-for-tests",
		requestTimeoutMs: 5_000,
		logError: (message, context) => logs.push({ message, context }),
	});

	const result = await handler(
		event("PATCH", "/api/profiles/alice", {
			headers: { authorization: "Basic secret-value" },
			body: JSON.stringify({ bio: "private bio" }),
		}),
	);

	assert.equal(result.statusCode, 502);
	assert.deepEqual(logs, [
		{
			message: "profile API request failed",
			context: { path: "/api/profiles/alice", isTimeout: false },
		},
	]);
	assert.equal(JSON.stringify(logs).includes("secret-value"), false);
	assert.equal(JSON.stringify(logs).includes("private bio"), false);
});

test("maps an upstream timeout to 504", async () => {
	const handler = createHandler({
		getBaseUrl: () => "http://profile-api.app.internal:8080",
		fetch: (_input, init) =>
			new Promise((_resolve, reject) => {
				init?.signal?.addEventListener(
					"abort",
					() => {
						const error = new Error("aborted");
						error.name = "AbortError";
						reject(error);
					},
					{ once: true },
				);
			}),
		generateBio: async () => "Generated bio",
		generatePassword: () => "random-password-for-tests",
		requestTimeoutMs: 1,
		logError: () => undefined,
	});

	const result = await handler(event("GET", "/health"));

	assert.equal(result.statusCode, 504);
});

test("returns 404 or 405 without calling the private service", async (t) => {
	for (const testCase of [
		{ name: "unknown route", event: event("GET", "/unknown"), status: 404 },
		{
			name: "unsupported method",
			event: event("DELETE", "/api/profiles/alice"),
			status: 405,
		},
	]) {
		await t.test(testCase.name, async () => {
			const { handler, calls } = createSuccessfulHandler();
			const result = await handler(testCase.event);

			assert.equal(result.statusCode, testCase.status);
			assert.equal(calls.length, 0);
		});
	}
});

test("returns an existing generated profile without calling OpenRouter", async () => {
	const calls: FetchCall[] = [];
	let generationCalls = 0;
	const profile = {
		username: deriveGeneratedUsername("alice"),
		name: "Alice",
		bio: "Existing bio",
	};
	const handler = createHandler({
		getBaseUrl: () => "http://profile-api.app.internal:8080",
		fetch: async (input, init) => {
			calls.push({ url: input.toString(), init });
			return new Response(JSON.stringify(profile), {
				status: 200,
				headers: { "content-type": "application/json" },
			});
		},
		generateBio: async () => {
			generationCalls += 1;
			return "must not be used";
		},
		generatePassword: () => "must-not-be-used",
		requestTimeoutMs: 5_000,
		logError: () => undefined,
	});

	const result = await handler(
		event("POST", "/api/profiles/generate-bio", {
			body: JSON.stringify({ name: "  ＡＬＩＣＥ  " }),
		}),
	);

	assert.equal(result.statusCode, 200);
	assert.deepEqual(JSON.parse(result.body), {
		name: "Alice",
		bio: "Existing bio",
	});
	assert.equal(result.body.includes("username"), false);
	assert.equal(generationCalls, 0);
	assert.equal(calls.length, 1);
	assert.equal(
		calls[0]?.url,
		`http://profile-api.app.internal:8080/api/profiles/${deriveGeneratedUsername("alice")}`,
	);
});

test("generates and saves a bio through the existing Go create endpoint", async () => {
	const calls: FetchCall[] = [];
	const handler = createHandler({
		getBaseUrl: () => "http://profile-api.app.internal:8080",
		fetch: async (input, init) => {
			calls.push({ url: input.toString(), init });
			if (init?.method === "GET") {
				return new Response(JSON.stringify({ error: "profile not found" }), {
					status: 404,
				});
			}
			return new Response(
				JSON.stringify({ name: "Alice", bio: "A focused builder." }),
				{
					status: 201,
					headers: { "content-type": "application/json" },
				},
			);
		},
		generateBio: async (name) => {
			assert.equal(name, "Alice");
			return "  A focused builder.  ";
		},
		generatePassword: () => "unique-random-password",
		requestTimeoutMs: 5_000,
		logError: () => undefined,
	});

	const result = await handler(
		event("POST", "/api/profiles/generate-bio", {
			body: JSON.stringify({ name: " Alice " }),
		}),
	);

	assert.equal(result.statusCode, 201);
	assert.deepEqual(JSON.parse(result.body), {
		name: "Alice",
		bio: "A focused builder.",
	});
	assert.equal(result.body.includes("username"), false);
	assert.equal(calls.length, 2);
	assert.equal(
		calls[1]?.url,
		"http://profile-api.app.internal:8080/api/profiles",
	);
	assert.deepEqual(JSON.parse(String(calls[1]?.init?.body)), {
		username: deriveGeneratedUsername("alice"),
		password: "unique-random-password",
		name: "Alice",
		bio: "A focused builder.",
	});
});

test("re-reads the winning profile after a concurrent create conflict", async () => {
	const statuses = [404, 409, 200];
	const methods: string[] = [];
	const handler = createHandler({
		getBaseUrl: () => "http://profile-api.app.internal:8080",
		fetch: async (_input, init) => {
			methods.push(String(init?.method));
			const status = statuses.shift() ?? 500;
			return new Response(
				JSON.stringify(
					status === 200
						? { name: "Alice", bio: "Winner" }
						: { error: "conflict" },
				),
				{ status, headers: { "content-type": "application/json" } },
			);
		},
		generateBio: async () => "Candidate",
		generatePassword: () => "unique-random-password",
		requestTimeoutMs: 5_000,
		logError: () => undefined,
	});

	const result = await handler(
		event("POST", "/api/profiles/generate-bio", {
			body: JSON.stringify({ name: "Alice" }),
		}),
	);

	assert.equal(result.statusCode, 200);
	assert.deepEqual(methods, ["GET", "POST", "GET"]);
	assert.deepEqual(JSON.parse(result.body), { name: "Alice", bio: "Winner" });
});

test("validates the generate-bio name before any downstream call", async (t) => {
	for (const body of [
		{},
		{ name: "" },
		{ name: "x".repeat(81) },
		{ name: "Alice", username: "override" },
	]) {
		await t.test(JSON.stringify(body), async () => {
			const { handler, calls } = createSuccessfulHandler();
			const result = await handler(
				event("POST", "/api/profiles/generate-bio", {
					body: JSON.stringify(body),
				}),
			);
			assert.equal(result.statusCode, 400);
			assert.equal(calls.length, 0);
		});
	}
});

test("rejects an oversized generated bio instead of silently truncating it", async () => {
	const calls: FetchCall[] = [];
	const logs: string[] = [];
	const handler = createHandler({
		getBaseUrl: () => "http://profile-api.app.internal:8080",
		fetch: async (input, init) => {
			calls.push({ url: input.toString(), init });
			return new Response("{}", { status: 404 });
		},
		generateBio: async () => "界".repeat(501),
		generatePassword: () => "unique-random-password",
		requestTimeoutMs: 5_000,
		logError: (message) => logs.push(message),
	});

	const result = await handler(
		event("POST", "/api/profiles/generate-bio", {
			body: JSON.stringify({ name: "Alice" }),
		}),
	);

	assert.equal(result.statusCode, 502);
	assert.equal(calls.length, 1);
	assert.deepEqual(logs, ["model returned an invalid bio"]);
});

test("maps OpenRouter failures without exposing API details", async () => {
	const logs: Array<{ message: string; context: Record<string, unknown> }> = [];
	const handler = createHandler({
		getBaseUrl: () => "http://profile-api.app.internal:8080",
		fetch: async () => new Response("{}", { status: 404 }),
		generateBio: async () => {
			throw new BioGenerationError("Bearer highly-secret-key", "upstream", 503);
		},
		generatePassword: () => "unique-random-password",
		requestTimeoutMs: 5_000,
		logError: (message, context) => logs.push({ message, context }),
	});

	const result = await handler(
		event("POST", "/api/profiles/generate-bio", {
			body: JSON.stringify({ name: "Alice" }),
		}),
	);

	assert.equal(result.statusCode, 502);
	assert.deepEqual(JSON.parse(result.body), { message: "model unavailable" });
	assert.deepEqual(logs, [
		{
			message: "bio generation failed",
			context: {
				path: "/api/profiles/generate-bio",
				isTimeout: false,
				reason: "upstream",
				upstreamStatus: 503,
			},
		},
	]);
	assert.equal(JSON.stringify(logs).includes("highly-secret-key"), false);
});
