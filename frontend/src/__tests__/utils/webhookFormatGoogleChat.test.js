import { describe, expect, it } from "vitest";
import {
	detectWebhookFormat,
	WEBHOOK_FORMATS,
	webhookFormatLabel,
} from "../../utils/webhookFormat";

// Mirrors TestIsGoogleChatWebhookURL in
// server-source-code/internal/queue/notification_googlechat_test.go.
// Keep the two tables in step.
describe("detectWebhookFormat: Google Chat", () => {
	it.each([
		[
			"incoming webhook",
			"https://chat.googleapis.com/v1/spaces/AAAAabcdEFG/messages?key=k&token=t",
		],
		[
			"uppercase host",
			"https://CHAT.googleapis.com/v1/spaces/AAAA/messages?key=k&token=t",
		],
		[
			"surrounding whitespace",
			"  https://chat.googleapis.com/v1/spaces/AAAA/messages?key=k&token=t\n",
		],
		["trailing slash", "https://chat.googleapis.com/v1/spaces/AAAA/messages/"],
	])("%s -> google_chat", (_name, url) => {
		expect(detectWebhookFormat(url)).toBe(WEBHOOK_FORMATS.GOOGLE_CHAT);
	});

	it.each([
		[
			"other google api host",
			"https://www.googleapis.com/v1/spaces/AAAA/messages",
		],
		[
			"lookalike host",
			"https://chat.googleapis.com.example.com/v1/spaces/AAAA/messages",
		],
		[
			"not the messages endpoint",
			"https://chat.googleapis.com/v1/spaces/AAAA/members",
		],
		["missing space id", "https://chat.googleapis.com/v1/spaces//messages"],
		[
			"extra path segment",
			"https://chat.googleapis.com/v1/spaces/AAAA/messages/123",
		],
	])("%s -> generic", (_name, url) => {
		expect(detectWebhookFormat(url)).toBe(WEBHOOK_FORMATS.GENERIC);
	});

	it("has a label for the destination form", () => {
		expect(webhookFormatLabel(WEBHOOK_FORMATS.GOOGLE_CHAT)?.label).toBe(
			"Google Chat",
		);
	});
});
