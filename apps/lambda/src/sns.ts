import { PublishCommand, type SNSClient } from "@aws-sdk/client-sns";

import type { BioGenerationRequestedEvent } from "./handler.js";

type PublisherOptions = {
	client: SNSClient;
	getTopicArn: () => string | undefined;
};

export function createBioJobPublisher(options: PublisherOptions) {
	return async (event: BioGenerationRequestedEvent): Promise<void> => {
		const topicArn = options.getTopicArn();
		if (!topicArn) throw new Error("BIO_JOB_TOPIC_ARN is not configured");

		await options.client.send(
			new PublishCommand({
				TopicArn: topicArn,
				Message: JSON.stringify(event),
				MessageAttributes: {
					eventType: {
						DataType: "String",
						StringValue: event.eventType,
					},
				},
			}),
		);
	};
}
