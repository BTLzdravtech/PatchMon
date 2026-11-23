package queue

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"
)

const googleChatURL = "https://chat.googleapis.com/v1/spaces/AAAAabcdEFG/messages?key=k&token=t"

func TestIsGoogleChatWebhookURL(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{"incoming webhook", googleChatURL, true},
		{"uppercase host", "https://CHAT.googleapis.com/v1/spaces/AAAA/messages?key=k&token=t", true},
		{"surrounding whitespace", "  " + googleChatURL + "\n", true},
		{"trailing slash", "https://chat.googleapis.com/v1/spaces/AAAA/messages/", true},
		{"other google api host", "https://www.googleapis.com/v1/spaces/AAAA/messages", false},
		{"lookalike host", "https://chat.googleapis.com.example.com/v1/spaces/AAAA/messages", false},
		{"not the messages endpoint", "https://chat.googleapis.com/v1/spaces/AAAA/members", false},
		{"missing space id", "https://chat.googleapis.com/v1/spaces//messages", false},
		{"extra path segment", "https://chat.googleapis.com/v1/spaces/AAAA/messages/123", false},
		{"slack", "https://hooks.slack.com/services/T/B/X", false},
		{"unparseable", "https://%zz/v1/spaces/AAAA/messages", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGoogleChatWebhookURL(tt.url); got != tt.want {
				t.Fatalf("isGoogleChatWebhookURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

// Other matchers must not claim a Chat URL, or it would be sent a body that
// Chat rejects.
func TestGoogleChatURLNotClaimedByOtherFormats(t *testing.T) {
	if isDiscordWebhookURL(googleChatURL) || isSlackIncomingWebhookURL(googleChatURL) || isSlackCompatibleWebhookURL(googleChatURL) {
		t.Fatal("a Google Chat URL was matched by another webhook format")
	}
}

// Chat answers 400 to any field it does not know, so the body must be exactly
// {"text": "..."}.
func assertOnlyText(t *testing.T, b []byte) string {
	t.Helper()
	var decoded map[string]interface{}
	if err := json.Unmarshal(b, &decoded); err != nil {
		t.Fatalf("unmarshal %q: %v", b, err)
	}
	if len(decoded) != 1 {
		t.Fatalf("body has keys other than text: %v", decoded)
	}
	text, ok := decoded["text"].(string)
	if !ok || strings.TrimSpace(text) == "" {
		t.Fatalf("body has no usable text: %v", decoded)
	}
	return text
}

func TestGoogleChatNotificationBody(t *testing.T) {
	b, err := googleChatWebhookBody(testPayload())
	if err != nil {
		t.Fatalf("googleChatWebhookBody: %v", err)
	}
	text := assertOnlyText(t, b)
	p := testPayload()
	if !strings.Contains(text, p.Title) {
		t.Errorf("text lost the title %q: %q", p.Title, text)
	}
	if !strings.Contains(text, "<https://patchmon.example/hosts/abc-123|") {
		t.Errorf("text lost the PatchMon link: %q", text)
	}
}

func TestGoogleChatNotificationBodyIsTruncated(t *testing.T) {
	p := testPayload()
	p.Message = strings.Repeat("ž", 3*googleChatTextMaxRunes)
	b, err := googleChatWebhookBody(p)
	if err != nil {
		t.Fatalf("googleChatWebhookBody: %v", err)
	}
	text := assertOnlyText(t, b)
	if n := utf8.RuneCountInString(text); n > googleChatTextMaxRunes {
		t.Fatalf("text is %d runes, limit %d", n, googleChatTextMaxRunes)
	}
	if !utf8.ValidString(text) {
		t.Fatal("truncation split a multi-byte character")
	}
}

func TestGoogleChatScheduledReportBody(t *testing.T) {
	b, err := googleChatScheduledReportWebhookBody(reportSubject, reportHTML)
	if err != nil {
		t.Fatalf("googleChatScheduledReportWebhookBody: %v", err)
	}
	text := assertOnlyText(t, b)
	if !strings.Contains(text, reportSubject) {
		t.Errorf("text lost the report subject %q: %q", reportSubject, text)
	}
}
