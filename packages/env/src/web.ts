import { createEnv } from "@t3-oss/env-nextjs";
import { z } from "zod";

export const env = createEnv({
	client: {
		NEXT_PUBLIC_PROFILE_API_URL: z.string().url(),
	},
	experimental__runtimeEnv: {
		NEXT_PUBLIC_PROFILE_API_URL: process.env.NEXT_PUBLIC_PROFILE_API_URL,
	},
	skipValidation: Boolean(process.env.SKIP_ENV_VALIDATION),
	emptyStringAsUndefined: true,
});
