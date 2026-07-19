import { BioGenerationError } from "./openrouter.js";

type Fetch = (
	input: string | URL | Request,
	init?: RequestInit,
) => Promise<Response>;

export type SQSEvent = {
	Records?: Array<{
		messageId?: string;
		body?: string;
		attributes?: { ApproximateReceiveCount?: string };
	}>;
};

export type SQSBatchResponse = {
	batchItemFailures: Array<{ itemIdentifier: string }>;
};

type RequestedEvent = {
	eventId: string;
	eventType: "profile.bio.generation.requested";
	eventVersion: 1;
	occurredAt: string;
	jobId: string;
	payload: { username: string; name: string };
};

type Dependencies = {
	getBaseUrl: () => string | undefined;
	getInternalKey: () => string | undefined;
	fetch: Fetch;
	generateBio: (name: string) => Promise<string>;
	generatePassword: () => string;
	requestTimeoutMs: number;
	maxReceiveCount: number;
	logError: (message: string, context: Record<string, unknown>) => void;
};

export function createWorkerHandler(dependencies: Dependencies) {
	return async function handler(
		event: SQSEvent = {},
	): Promise<SQSBatchResponse> {
		const failures: SQSBatchResponse["batchItemFailures"] = [];
		for (const record of event.Records ?? []) {
			const messageId = record.messageId ?? "missing-message-id";
			try {
				await processRecord(record, dependencies);
			} catch {
				failures.push({ itemIdentifier: messageId });
			}
		}
		return { batchItemFailures: failures };
	};
}

async function processRecord(
	record: NonNullable<SQSEvent["Records"]>[number],
	dependencies: Dependencies,
): Promise<void> {
	const messageId = record.messageId ?? "missing-message-id";
	const receiveCount = Math.max(
		1,
		Number.parseInt(record.attributes?.ApproximateReceiveCount ?? "1", 10) || 1,
	);
	let requested: RequestedEvent;
	try {
		requested = parseRequestedEvent(record.body);
	} catch {
		dependencies.logError("invalid bio job event", { messageId, receiveCount });
		throw new Error("invalid event");
	}

	const context = { messageId, jobId: requested.jobId, receiveCount };
	try {
		const claim = await callGo(dependencies, {
			path: `/internal/bio-jobs/${requested.jobId}/claim`,
			method: "POST",
		});
		if (!claim.ok) throw new WorkerError("profile_unavailable");
		const claimBody = await claim.json();
		if (!isRecord(claimBody) || typeof claimBody.claimed !== "boolean") {
			throw new WorkerError("invalid_job_response");
		}
		if (!claimBody.claimed) return;

		const bio = (await dependencies.generateBio(requested.payload.name)).trim();
		if (!bio || Array.from(bio).length > 500) {
			throw new WorkerError("invalid_generated_bio");
		}

		const created = await callGo(dependencies, {
			path: "/api/profiles",
			method: "POST",
			body: JSON.stringify({
				username: requested.payload.username,
				password: dependencies.generatePassword(),
				name: requested.payload.name,
				bio,
			}),
			internal: false,
		});
		if (!created.ok && created.status !== 409) {
			throw new WorkerError("profile_write_failed");
		}

		const completed = await callGo(dependencies, {
			path: `/internal/bio-jobs/${requested.jobId}/complete`,
			method: "POST",
		});
		if (!completed.ok) throw new WorkerError("job_complete_failed");
	} catch (error) {
		const errorCode = classifyError(error);
		dependencies.logError("bio job processing failed", {
			...context,
			errorCode,
		});
		await callGo(dependencies, {
			path: `/internal/bio-jobs/${requested.jobId}/fail`,
			method: "POST",
			body: JSON.stringify({
				errorCode,
				final: receiveCount >= dependencies.maxReceiveCount,
			}),
		}).catch(() => undefined);
		throw error;
	}
}

type GoRequest = {
	path: string;
	method: string;
	body?: string;
	internal?: boolean;
};

async function callGo(
	dependencies: Dependencies,
	request: GoRequest,
): Promise<Response> {
	const baseUrl = dependencies.getBaseUrl();
	const internalKey = dependencies.getInternalKey();
	if (!baseUrl || (request.internal !== false && !internalKey)) {
		throw new WorkerError("worker_configuration");
	}
	const url = new URL(baseUrl);
	url.pathname = request.path;
	const headers: Record<string, string> = { accept: "application/json" };
	if (request.body !== undefined) headers["content-type"] = "application/json";
	if (request.internal !== false && internalKey) {
		headers["x-profile-internal-key"] = internalKey;
	}

	const controller = new AbortController();
	const timer = setTimeout(
		() => controller.abort(),
		dependencies.requestTimeoutMs,
	);
	try {
		return await dependencies.fetch(url, {
			method: request.method,
			headers,
			body: request.body,
			signal: controller.signal,
		});
	} catch {
		throw new WorkerError("profile_unavailable");
	} finally {
		clearTimeout(timer);
	}
}

function parseRequestedEvent(body: string | undefined): RequestedEvent {
	if (!body) throw new Error("missing body");
	const envelope: unknown = JSON.parse(body);
	if (!isRecord(envelope) || typeof envelope.Message !== "string") {
		throw new Error("invalid SNS envelope");
	}
	const value: unknown = JSON.parse(envelope.Message);
	if (
		!isRecord(value) ||
		value.eventType !== "profile.bio.generation.requested" ||
		value.eventVersion !== 1 ||
		typeof value.eventId !== "string" ||
		typeof value.occurredAt !== "string" ||
		typeof value.jobId !== "string" ||
		!isRecord(value.payload) ||
		typeof value.payload.username !== "string" ||
		typeof value.payload.name !== "string"
	) {
		throw new Error("invalid event");
	}
	return value as RequestedEvent;
}

class WorkerError extends Error {
	constructor(readonly code: string) {
		super(code);
	}
}

function classifyError(error: unknown): string {
	if (error instanceof WorkerError) return error.code;
	if (error instanceof BioGenerationError) {
		if (error.kind === "configuration") return "model_configuration";
		if (error.kind === "timeout") return "model_timeout";
		if (error.kind === "invalid-response") return "model_invalid_response";
		return "model_unavailable";
	}
	return "worker_failure";
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}
