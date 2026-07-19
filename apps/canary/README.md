# Production Synthetics Canary

This directory contains the repository-owned configuration for the production
CloudWatch Synthetics Multi-checks canary. AWS resource creation remains a
manual Console exercise.

## Coverage

The canary performs three read-only public checks in order:

1. `GET https://full-stack.seebyte.xyz`
2. `GET https://f7a52mymmg.execute-api.ap-northeast-1.amazonaws.com/health`
3. `GET https://f7a52mymmg.execute-api.ap-northeast-1.amazonaws.com/ready`

The Web check asserts HTTP 200 and HTML. The API checks assert HTTP 200, JSON,
and the existing `status` response contract. It does not generate a bio, write
to PostgreSQL, send credentials, or use `OPENROUTER_API_KEY`.

## Local validation

From the repository root:

```bash
node apps/canary/validate.mjs
```

This is an offline structural check. It deliberately does not call production
endpoints or AWS.

## Console deployment

Use the exact settings in
`docs/AWS_SYNTHETICS_CONSOLE_CHECKLIST.md`. In the Multi-checks blueprint editor,
replace the generated configuration with `blueprint-config.json`.

Use the latest runtime displayed by the Console that supports Multi-checks
(`syn-nodejs-3.0` or later). Do not attach the canary to the application VPC.

## Updating

1. Edit `blueprint-config.json` in the repository.
2. Run the offline validator.
3. In the Canary Console choose **Edit**, replace the Multi-checks configuration,
   and first run it once.
4. Confirm all three steps pass before restoring the recurring schedule.

The repository does not automatically deploy this configuration in the first
learning iteration.

## Stopping and deleting

- Temporary cost stop: open the canary and choose **Stop**. The configuration,
  artifacts, logs, and alarm remain.
- Full cleanup: delete the canary, then review its generated Lambda function,
  execution role, CloudWatch log group, S3 artifacts, alarm, and SNS topic.
- The SNS email topic can be retained for later SQS/DLQ alarms, but only after
  confirming that no unwanted alarm actions still reference it.

Stopping the canary does not delete retained S3 artifacts or log data; their
lifecycle and retention settings must be configured separately.
