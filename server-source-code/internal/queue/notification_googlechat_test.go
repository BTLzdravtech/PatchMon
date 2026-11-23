package queue

import (
	"bytes"
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

// Chat answers 400 to any field it does not know. These are the only
// top-level and card fields the body may use.
type gcWidget struct {
	TextParagraph *struct {
		Text string `json:"text"`
	} `json:"textParagraph"`
	DecoratedText *struct {
		TopLabel string `json:"topLabel"`
		Text     string `json:"text"`
		WrapText bool   `json:"wrapText"`
	} `json:"decoratedText"`
	ButtonList *struct {
		Buttons []struct {
			Text    string `json:"text"`
			OnClick struct {
				OpenLink struct {
					URL string `json:"url"`
				} `json:"openLink"`
			} `json:"onClick"`
		} `json:"buttons"`
	} `json:"buttonList"`
}

type gcMessage struct {
	FallbackText string `json:"fallbackText"`
	CardsV2      []struct {
		CardID string `json:"cardId"`
		Card   struct {
			Header struct {
				Title    string `json:"title"`
				Subtitle string `json:"subtitle"`
			} `json:"header"`
			Sections []struct {
				Widgets []gcWidget `json:"widgets"`
			} `json:"sections"`
		} `json:"card"`
	} `json:"cardsV2"`
}

// decodeGoogleChat fails on any field outside gcMessage, which is how Chat
// itself treats an unknown field.
func decodeGoogleChat(t *testing.T, b []byte) (gcMessage, []gcWidget) {
	t.Helper()
	var m gcMessage
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("body uses a field Chat would reject: %v\n%s", err, b)
	}
	if len(m.CardsV2) != 1 || m.CardsV2[0].CardID == "" || len(m.CardsV2[0].Card.Sections) != 1 {
		t.Fatalf("want exactly one card with one section: %s", b)
	}
	if strings.TrimSpace(m.CardsV2[0].Card.Header.Title) == "" {
		t.Fatalf("card header title is required: %s", b)
	}
	if strings.TrimSpace(m.FallbackText) == "" {
		t.Fatalf("fallbackText is empty: %s", b)
	}
	widgets := m.CardsV2[0].Card.Sections[0].Widgets
	if len(widgets) == 0 {
		t.Fatalf("a section must contain at least one widget: %s", b)
	}
	return m, widgets
}

func TestGoogleChatNotificationCard(t *testing.T) {
	p := testPayload()
	b, err := googleChatWebhookBody(p)
	if err != nil {
		t.Fatalf("googleChatWebhookBody: %v", err)
	}
	m, widgets := decodeGoogleChat(t, b)

	header := m.CardsV2[0].Card.Header
	if !strings.Contains(header.Title, p.Title) {
		t.Errorf("header title %q lost the alert title %q", header.Title, p.Title)
	}
	if !strings.Contains(header.Subtitle, strings.ToUpper(p.Severity)) {
		t.Errorf("header subtitle %q lost the severity", header.Subtitle)
	}
	if !strings.Contains(m.FallbackText, p.Title) {
		t.Errorf("fallbackText %q lost the alert title", m.FallbackText)
	}

	var sawMessage, sawField, sawLink bool
	for _, w := range widgets {
		switch {
		case w.TextParagraph != nil:
			sawMessage = sawMessage || strings.Contains(w.TextParagraph.Text, p.Message)
		case w.DecoratedText != nil:
			if w.DecoratedText.TopLabel == "" || w.DecoratedText.Text == "" {
				t.Errorf("detail row with an empty label or value: %+v", *w.DecoratedText)
			}
			sawField = true
		case w.ButtonList != nil:
			for _, btn := range w.ButtonList.Buttons {
				sawLink = sawLink || btn.OnClick.OpenLink.URL == "https://patchmon.example/hosts/abc-123"
			}
		}
	}
	if !sawMessage || !sawField || !sawLink {
		t.Fatalf("card is missing parts: message=%v detail=%v link=%v\n%s", sawMessage, sawField, sawLink, b)
	}
}

// Card text is rendered as HTML, so event content must arrive escaped, and
// the Markdown code fence around error output must not leak into the card.
func TestGoogleChatNotificationCardEscapesAndUnfences(t *testing.T) {
	p := testPayload()
	p.EventType = "patch_run_failed"
	p.Title = "Patch <failed>"
	p.Message = "apt said: <b>held</b> & broken"
	p.Metadata["host_name"] = "web-01"
	p.Metadata["stage"] = "failed"
	p.Metadata["error_message"] = "E: <unmet> dependencies"

	b, err := googleChatWebhookBody(p)
	if err != nil {
		t.Fatalf("googleChatWebhookBody: %v", err)
	}
	m, widgets := decodeGoogleChat(t, b)
	if !strings.Contains(m.CardsV2[0].Card.Header.Title, "Patch &lt;failed&gt;") {
		t.Errorf("header title not escaped: %q", m.CardsV2[0].Card.Header.Title)
	}
	var sawError bool
	for _, w := range widgets {
		if w.TextParagraph != nil && strings.Contains(w.TextParagraph.Text, "<b>") {
			t.Errorf("message not escaped: %q", w.TextParagraph.Text)
		}
		if w.DecoratedText != nil && w.DecoratedText.TopLabel == "Error" {
			sawError = true
			if w.DecoratedText.Text != "E: &lt;unmet&gt; dependencies" {
				t.Errorf("error row = %q, want it escaped and without a code fence", w.DecoratedText.Text)
			}
		}
	}
	if !sawError {
		t.Fatalf("no Error row in the card: %s", b)
	}
}

// Chat caps cardsV2 at 32 KB and 100 widgets; an oversized event must still
// produce a deliverable card.
func TestGoogleChatNotificationCardIsBounded(t *testing.T) {
	p := testPayload()
	p.EventType = "compliance_scan_completed"
	p.Message = strings.Repeat("ž", 50000)
	profiles := make([]interface{}, 500)
	for i := range profiles {
		profiles[i] = map[string]interface{}{"profile": strings.Repeat("p", 5000), "score": "50"}
	}
	p.Metadata["profile_summaries"] = profiles

	b, err := googleChatWebhookBody(p)
	if err != nil {
		t.Fatalf("googleChatWebhookBody: %v", err)
	}
	_, widgets := decodeGoogleChat(t, b)
	if len(widgets) > 100 {
		t.Fatalf("%d widgets, Chat allows 100", len(widgets))
	}
	if len(b) > 32*1024 {
		t.Fatalf("body is %d bytes, Chat allows 32 KB of cards", len(b))
	}
	if !utf8.Valid(b) {
		t.Fatal("truncation split a multi-byte character")
	}
}

func TestGoogleChatScheduledReportCard(t *testing.T) {
	b, err := googleChatScheduledReportWebhookBody(reportSubject, reportHTML)
	if err != nil {
		t.Fatalf("googleChatScheduledReportWebhookBody: %v", err)
	}
	m, widgets := decodeGoogleChat(t, b)
	if !strings.Contains(m.CardsV2[0].Card.Header.Title, reportSubject) {
		t.Errorf("header title %q lost the report subject", m.CardsV2[0].Card.Header.Title)
	}
	if widgets[0].TextParagraph == nil || !strings.Contains(widgets[0].TextParagraph.Text, "12 hosts need updates") {
		t.Errorf("report preview missing or still HTML: %+v", widgets[0])
	}
}
