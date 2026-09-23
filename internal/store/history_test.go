package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDetailedHistoryFilteringPinningAndBounds(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "history.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	if err := s.EnsureUser(ctx, "user"); err != nil {
		t.Fatal(err)
	}
	failed, err := s.AddHistoryDetailed(ctx, "user", "select secret", 42, 0, "conn-1", "appdb", "SELECT", false, "42501", "permission denied")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetHistoryPinned(ctx, "user", failed.ID, true); err != nil {
		t.Fatal(err)
	}
	_, err = s.AddHistoryDetailed(ctx, "user", "update items", 120, 3, "conn-2", "otherdb", "UPDATE", true, "", "")
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.SearchHistory(ctx, "user", HistoryFilter{Query: "secret", ConnectionID: "conn-1", Status: "failed", MinDurationMS: 40, PinnedOnly: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != failed.ID || got[0].Success || !got[0].Pinned || got[0].ErrorCode != "42501" || got[0].DatabaseName != "appdb" {
		t.Fatalf("unexpected filtered history: %+v", got)
	}
	if _, err := s.AddHistoryDetailed(ctx, "user", string(make([]byte, 1<<20+1)), 0, 0, "", "", "", true, "", ""); err == nil {
		t.Fatal("oversized SQL should be rejected")
	}
}
