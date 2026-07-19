import { randomBytes } from "node:crypto";

import { createWorkerHandler } from "./handler.js";
import { createOpenRouterBioGenerator } from "./openrouter.js";

const generateBio = createOpenRouterBioGenerator({
	fetch: globalThis.fetch,
	getApiKey: () => process.env.OPENROUTER_API_KEY,
	getModel: () => process.env.OPENROUTER_MODEL,
	requestTimeoutMs: 45_000,
	retryDelayMs: 400,
});

export const handler = createWorkerHandler({
	getBaseUrl: () => process.env.PROFILE_API_BASE_URL,
	getInternalKey: () => process.env.BIO_JOB_INTERNAL_KEY,
	fetch: globalThis.fetch,
	generateBio,
	generatePassword: () => randomBytes(32).toString("base64url"),
	requestTimeoutMs: 5_000,
	maxReceiveCount: 5,
	logError: (message, context) => console.error(message, context),
});
