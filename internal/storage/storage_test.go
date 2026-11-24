package storage

import (
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

	if err := s.CreateGroup("devs", 1); err != nil {
		t.Fatalf("create group: %v", err)
	}

	group, err := s.GetGroup("devs", 1)
	if err != nil {
		t.Fatalf("get group: %v", err)
	}

	user, err := s.CreateOrUpdateUser(10, "alice", "Alice", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := s.AddMember(group.ID, user); err != nil {
		t.Fatalf("add member: %v", err)
	}

	members, err := s.GetGroupMembers(group.ID)
	if err != nil || len(members) != 1 || members[0].User.Username != "alice" {
		t.Fatalf("unexpected members %+v, err %v", members, err)
	}
}
