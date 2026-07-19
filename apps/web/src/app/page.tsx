"use client";

import { type FormEvent, useEffect, useRef, useState } from "react";

import {
	type BioJobStatus,
	generateOrGetBio,
	ProfileApiError,
	type PublicBio,
} from "@/lib/profile-api";

export default function Home() {
	const [name, setName] = useState("");
	const [profile, setProfile] = useState<PublicBio | null>(null);
	const [error, setError] = useState<string | null>(null);
	const [isSubmitting, setIsSubmitting] = useState(false);
	const [jobStatus, setJobStatus] = useState<BioJobStatus | null>(null);
	const requestController = useRef<AbortController | null>(null);

	useEffect(() => () => requestController.current?.abort(), []);

	async function handleSubmit(event: FormEvent<HTMLFormElement>) {
		event.preventDefault();
		if (isSubmitting) return;

		const trimmedName = name.trim();
		if (!trimmedName) {
			setProfile(null);
			setError("请输入一个 Name");
			return;
		}
		if (Array.from(trimmedName).length > 80) {
			setProfile(null);
			setError("Name 最多 80 个字符");
			return;
		}

		setError(null);
		setJobStatus("pending");
		setIsSubmitting(true);
		requestController.current?.abort();
		const controller = new AbortController();
		requestController.current = controller;
		try {
			const result = await generateOrGetBio(trimmedName, {
				signal: controller.signal,
				onStatus: setJobStatus,
			});
			setProfile(result);
			setName(result.name);
		} catch (requestError) {
			if (
				requestError instanceof DOMException &&
				requestError.name === "AbortError"
			)
				return;
			setProfile(null);
			setError(
				requestError instanceof ProfileApiError
					? requestError.message
					: "暂时无法生成 Bio，请稍后再试",
			);
		} finally {
			if (requestController.current === controller) {
				requestController.current = null;
				setIsSubmitting(false);
			}
		}
	}

	return (
		<main className="profile-home">
			<section className="profile-search" aria-labelledby="page-title">
				<div className="brand">
					<span className="brand-word">BioNote</span>
					<span className="brand-pixel" aria-hidden="true" />
				</div>
				<h1 id="page-title" className="sr-only">
					通过 Name 查询或生成个人 Bio
				</h1>

				<form className="search-form" onSubmit={handleSubmit} noValidate>
					<label className="sr-only" htmlFor="profile-name">
						Name
					</label>
					<div className="search-field">
						<input
							id="profile-name"
							name="name"
							type="text"
							value={name}
							onChange={(event) => setName(event.target.value)}
							placeholder="输入你的 Name"
							autoComplete="name"
							maxLength={80}
							disabled={isSubmitting}
							aria-describedby={error ? "profile-error" : undefined}
							aria-invalid={Boolean(error)}
						/>
						<button
							className="generate-button"
							type="submit"
							disabled={isSubmitting}
						>
							{isSubmitting ? (
								<>
									<span className="button-spinner" aria-hidden="true" />
									{jobStatus === "pending" ? "正在排队" : "正在生成"}
								</>
							) : (
								<>
									生成 Bio <span aria-hidden="true">→</span>
								</>
							)}
						</button>
					</div>
				</form>

				<div className="response-region" aria-live="polite">
					{isSubmitting ? (
						<p className="result-label">
							{jobStatus === "pending"
								? "任务已进入队列"
								: "Worker 正在生成 Bio"}
						</p>
					) : null}
					{error ? (
						<p id="profile-error" className="form-error" role="alert">
							{error}
						</p>
					) : null}

					{profile ? (
						<article className="bio-result">
							<p className="result-label">{profile.name}</p>
							<p className="result-bio">{profile.bio}</p>
						</article>
					) : null}
				</div>
			</section>
		</main>
	);
}
