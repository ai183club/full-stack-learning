import assert from "node:assert/strict";
import { test } from "node:test";

import {
	BioGenerationError,
	createOpenRouterBioGenerator,
} from "./openrouter.js";

test("calls OpenRouter with Gemma, reasoning, and a server-side bearer token", async () => {
	let request: RequestInit | undefined;
	const generateBio = createOpenRouterBioGenerator({
		fetch: async (input, init) => {
			assert.equal(
				input.toString(),
				"https://openrouter.ai/api/v1/chat/completions",
			);
			request = init;
			return new Response(
				JSON.stringify({
					choices: [{ message: { content: "A thoughtful builder." } }],
				}),
				{ status: 200, headers: { "content-type": "application/json" } },
			);
		},
		getApiKey: () => "test-api-key",
		getModel: () => undefined,
		requestTimeoutMs: 5_000,
	});

	const bio = await generateBio("Alice");
	const body = JSON.parse(String(request?.body));

	assert.equal(bio, "A thoughtful builder.");
	assert.equal(request?.method, "POST");
	assert.deepEqual(request?.headers, {
		Authorization: "Bearer test-api-key",
		"Content-Type": "application/json",
	});
	assert.equal(body.model, "google/gemma-4-31b-it:free");
	assert.equal(body.max_tokens, 220);
	assert.deepEqual(body.reasoning, {
		enabled: true,
		max_tokens: 64,
		exclude: true,
	});
	assert.equal(body.messages[1].content.includes("Alice"), true);
	assert.equal(body.messages[0].content.includes("简体中文"), true);
	assert.equal(body.messages[1].content.includes("中文个人简介"), true);
});

test("supports overriding the OpenRouter model", async () => {
	let model: string | undefined;
	const generateBio = createOpenRouterBioGenerator({
		fetch: async (_input, init) => {
			model = JSON.parse(String(init?.body)).model;
			return new Response(
				JSON.stringify({ choices: [{ message: { content: "Bio" } }] }),
				{ status: 200 },
			);
		},
		getApiKey: () => "test-api-key",
		getModel: () => "google/gemma-custom",
		requestTimeoutMs: 5_000,
	});

	await generateBio("Alice");
	assert.equal(model, "google/gemma-custom");
});

test("fails safely when the OpenRouter API key is missing", async () => {
	const generateBio = createOpenRouterBioGenerator({
		fetch: async () => {
			throw new Error("must not run");
		},
		getApiKey: () => undefined,
		getModel: () => undefined,
		requestTimeoutMs: 5_000,
	});

	await assert.rejects(generateBio("Alice"), (error: unknown) => {
		assert.equal(error instanceof BioGenerationError, true);
		assert.equal((error as BioGenerationError).kind, "configuration");
		return true;
	});
});

test("does not include an OpenRouter error body in the thrown error", async () => {
	let calls = 0;
	const generateBio = createOpenRouterBioGenerator({
		fetch: async () => {
			calls += 1;
			return new Response(
				JSON.stringify({ error: "secret provider details" }),
				{
					status: 429,
				},
			);
		},
		getApiKey: () => "test-api-key",
		getModel: () => undefined,
		requestTimeoutMs: 5_000,
		retryDelayMs: 0,
	});

	await assert.rejects(generateBio("Alice"), (error: unknown) => {
		assert.equal(error instanceof BioGenerationError, true);
		assert.equal((error as BioGenerationError).kind, "upstream");
		assert.equal((error as BioGenerationError).statusCode, 429);
		assert.equal(String(error).includes("secret provider details"), false);
		return true;
	});
	assert.equal(calls, 2);
});

test("retries one transient provider error and then returns the bio", async () => {
	const statuses = [503, 200];
	const delays: number[] = [];
	const generateBio = createOpenRouterBioGenerator({
		fetch: async () => {
			const status = statuses.shift() ?? 500;
			return new Response(
				JSON.stringify({
					choices: [{ message: { content: "Recovered bio" } }],
				}),
				{ status },
			);
		},
		getApiKey: () => "test-api-key",
		getModel: () => undefined,
		requestTimeoutMs: 5_000,
		retryDelayMs: 25,
		sleep: async (milliseconds) => {
			delays.push(milliseconds);
		},
	});

	assert.equal(await generateBio("Alice"), "Recovered bio");
	assert.deepEqual(delays, [25]);
});

test("does not retry a non-transient client error", async () => {
	let calls = 0;
	const generateBio = createOpenRouterBioGenerator({
		fetch: async () => {
			calls += 1;
			return new Response("{}", { status: 401 });
		},
		getApiKey: () => "test-api-key",
		getModel: () => undefined,
		requestTimeoutMs: 5_000,
		sleep: async () => {
			throw new Error("must not sleep");
		},
	});

	await assert.rejects(generateBio("Alice"), (error: unknown) => {
		assert.equal((error as BioGenerationError).statusCode, 401);
		return true;
	});
	assert.equal(calls, 1);
});
