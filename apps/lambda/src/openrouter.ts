type Fetch = (
	input: string | URL | Request,
	init?: RequestInit,
) => Promise<Response>;

export class BioGenerationError extends Error {
	constructor(
		message: string,
		readonly kind:
			| "configuration"
			| "timeout"
			| "upstream"
			| "invalid-response",
		readonly statusCode?: number,
	) {
		super(message);
		this.name = "BioGenerationError";
	}
}

type OpenRouterDependencies = {
	fetch: Fetch;
	getApiKey: () => string | undefined;
	getModel: () => string | undefined;
	requestTimeoutMs: number;
	retryDelayMs?: number;
	sleep?: (milliseconds: number) => Promise<void>;
};

const defaultModel = "google/gemma-4-31b-it:free";
const endpoint = "https://openrouter.ai/api/v1/chat/completions";

export function createOpenRouterBioGenerator(
	dependencies: OpenRouterDependencies,
) {
	return async function generateBio(name: string): Promise<string> {
		const apiKey = dependencies.getApiKey()?.trim();
		if (!apiKey) {
			throw new BioGenerationError(
				"OPENROUTER_API_KEY is not configured",
				"configuration",
			);
		}

		const controller = new AbortController();
		const timer = setTimeout(
			() => controller.abort(),
			dependencies.requestTimeoutMs,
		);
		try {
			const request = createRequest(apiKey, name, dependencies.getModel());
			for (let attempt = 0; attempt < 2; attempt += 1) {
				const response = await dependencies.fetch(endpoint, {
					...request,
					signal: controller.signal,
				});

				if (response.ok) {
					const payload: unknown = await response.json();
					const content = extractAssistantContent(payload);
					if (!content) {
						throw new BioGenerationError(
							"OpenRouter response did not contain assistant content",
							"invalid-response",
						);
					}
					return content;
				}

				if (attempt === 0 && isRetryableStatus(response.status)) {
					await (dependencies.sleep ?? sleep)(dependencies.retryDelayMs ?? 400);
					continue;
				}

				throw new BioGenerationError(
					`OpenRouter returned HTTP ${response.status}`,
					"upstream",
					response.status,
				);
			}

			throw new BioGenerationError("OpenRouter request failed", "upstream");
		} catch (error) {
			if (error instanceof BioGenerationError) throw error;
			if (error instanceof Error && error.name === "AbortError") {
				throw new BioGenerationError("OpenRouter request timed out", "timeout");
			}
			throw new BioGenerationError("OpenRouter request failed", "upstream");
		} finally {
			clearTimeout(timer);
		}
	};
}

function createRequest(
	apiKey: string,
	name: string,
	configuredModel: string | undefined,
): RequestInit {
	return {
		method: "POST",
		headers: {
			Authorization: `Bearer ${apiKey}`,
			"Content-Type": "application/json",
		},
		body: JSON.stringify({
			model: configuredModel?.trim() || defaultModel,
			messages: [
				{
					role: "system",
					content:
						"你是一名专业的中文个人简介撰稿人。请只使用自然、流畅的简体中文输出最终个人简介，不得使用英文，不得输出 Markdown、标题、标签、分析过程或引号。内容不得超过 500 个 Unicode 字符。",
				},
				{
					role: "user",
					content: `请为名为 ${JSON.stringify(name)} 的人撰写一段温暖、自然且适合公开展示的中文个人简介。可以根据名字营造克制的表达风格，但不要虚构年龄、性别、职业、学历、雇主、所在地、具体成就或其他敏感事实。`,
				},
			],
			max_tokens: 220,
			reasoning: { enabled: true, max_tokens: 64, exclude: true },
		}),
	};
}

function isRetryableStatus(statusCode: number): boolean {
	return statusCode === 429 || [502, 503, 504].includes(statusCode);
}

function sleep(milliseconds: number): Promise<void> {
	return new Promise((resolve) => setTimeout(resolve, milliseconds));
}

function extractAssistantContent(payload: unknown): string | undefined {
	if (!isRecord(payload) || !Array.isArray(payload.choices)) return undefined;
	const firstChoice = payload.choices[0];
	if (!isRecord(firstChoice) || !isRecord(firstChoice.message))
		return undefined;
	const content = firstChoice.message.content;
	if (typeof content === "string") return content.trim() || undefined;
	if (!Array.isArray(content)) return undefined;
	const text = content
		.filter(isRecord)
		.map((part) => (typeof part.text === "string" ? part.text : ""))
		.join("")
		.trim();
	return text || undefined;
}

function isRecord(value: unknown): value is Record<string, unknown> {
	return typeof value === "object" && value !== null && !Array.isArray(value);
}
