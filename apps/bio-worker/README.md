# Bio Worker Lambda

Consumes SNS-wrapped SQS messages for asynchronous bio generation. It claims a
database-backed job, calls OpenRouter, creates the profile through the private
Go service, and completes the job. Failed records are returned through Lambda
partial batch response so SQS retries and eventually moves them to the DLQ.

## Commands

```bash
pnpm --filter bio-worker test
pnpm --filter bio-worker build
```

## Runtime configuration

- `PROFILE_API_BASE_URL=http://profile-api.app.internal:8080`
- `BIO_JOB_INTERNAL_KEY` shared only by the API Lambda, Worker, and Go task
- `OPENROUTER_API_KEY` stored only in the Worker Lambda environment after cutover
- `OPENROUTER_MODEL` optional

The worker must run in the application VPC so it can resolve Cloud Map. It also
needs the existing outbound path to OpenRouter. Its log retention is 7 days.

Recommended Lambda/SQS settings are defined in
`docs/AWS_ASYNC_BIO_CONSOLE_CHECKLIST.md`.
