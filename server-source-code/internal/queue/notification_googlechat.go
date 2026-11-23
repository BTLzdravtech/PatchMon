package queue

import (
	"encoding/json"
	"html"
	"net/url"
	"strings"

	"github.com/PatchMon/PatchMon/server-source-code/internal/notifications"
)

// Google Chat incoming webhooks (fork feature).
//
// Chat needs its own format because its API is strict: a body carrying any
// field it does not know ("event_type", "username", "icon_emoji", ...) is
// rejected with 400 "Unknown name". Neither the generic body nor the Slack
// body gets through, so a Chat URL gets a body built only from documented
// Message fields: a cardsV2 card plus fallbackText.
//
// Card text is rendered as HTML, so every value from an event is escaped.

const (
	// Chat caps cardsV2 at 32 KB and 100 widgets per card. These bounds keep
	// a card far below both whatever an event carries.
	googleChatParagraphMaxRunes = 2000
	googleChatFieldMaxRunes     = 500
	googleChatMaxFields         = 30
	// googleChatFallbackMaxRunes bounds the plain-text line used where the
	// card cannot be shown, such as a mobile notification.
	googleChatFallbackMaxRunes = 300
)

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

// googleChatText prepares an event value for a card: bounded, then escaped,
// because Chat renders card text as HTML.
func googleChatText(s string, maxRunes int) string {
	return html.EscapeString(truncateUTF8(strings.TrimSpace(s), maxRunes))
}

// googleChatFieldValue drops the Markdown code fence the shared field builder
// wraps around error output; a card would show the backticks literally.
func googleChatFieldValue(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "```")
	v = strings.TrimSuffix(v, "```")
	return strings.TrimSpace(v)
}

func googleChatMessage(cardID, title, subtitle, fallback string, widgets []map[string]interface{}) ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"fallbackText": truncateUTF8(fallback, googleChatFallbackMaxRunes),
		"cardsV2": []map[string]interface{}{{
			"cardId": cardID,
			"card": map[string]interface{}{
				"header": map[string]interface{}{
					"title":    googleChatText(title, 200),
					"subtitle": googleChatText(subtitle, 200),
				},
				"sections": []map[string]interface{}{{"widgets": widgets}},
			},
		}},
	})
}

// googleChatWebhookBody renders an alert as one card: severity and title in
// the header, the message, one labelled row per event detail (the same details
// the Discord embed shows), and a button that opens the item in PatchMon.
func googleChatWebhookBody(p notifications.NotificationDeliverPayload) ([]byte, error) {
	sev := strings.TrimSpace(p.Severity)
	if sev == "" {
		sev = "notice"
	}
	title := strings.TrimSpace(p.Title)
	if title == "" {
		title = "PatchMon"
	}
	msg := strings.TrimSpace(p.Message)

	var widgets []map[string]interface{}
	if msg != "" {
		widgets = append(widgets, map[string]interface{}{
			"textParagraph": map[string]interface{}{"text": googleChatText(msg, googleChatParagraphMaxRunes)},
		})
	}
	for i, f := range buildNOCFields(p) {
		if i >= googleChatMaxFields {
			break
		}
		name, _ := f["name"].(string)
		value, _ := f["value"].(string)
		widgets = append(widgets, map[string]interface{}{
			"decoratedText": map[string]interface{}{
				"topLabel": googleChatText(name, 100),
				"text":     googleChatText(googleChatFieldValue(value), googleChatFieldMaxRunes),
				"wrapText": true,
			},
		})
	}
	if link := nocMetaStr(p.Metadata, "app_link"); link != "" {
		widgets = append(widgets, map[string]interface{}{
			"buttonList": map[string]interface{}{
				"buttons": []map[string]interface{}{{
					"text":    "View in PatchMon",
					"onClick": map[string]interface{}{"openLink": map[string]interface{}{"url": link}},
				}},
			},
		})
	}
	if len(widgets) == 0 {
		// A section must contain at least one widget.
		widgets = append(widgets, map[string]interface{}{
			"textParagraph": map[string]interface{}{"text": googleChatText(title, googleChatParagraphMaxRunes)},
		})
	}

	fallback := severityEmoji(sev) + " " + title
	if msg != "" {
		fallback += ": " + msg
	}
	return googleChatMessage("patchmon-alert", severityEmoji(sev)+" "+title, "PatchMon · "+strings.ToUpper(sev), fallback, widgets)
}

// googleChatScheduledReportWebhookBody sends the short report preview as a
// card. The CSV is left out: it would not fit in a card.
func googleChatScheduledReportWebhookBody(subject, htmlBody string) ([]byte, error) {
	subj := strings.TrimSpace(subject)
	if subj == "" {
		subj = "(no subject)"
	}
	preview := stripScheduledReportHTML(htmlBody)
	if preview == "" {
		preview = "No HTML body in this delivery"
	}
	widgets := []map[string]interface{}{{
		"textParagraph": map[string]interface{}{"text": googleChatText(preview, googleChatParagraphMaxRunes)},
	}}
	return googleChatMessage("patchmon-report", "📊 "+subj, "PatchMon · Scheduled report", "PatchMon scheduled report: "+subj, widgets)
}
