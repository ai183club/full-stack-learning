import { randomBytes, randomUUID } from "node:crypto";

import { SNSClient } from "@aws-sdk/client-sns";

import { createHandler } from "./handler.js";
import { createOpenRouterBioGenerator } from "./openrouter.js";
import { createBioJobPublisher } from "./sns.js";

const publishBioJob = process.env.BIO_JOB_TOPIC_ARN
	? createBioJobPublisher({
			client: new SNSClient({}),
			getTopicArn: () => process.env.BIO_JOB_TOPIC_ARN,
		})
	: undefined;

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
	generateJobId: randomUUID,
	getInternalKey: () => process.env.BIO_JOB_INTERNAL_KEY,
	...(publishBioJob ? { publishBioJob } : {}),
	requestTimeoutMs: 5_000,
	logError: (message, context) => console.error(message, context),
});

export const handler = profileHandler;
