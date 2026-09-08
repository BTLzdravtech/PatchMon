package handler

import (
	"context"
	"testing"
)

func TestNewHostAutoUpdate(t *testing.T) {
	ctx := context.Background()
	tr, fa := true, false
	if !newHostAutoUpdate(ctx, nil, &tr) {
		t.Error("explicit true must win")
	}
	if newHostAutoUpdate(ctx, nil, &fa) {
		t.Error("explicit false must win")
	}
	if newHostAutoUpdate(ctx, nil, nil) {
		t.Error("no settings store and no explicit value must default to false")
	}
}
