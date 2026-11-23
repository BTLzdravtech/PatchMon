package handler

import (
	"context"
	"net/http/httptest"
	"testing"
)

func TestNewPendingHostEnablesAgentAutoUpdate(t *testing.T) {
	h := newPendingHost("web-1", "pending-x", "patchmon_abc", "hash")
	if !h.AutoUpdate {
		t.Fatal("new hosts must default to auto_update=true so the global setting alone decides")
	}
	if h.Status != "pending" || h.MachineID == nil || *h.MachineID != "pending-x" || h.FriendlyName != "web-1" || h.ApiID != "patchmon_abc" || h.ApiKey != "hash" {
		t.Fatalf("unexpected host: %+v", h)
	}
}

// Fork policy: enrolment does not keep newPendingHost's upstream default. An
// explicit auto_update wins; without one the host inherits the global setting,
// which reads as false when settings are unavailable (see newHostAutoUpdate).
func TestCreateEnrolledHostAutoUpdateFollowsForkPolicy(t *testing.T) {
	on, off := true, false
	tests := []struct {
		name     string
		explicit *bool
		want     bool
	}{
		{"explicit true", &on, true},
		{"explicit false", &off, false},
		{"omitted without settings", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeEnrollHosts{}
			h := &AutoEnrollmentHandler{hosts: fake}
			host, _, ok := h.createEnrolledHost(context.Background(), httptest.NewRecorder(), "web-01", "lxc-1", nil, "", tt.explicit)
			if !ok || host == nil || len(fake.created) != 1 {
				t.Fatalf("expected one created host, ok=%v", ok)
			}
			if got := fake.created[0].AutoUpdate; got != tt.want {
				t.Fatalf("AutoUpdate = %v, want %v", got, tt.want)
			}
		})
	}
}
