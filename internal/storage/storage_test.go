package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestStorage(t *testing.T) *Storage {
	t.Helper()

	s, err := New(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}

	return s
}

func TestStorageRoundTrip(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	if err := s.CreateGroup(ctx, "devs", 1); err != nil {
		t.Fatalf("create group: %v", err)
	}

	group, err := s.GetGroup(ctx, "devs", 1)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}

	user, err := s.CreateOrUpdateUser(ctx, 10, "alice", "Alice", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := s.AddMember(ctx, group.ID, user); err != nil {
		t.Fatalf("add member: %v", err)
	}

	members, err := s.GetGroupMembers(ctx, group.ID)
	if err != nil || len(members) != 1 || members[0].User.Username != "alice" {
		t.Fatalf("unexpected members %+v, err %v", members, err)
	}
}
