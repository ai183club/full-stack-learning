import { env } from "@full-stack-learning/env/web";

export type PublicBio = {
	name: string;
	bio: string;
};

export type BioJobStatus = "pending" | "running" | "completed" | "failed";

type BioJob = {
	jobId: string;
	name?: string;
	status: BioJobStatus;
	bio?: string;
};

export class ProfileApiError extends Error {
	constructor(message: string) {
		super(message);
		this.name = "ProfileApiError";
	}
}

const apiBaseUrl = env.NEXT_PUBLIC_PROFILE_API_URL.replace(/\/$/, "");
const pollIntervalMs = 2_000;
const maxPollAttempts = 60;

export async function generateOrGetBio(
	name: string,
	options: {
		signal?: AbortSignal;
		onStatus?: (status: BioJobStatus) => void;
	} = {},
): Promise<PublicBio> {
	const submitted = await requestJSON(
		`${apiBaseUrl}/api/profiles/generate-bio`,
		{
			method: "POST",
			headers: { "content-type": "application/json" },
			body: JSON.stringify({ name }),
			signal: options.signal,
		},
	);
	if (isPublicBio(submitted)) {
		options.onStatus?.("completed");
		return submitted;
	}
	const initial = parseJob(submitted);
	options.onStatus?.(initial.status);

	for (let attempt = 0; attempt < maxPollAttempts; attempt += 1) {
		const job =
			attempt === 0 && initial.status === "completed"
				? initial
				: parseJob(
						await requestJSON(`${apiBaseUrl}/api/bio-jobs/${initial.jobId}`, {
							signal: options.signal,
						}),
					);
		options.onStatus?.(job.status);

		if (job.status === "completed") {
			if (typeof job.name !== "string" || typeof job.bio !== "string") {
				throw new ProfileApiError("任务完成但返回结果无效");
			}
			return { name: job.name, bio: job.bio };
		}
		if (job.status === "failed") {
			throw new ProfileApiError("Bio 生成失败，请稍后重新提交");
		}
		await delay(pollIntervalMs, options.signal);
	}

	throw new ProfileApiError("Bio 仍在生成中，请稍后再试");
}

async function requestJSON(url: string, init?: RequestInit): Promise<unknown> {
	let response: Response;
	try {
		response = await fetch(url, init);
	} catch (error) {
		if (error instanceof DOMException && error.name === "AbortError")
			throw error;
		throw new ProfileApiError("无法连接到服务，请检查网络后重试");
	}
	const payload: unknown = await response.json().catch(() => undefined);
	if (!response.ok)
		throw new ProfileApiError(readErrorMessage(response.status, payload));
	return payload;
}

function parseJob(value: unknown): BioJob {
	if (
		!isRecord(value) ||
		typeof value.jobId !== "string" ||
		!isJobStatus(value.status)
	) {
		throw new ProfileApiError("服务返回了无法识别的任务数据");
	}
	return {
		jobId: value.jobId,
		status: value.status,
		name: typeof value.name === "string" ? value.name : undefined,
		bio: typeof value.bio === "string" ? value.bio : undefined,
	};
}

function isJobStatus(value: unknown): value is BioJobStatus {
	return ["pending", "running", "completed", "failed"].includes(String(value));
}

function isPublicBio(value: unknown): value is PublicBio {
	return (
		isRecord(value) &&
		typeof value.name === "string" &&
		typeof value.bio === "string"
	);
}

function readErrorMessage(status: number, payload: unknown): string {
	if (
		isRecord(payload) &&
		typeof payload.message === "string" &&
		status < 500
	) {
		return payload.message;
	}
	if (status === 503) return "任务队列暂时不可用，请稍后再试";
	if (status >= 500) return "Bio 服务暂时不可用，请稍后再试";
	return "请求失败，请稍后再试";
}

function delay(milliseconds: number, signal?: AbortSignal): Promise<void> {
	return new Promise((resolve, reject) => {
		const abort = () => {
			clearTimeout(timer);
			reject(new DOMException("Aborted", "AbortError"));
		};
		const timer = setTimeout(() => {
			signal?.removeEventListener("abort", abort);
			resolve();
		}, milliseconds);
		if (signal?.aborted) return abort();
		signal?.addEventListener("abort", abort, { once: true });
	});
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}
