package handler

import (
	"context"

	"github.com/PatchMon/PatchMon/server-source-code/internal/store"
)

// newHostAutoUpdate decides the agent auto-update flag for a host being
// created. An explicit value from the request wins; otherwise the host
// inherits the global "agent auto-update" setting so an org that has
// auto-update switched on gets it on new hosts without touching every row
// by hand afterwards. Falls back to false when settings cannot be read.
//
// Historically neither enrollment path set the field at all, so every host
// came out disabled regardless of the global toggle or the column default.
func newHostAutoUpdate(ctx context.Context, settings *store.SettingsStore, explicit *bool) bool {
	if explicit != nil {
		return *explicit
	}
	if settings == nil {
		return false
	}
	s, err := settings.GetFirst(ctx)
	if err != nil || s == nil {
		return false
	}
	return s.AutoUpdate
}
