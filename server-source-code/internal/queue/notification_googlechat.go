package queue

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/PatchMon/PatchMon/server-source-code/internal/notifications"
)

// Google Chat incoming webhooks (fork feature).
//
// Chat needs its own format because its API is strict: a body carrying any
// field it does not know ("event_type", "username", "icon_emoji", ...) is
// rejected with 400 "Unknown name". Neither the generic body nor the Slack
// body gets through, so a Chat URL must be sent {"text": ...} and nothing else.

// googleChatTextMaxRunes stays under Chat's 4096-character limit for a
// message's text.
const googleChatTextMaxRunes = 4000

// isGoogleChatWebhookURL matches
// https://chat.googleapis.com/v1/spaces/<space>/messages?key=...&token=...
func isGoogleChatWebhookURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	if strings.ToLower(u.Hostname()) != "chat.googleapis.com" {
		return false
	}
	segs := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	return len(segs) == 4 && segs[0] == "v1" && segs[1] == "spaces" && segs[2] != "" && segs[3] == "messages"
}

// googleChatWebhookBody reuses the Slack text: Chat renders the same *bold*
// and <url|label> link markup.
func googleChatWebhookBody(p notifications.NotificationDeliverPayload) ([]byte, error) {
	return json.Marshal(map[string]string{
		"text": truncateUTF8(slackIncomingWebhookText(p), googleChatTextMaxRunes),
	})
}

// googleChatScheduledReportWebhookBody sends the short report preview. The CSV
// is left out: it would not fit in a single Chat message.
func googleChatScheduledReportWebhookBody(subject, html string) ([]byte, error) {
	return json.Marshal(map[string]string{
		"text": truncateUTF8(genericScheduledReportText(subject, html), googleChatTextMaxRunes),
	})
}
