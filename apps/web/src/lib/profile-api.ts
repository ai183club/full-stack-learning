export type PublicBio = {
	name: string;
	bio: string;
};

export class ProfileApiError extends Error {
	constructor(message: string) {
		super(message);
		this.name = "ProfileApiError";
	}
}

const apiBaseUrl = env.NEXT_PUBLIC_PROFILE_API_URL.replace(/\/$/, "");

export async function generateOrGetBio(name: string): Promise<PublicBio> {
	let response: Response;
	try {
		response = await fetch(`${apiBaseUrl}/api/profiles/generate-bio`, {
			method: "POST",
			headers: { "content-type": "application/json" },
			body: JSON.stringify({ name }),
		});
	} catch {
		throw new ProfileApiError("无法连接到服务，请检查网络后重试");
	}

	const payload: unknown = await response.json().catch(() => undefined);
	if (!response.ok) {
		throw new ProfileApiError(readErrorMessage(response.status, payload));
	}
	if (!isPublicBio(payload)) {
		throw new ProfileApiError("服务返回了无法识别的数据");
	}
	return payload;
}

function readErrorMessage(status: number, payload: unknown): string {
	if (isRecord(payload) && typeof payload.message === "string") {
		if (status >= 400 && status < 500) return payload.message;
	}
	if (status === 504) return "生成 Bio 超时，请稍后再试";
	if (status >= 500) return "Bio 服务暂时不可用，请稍后再试";
	return "请求失败，请稍后再试";
}

function isPublicBio(value: unknown): value is PublicBio {
	return (
		isRecord(value) &&
		typeof value.name === "string" &&
		typeof value.bio === "string"
	);
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}

import { env } from "@full-stack-learning/env/web";
