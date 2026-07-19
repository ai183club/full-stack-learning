import { createHash } from "node:crypto";

import { BioGenerationError } from "./openrouter.js";

export type LambdaEvent = {
	rawPath?: string;
	path?: string;
	rawQueryString?: string;
	headers?: Record<string, string | undefined>;
	body?: string | null;
	isBase64Encoded?: boolean;
	requestContext?: { http?: { method?: string } };
};

export type LambdaResponse = {
	statusCode: number;
	headers: Record<string, string>;
	body: string;
};

type Fetch = (
	input: string | URL | Request,
	init?: RequestInit,
) => Promise<Response>;

type HandlerDependencies = {
	getBaseUrl: () => string | undefined;
	fetch: Fetch;
	generateBio?: (name: string) => Promise<string>;
	generatePassword?: () => string;
	generateJobId?: () => string;
	getInternalKey?: () => string | undefined;
	publishBioJob?: (event: BioGenerationRequestedEvent) => Promise<void>;
	requestTimeoutMs: number;
	logError: (
		message: string,
		context: {
			path: string;
			isTimeout: boolean;
			reason?: string;
			upstreamStatus?: number;
		},
	) => void;
};

type Route =
	| "health"
	| "ready"
	| "create"
	| "find"
	| "update"
	| "generate-bio"
	| "find-bio-job";

export type BioGenerationRequestedEvent = {
	eventId: string;
	eventType: "profile.bio.generation.requested";
	eventVersion: 1;
	occurredAt: string;
	jobId: string;
	payload: {
		username: string;
		name: string;
	};
};

const maxBioCharacters = 500;
const maxNameCharacters = 80;
const maxRequestBodyBytes = 16 * 1024;
const generatedBioPath = "/api/profiles/generate-bio";
const profilePathPattern = /^\/api\/profiles\/([a-z0-9_]{3,32})$/;
const bioJobPathPattern = /^\/api\/bio-jobs\/[0-9a-f-]{36}$/;

export function createHandler(dependencies: HandlerDependencies) {
	return async function handler(
		event: LambdaEvent = {},
	): Promise<LambdaResponse> {
		const method = event.requestContext?.http?.method?.toUpperCase() ?? "GET";
		const path = event.rawPath ?? event.path ?? "/";
		if (method === "OPTIONS") {
			return { statusCode: 204, headers: {}, body: "" };
		}
		const route = resolveRoute(method, path);

		if (route === "not-found") {
			return jsonResponse(404, { message: "route not found" });
		}
		if (route === "method-not-allowed") {
			return jsonResponse(405, { message: "method not allowed" });
		}

		const baseUrl = dependencies.getBaseUrl();
		if (!baseUrl) {
			dependencies.logError("profile API base URL is not configured", {
				path,
				isTimeout: false,
			});
			return jsonResponse(500, { message: "upstream configuration error" });
		}

		if (route === "generate-bio") {
			return dependencies.publishBioJob
				? handleAsyncGenerateBio(event, baseUrl, dependencies)
				: handleGenerateBio(event, baseUrl, dependencies);
		}

		let body: string | undefined;
		let authorization: string | undefined;
		if (route === "create" || route === "update") {
			if (route === "update") {
				authorization = getHeader(event.headers, "authorization");
				if (!authorization) {
					return jsonResponse(401, {
						message: "authorization is required",
					});
				}
			}

			const bodyResult = parseRequestBody(event);
			if (!bodyResult.ok) {
				return jsonResponse(bodyResult.statusCode, {
					message: bodyResult.message,
				});
			}

			const validationError =
				route === "create"
					? validateCreateBody(bodyResult.value)
					: validateUpdateBody(bodyResult.value);
			if (validationError) {
				return jsonResponse(400, { message: validationError });
			}
			body = bodyResult.raw;
		}

		return requestProfileApi(dependencies, {
			baseUrl,
			path,
			query: event.rawQueryString,
			method,
			body,
			authorization,
		});
	};
}

async function handleAsyncGenerateBio(
	event: LambdaEvent,
	baseUrl: string,
	dependencies: HandlerDependencies,
): Promise<LambdaResponse> {
	const bodyResult = parseRequestBody(event);
	if (!bodyResult.ok) {
		return jsonResponse(bodyResult.statusCode, { message: bodyResult.message });
	}
	const nameResult = validateAndNormalizeName(bodyResult.value);
	if (!nameResult.ok) return jsonResponse(400, { message: nameResult.message });

	const internalKey = dependencies.getInternalKey?.();
	const generateJobId = dependencies.generateJobId;
	const publishBioJob = dependencies.publishBioJob;
	const candidateJobId = generateJobId?.();
	if (!internalKey || !candidateJobId || !generateJobId || !publishBioJob) {
		dependencies.logError("async bio configuration is incomplete", {
			path: generatedBioPath,
			isTimeout: false,
		});
		return jsonResponse(500, { message: "async job configuration error" });
	}

	const username = deriveGeneratedUsername(nameResult.nameKey);
	const upstream = await requestProfileApi(dependencies, {
		baseUrl,
		path: "/internal/bio-jobs",
		method: "POST",
		internalKey,
		body: JSON.stringify({
			jobId: candidateJobId,
			username,
			name: nameResult.displayName,
		}),
	});
	if (!isSuccessful(upstream.statusCode)) return upstream;

	let job: { jobId: string; name: string; status: string };
	try {
		const parsed: unknown = JSON.parse(upstream.body);
		if (
			!isRecord(parsed) ||
			typeof parsed.jobId !== "string" ||
			typeof parsed.name !== "string" ||
			typeof parsed.status !== "string"
		) {
			throw new Error("invalid job response");
		}
		job = { jobId: parsed.jobId, name: parsed.name, status: parsed.status };
	} catch {
		return jsonResponse(502, { message: "invalid upstream response" });
	}

	if (job.status === "pending") {
		const jobEvent: BioGenerationRequestedEvent = {
			eventId: generateJobId(),
			eventType: "profile.bio.generation.requested",
			eventVersion: 1,
			occurredAt: new Date().toISOString(),
			jobId: job.jobId,
			payload: { username, name: job.name },
		};
		try {
			await publishBioJob(jobEvent);
		} catch {
			dependencies.logError("publish bio job failed", {
				path: generatedBioPath,
				isTimeout: false,
				reason: "sns-publish-failed",
			});
			return jsonResponse(503, { message: "job queue unavailable" });
		}
	}

	return jsonResponse(202, { jobId: job.jobId, status: job.status });
}

async function handleGenerateBio(
	event: LambdaEvent,
	baseUrl: string,
	dependencies: HandlerDependencies,
): Promise<LambdaResponse> {
	const bodyResult = parseRequestBody(event);
	if (!bodyResult.ok) {
		return jsonResponse(bodyResult.statusCode, { message: bodyResult.message });
	}

	const nameResult = validateAndNormalizeName(bodyResult.value);
	if (!nameResult.ok) {
		return jsonResponse(400, { message: nameResult.message });
	}
	if (!dependencies.generateBio || !dependencies.generatePassword) {
		return jsonResponse(500, { message: "model configuration error" });
	}

	const username = deriveGeneratedUsername(nameResult.nameKey);
	const profilePath = `/api/profiles/${username}`;
	const existing = await requestProfileApi(dependencies, {
		baseUrl,
		path: profilePath,
		method: "GET",
	});
	if (existing.statusCode !== 404) {
		return isSuccessful(existing.statusCode)
			? toPublicBioResponse(existing, dependencies)
			: existing;
	}

	let bio: string;
	try {
		bio = (await dependencies.generateBio(nameResult.displayName)).trim();
	} catch (error) {
		const isTimeout =
			error instanceof BioGenerationError && error.kind === "timeout";
		dependencies.logError("bio generation failed", {
			path: generatedBioPath,
			isTimeout,
			reason:
				error instanceof BioGenerationError ? error.kind : "unknown-error",
			upstreamStatus:
				error instanceof BioGenerationError ? error.statusCode : undefined,
		});
		if (error instanceof BioGenerationError && error.kind === "configuration") {
			return jsonResponse(500, { message: "model configuration error" });
		}
		return jsonResponse(isTimeout ? 504 : 502, {
			message: isTimeout ? "model request timeout" : "model unavailable",
		});
	}

	const bioError = validateGeneratedBio(bio);
	if (bioError) {
		dependencies.logError("model returned an invalid bio", {
			path: generatedBioPath,
			isTimeout: false,
		});
		return jsonResponse(502, { message: bioError });
	}

	const created = await requestProfileApi(dependencies, {
		baseUrl,
		path: "/api/profiles",
		method: "POST",
		body: JSON.stringify({
			username,
			password: dependencies.generatePassword(),
			name: nameResult.displayName,
			bio,
		}),
	});
	if (created.statusCode !== 409) {
		return isSuccessful(created.statusCode)
			? toPublicBioResponse(created, dependencies)
			: created;
	}

	const winner = await requestProfileApi(dependencies, {
		baseUrl,
		path: profilePath,
		method: "GET",
	});
	return isSuccessful(winner.statusCode)
		? toPublicBioResponse(winner, dependencies)
		: winner;
}

function toPublicBioResponse(
	upstream: LambdaResponse,
	dependencies: HandlerDependencies,
): LambdaResponse {
	try {
		const profile: unknown = JSON.parse(upstream.body);
		if (
			isRecord(profile) &&
			typeof profile.name === "string" &&
			typeof profile.bio === "string"
		) {
			return jsonResponse(upstream.statusCode, {
				name: profile.name,
				bio: profile.bio,
			});
		}
	} catch {
		// The safe response below intentionally hides malformed upstream content.
	}
	dependencies.logError("profile API returned an invalid generated profile", {
		path: generatedBioPath,
		isTimeout: false,
	});
	return jsonResponse(502, { message: "invalid upstream response" });
}

function isSuccessful(statusCode: number): boolean {
	return statusCode >= 200 && statusCode < 300;
}

type ProfileRequest = {
	baseUrl: string;
	path: string;
	query?: string;
	method: string;
	body?: string;
	authorization?: string;
	internalKey?: string;
};

async function requestProfileApi(
	dependencies: HandlerDependencies,
	request: ProfileRequest,
): Promise<LambdaResponse> {
	const upstreamUrl = new URL(request.baseUrl);
	upstreamUrl.pathname = request.path;
	upstreamUrl.search = request.query ? `?${request.query}` : "";

	const headers: Record<string, string> = { accept: "application/json" };
	if (request.body !== undefined) {
		headers["content-type"] = "application/json";
	}
	if (request.authorization) {
		headers.authorization = request.authorization;
	}
	if (request.internalKey) {
		headers["x-profile-internal-key"] = request.internalKey;
	}

	const controller = new AbortController();
	const timer = setTimeout(
		() => controller.abort(),
		dependencies.requestTimeoutMs,
	);

	try {
		const upstreamResponse = await dependencies.fetch(upstreamUrl, {
			method: request.method,
			headers,
			body: request.body,
			signal: controller.signal,
		});
		return {
			statusCode: upstreamResponse.status,
			headers: {
				"content-type":
					upstreamResponse.headers.get("content-type") ?? "application/json",
			},
			body: await upstreamResponse.text(),
		};
	} catch (error) {
		const isTimeout = error instanceof Error && error.name === "AbortError";
		dependencies.logError("profile API request failed", {
			path: request.path,
			isTimeout,
		});
		return jsonResponse(isTimeout ? 504 : 502, {
			message: isTimeout ? "upstream timeout" : "upstream unavailable",
		});
	} finally {
		clearTimeout(timer);
	}
}

export function deriveGeneratedUsername(nameKey: string): string {
	const digest = createHash("sha256").update(nameKey, "utf8").digest("hex");
	return `bio_${digest.slice(0, 28)}`;
}

function validateAndNormalizeName(
	value: Record<string, unknown>,
):
	| { ok: true; displayName: string; nameKey: string }
	| { ok: false; message: string } {
	const unknownField = findUnknownField(value, ["name"]);
	if (unknownField) {
		return { ok: false, message: `unknown field: ${unknownField}` };
	}
	if (typeof value.name !== "string") {
		return { ok: false, message: "name must be a string" };
	}
	const displayName = value.name.normalize("NFKC").trim();
	const length = Array.from(displayName).length;
	if (length < 1 || length > maxNameCharacters) {
		return { ok: false, message: "name must be 1-80 characters" };
	}
	return {
		ok: true,
		displayName,
		nameKey: displayName.toLocaleLowerCase("en-US"),
	};
}

function validateGeneratedBio(bio: string): string | undefined {
	if (bio.length === 0) {
		return "model returned an empty bio";
	}
	if (Array.from(bio).length > maxBioCharacters) {
		return "model returned a bio longer than 500 characters";
	}
	return undefined;
}

function resolveRoute(
	method: string,
	path: string,
): Route | "not-found" | "method-not-allowed" {
	if (path === "/health") {
		return method === "GET" ? "health" : "method-not-allowed";
	}
	if (path === "/ready") {
		return method === "GET" ? "ready" : "method-not-allowed";
	}
	if (path === generatedBioPath) {
		return method === "POST" ? "generate-bio" : "method-not-allowed";
	}
	if (bioJobPathPattern.test(path)) {
		return method === "GET" ? "find-bio-job" : "method-not-allowed";
	}
	if (path === "/api/profiles") {
		return method === "POST" ? "create" : "method-not-allowed";
	}
	if (!profilePathPattern.test(path)) {
		return "not-found";
	}
	if (method === "GET") {
		return "find";
	}
	if (method === "PATCH") {
		return "update";
	}
	return "method-not-allowed";
}

function parseRequestBody(
	event: LambdaEvent,
):
	| { ok: true; raw: string; value: Record<string, unknown> }
	| { ok: false; statusCode: number; message: string } {
	if (event.body === undefined || event.body === null || event.body === "") {
		return { ok: false, statusCode: 400, message: "JSON body is required" };
	}
	const raw = event.isBase64Encoded
		? Buffer.from(event.body, "base64").toString("utf8")
		: event.body;
	if (Buffer.byteLength(raw, "utf8") > maxRequestBodyBytes) {
		return { ok: false, statusCode: 413, message: "request body is too large" };
	}
	let value: unknown;
	try {
		value = JSON.parse(raw);
	} catch {
		return { ok: false, statusCode: 400, message: "invalid JSON body" };
	}
	if (!isRecord(value)) {
		return {
			ok: false,
			statusCode: 400,
			message: "JSON body must be an object",
		};
	}
	return { ok: true, raw, value };
}

function validateCreateBody(
	value: Record<string, unknown>,
): string | undefined {
	const unknownField = findUnknownField(value, [
		"username",
		"password",
		"name",
		"bio",
	]);
	if (unknownField) return `unknown field: ${unknownField}`;
	for (const field of ["username", "password", "name"] as const) {
		if (typeof value[field] !== "string") return `${field} must be a string`;
	}
	if (value.bio !== undefined && typeof value.bio !== "string")
		return "bio must be a string";
	return validateBio(value.bio);
}

function validateUpdateBody(
	value: Record<string, unknown>,
): string | undefined {
	const unknownField = findUnknownField(value, ["name", "bio"]);
	if (unknownField) return `unknown field: ${unknownField}`;
	if (value.name === undefined && value.bio === undefined)
		return "name or bio is required";
	if (value.name !== undefined && typeof value.name !== "string")
		return "name must be a string";
	if (value.bio !== undefined && typeof value.bio !== "string")
		return "bio must be a string";
	return validateBio(value.bio);
}

function validateBio(value: unknown): string | undefined {
	if (
		typeof value === "string" &&
		Array.from(value).length > maxBioCharacters
	) {
		return "bio must be at most 500 characters";
	}
	return undefined;
}

function findUnknownField(
	value: Record<string, unknown>,
	allowedFields: readonly string[],
): string | undefined {
	const allowed = new Set(allowedFields);
	return Object.keys(value).find((field) => !allowed.has(field));
}

function getHeader(
	headers: Record<string, string | undefined> | undefined,
	name: string,
): string | undefined {
	if (!headers) return undefined;
	const entry = Object.entries(headers).find(
		([headerName]) => headerName.toLowerCase() === name,
	);
	const value = entry?.[1];
	return value && value.trim().length > 0 ? value : undefined;
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

function jsonResponse(
	statusCode: number,
	body: Record<string, unknown>,
): LambdaResponse {
	return {
		statusCode,
		headers: { "content-type": "application/json" },
		body: JSON.stringify(body),
	};
}
