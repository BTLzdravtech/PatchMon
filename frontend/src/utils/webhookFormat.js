/**
 * Mirrors the webhook payload-format detection in
 * server-source-code/internal/queue/notification_worker.go
 * (isDiscordWebhookURL, isSlackIncomingWebhookURL, isSlackCompatibleWebhookURL)
 * and notification_googlechat.go (isGoogleChatWebhookURL).
 *
 * Keep the two in sync, and keep both test suites passing. This exists so the
 * destination form can show which payload the server will send before saving;
 * the server remains the authority on what is actually sent.
 */

const DISCORD_HOSTS = new Set([
	"discord.com",
	"discordapp.com",
	"www.discord.com",
]);

const GENERIC_INGEST_HOSTS = new Set([
	"automation.atlassian.com",
	"automation.codebarrel.io",
]);

const SLACK_COMPATIBLE_TOKEN = /^[A-Za-z0-9_-]{15,}$/;

export const WEBHOOK_FORMATS = {
	DISCORD: "discord",
	SLACK: "slack",
	SLACK_COMPATIBLE: "slack_compatible",
	GOOGLE_CHAT: "google_chat",
	GENERIC: "generic",
};

const LABELS = {
	[WEBHOOK_FORMATS.DISCORD]: {
		label: "Discord",
		detail: "Rich embed with severity colour",
	},
	[WEBHOOK_FORMATS.SLACK]: {
		label: "Slack",
		detail: "Slack incoming-webhook message",
	},
	[WEBHOOK_FORMATS.SLACK_COMPATIBLE]: {
		label: "Mattermost or Rocket.Chat",
		detail: "Slack-compatible message",
	},
	[WEBHOOK_FORMATS.GOOGLE_CHAT]: {
		label: "Google Chat",
		detail: "Plain text message for a Chat space",
	},
	[WEBHOOK_FORMATS.GENERIC]: {
		label: "Generic JSON",
		detail: "Structured PatchMon fields, plus a top-level text",
	},
};

/**
 * Returns the payload format PatchMon will send to this URL, or null when the
 * URL is not yet parseable (so a half-typed URL shows no hint at all).
 *
 * Note: unlike Go's url.Parse, the URL constructor rejects a scheme-less string.
 * Such a URL cannot be delivered anyway, so returning null is the useful answer.
 */
export function detectWebhookFormat(raw) {
	const trimmed = String(raw ?? "").trim();
	if (!trimmed) return null;

	let url;
	try {
		url = new URL(trimmed);
	} catch {
		return null;
	}
	if (url.protocol !== "http:" && url.protocol !== "https:") return null;

	const host = url.hostname.toLowerCase();
	// pathname keeps percent-encoding, matching Go's EscapedPath.
	const path = url.pathname;

	if (DISCORD_HOSTS.has(host) && path.includes("/api/webhooks/")) {
		return WEBHOOK_FORMATS.DISCORD;
	}
	if (host === "hooks.slack.com" && path.startsWith("/services/")) {
		return WEBHOOK_FORMATS.SLACK;
	}
	if (isSlackCompatible(host, path)) {
		return WEBHOOK_FORMATS.SLACK_COMPATIBLE;
	}
	if (isGoogleChat(host, path)) {
		return WEBHOOK_FORMATS.GOOGLE_CHAT;
	}
	return WEBHOOK_FORMATS.GENERIC;
}

function isSlackCompatible(host, path) {
	if (GENERIC_INGEST_HOSTS.has(host)) return false;

	const segments = path.replace(/^\/+|\/+$/g, "").split("/");
	for (let i = 0; i < segments.length; i++) {
		if (segments[i].toLowerCase() !== "hooks") continue;
		const rest = segments.slice(i + 1);
		if (rest.length === 0 || rest.length > 2) return false;
		return rest.every((token) => SLACK_COMPATIBLE_TOKEN.test(token));
	}
	return false;
}

function isGoogleChat(host, path) {
	if (host !== "chat.googleapis.com") return false;
	const segments = path.replace(/^\/+|\/+$/g, "").split("/");
	return (
		segments.length === 4 &&
		segments[0] === "v1" &&
		segments[1] === "spaces" &&
		segments[2] !== "" &&
		segments[3] === "messages"
	);
}

/** Human-readable name and one-line description for a format, or null. */
export function webhookFormatLabel(format) {
	return LABELS[format] ?? null;
}
