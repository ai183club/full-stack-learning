import { randomBytes } from "node:crypto";

import { createHandler } from "./handler.js";
import { createOpenRouterBioGenerator } from "./openrouter.js";

const generateBio = createOpenRouterBioGenerator({
	fetch: globalThis.fetch,
	getApiKey: () => process.env.OPENROUTER_API_KEY,
	getModel: () => process.env.OPENROUTER_MODEL,
	requestTimeoutMs: 28_000,
	retryDelayMs: 400,
});

const profileHandler = createHandler({
	getBaseUrl: () => process.env.PROFILE_API_BASE_URL,
	fetch: globalThis.fetch,
	generateBio,
	generatePassword: () => randomBytes(32).toString("base64url"),
	requestTimeoutMs: 5_000,
	logError: (message, context) => console.error(message, context),
});

export const handler = profileHandler;
