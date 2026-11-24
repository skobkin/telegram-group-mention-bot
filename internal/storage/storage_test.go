package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"

	_ "github.com/glebarez/go-sqlite"
)

func newTestStorage(t *testing.T) *Storage {
	t.Helper()

	s, err := New(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("open storage: %v", err)
	}

	t.Cleanup(func() { _ = s.Close() })

	return s
}

func mustGroup(t *testing.T, s *Storage, name string, chatID int64) *MentionGroup {
	t.Helper()

	if err := s.CreateGroup(context.Background(), name, chatID); err != nil {
		t.Fatalf("create group %s: %v", name, err)
	}

	group, err := s.GetGroup(context.Background(), name, chatID)
	if err != nil {
		t.Fatalf("get group %s: %v", name, err)
	}

	return group
}

func mustUser(t *testing.T, s *Storage, id int64, username string) *User {
	t.Helper()

	user, err := s.CreateOrUpdateUser(context.Background(), id, username, "First", "")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	return user
}

func TestStorageRoundTrip(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	group := mustGroup(t, s, "devs", 1)
	user := mustUser(t, s, 10, "alice")

	added, err := s.AddMember(ctx, group.ID, user)
	if err != nil || !added {
		t.Fatalf("add member: added=%v err=%v", added, err)
	}

	members, err := s.GetGroupMembers(ctx, group.ID)
	if err != nil || len(members) != 1 || members[0].User.Username != "alice" {
		t.Fatalf("unexpected members %+v, err %v", members, err)
	}
}

func TestAddMemberIsIdempotent(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	group := mustGroup(t, s, "devs", 1)
	user := mustUser(t, s, 10, "alice")

	if added, err := s.AddMember(ctx, group.ID, user); err != nil || !added {
		t.Fatalf("first add: %v %v", added, err)
	}

	if added, err := s.AddMember(ctx, group.ID, user); err != nil || added {
		t.Fatalf("second add must be a no-op without error: added=%v err=%v", added, err)
	}
}

func TestParallelJoinsProduceOneRow(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	group := mustGroup(t, s, "devs", 1)
	user := mustUser(t, s, 10, "alice")

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		inserts int
	)

	for range 20 {
		wg.Add(1)

		go func() {
			defer wg.Done()

			added, err := s.AddMember(ctx, group.ID, user)
			if err != nil {
				t.Errorf("parallel add failed: %v", err)
				return
			}

			if added {
				mu.Lock()
				inserts++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if inserts != 1 {
		t.Fatalf("expected exactly one insert, got %d", inserts)
	}

	members, _ := s.GetGroupMembers(ctx, group.ID)
	if len(members) != 1 {
		t.Fatalf("expected one member row, got %d", len(members))
	}
}

func TestRemoveMemberReportsMembership(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	group := mustGroup(t, s, "devs", 1)
	user := mustUser(t, s, 10, "alice")

	if removed, err := s.RemoveMember(ctx, group.ID, user.ID); err != nil || removed {
		t.Fatalf("removing a non-member: removed=%v err=%v", removed, err)
	}

	_, _ = s.AddMember(ctx, group.ID, user)

	if removed, err := s.RemoveMember(ctx, group.ID, user.ID); err != nil || !removed {
		t.Fatalf("removing a member: removed=%v err=%v", removed, err)
	}
}

func TestDeleteGroupCascadesToMembers(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	group := mustGroup(t, s, "devs", 1)

	_, _ = s.AddMember(ctx, group.ID, mustUser(t, s, 10, "alice"))

	if err := s.DeleteGroup(ctx, group.ID); err != nil {
		t.Fatalf("delete group: %v", err)
	}

	var count int64
	if err := s.db.Model(&GroupMember{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("member rows must be cascaded away: count=%d err=%v", count, err)
	}
}

func TestMemberRequiresExistingUser(t *testing.T) {
	s := newTestStorage(t)
	group := mustGroup(t, s, "devs", 1)

	if _, err := s.AddMember(context.Background(), group.ID, &User{ID: 999}); err == nil {
		t.Fatal("foreign keys must reject a member without a user row")
	}
}

func TestDeleteEmptyGroupsKeepsAllAndNonEmpty(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	mustGroup(t, s, "all", 1)
	mustGroup(t, s, "empty-b", 1)
	mustGroup(t, s, "empty-a", 1)
	mustGroup(t, s, "empty-other-chat", 2)
	busy := mustGroup(t, s, "busy", 1)

	_, _ = s.AddMember(ctx, busy.ID, mustUser(t, s, 10, "alice"))

	removed, err := s.DeleteEmptyGroups(ctx, 1, "all")
	if err != nil {
		t.Fatal(err)
	}

	if len(removed) != 2 || removed[0] != "empty-a" || removed[1] != "empty-b" {
		t.Fatalf("unexpected removed groups: %v", removed)
	}

	groups, _ := s.GetGroupsByChat(ctx, 1)
	if len(groups) != 2 {
		t.Fatalf("expected all+busy to remain, got %+v", groups)
	}

	if _, err := s.GetGroup(ctx, "empty-other-chat", 2); err != nil {
		t.Fatalf("other chats must not be touched: %v", err)
	}
}

func TestReopenKeepsSchemaVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reopen.sqlite")

	for range 2 {
		s, err := New(path)
		if err != nil {
			t.Fatal(err)
		}

		_ = s.Close()
	}
}

// legacySchema mirrors a database produced by older releases: GORM-created tables
// with leftover timestamp columns and per-member user data, no user_version.
const legacySchema = `
CREATE TABLE users (id integer, username text, first_name text, last_name text, PRIMARY KEY (id));
CREATE TABLE mention_groups (id integer, created_at datetime, updated_at datetime, deleted_at datetime, name text, chat_id integer, PRIMARY KEY (id));
CREATE UNIQUE INDEX idx_chat_group ON mention_groups(name, chat_id);
CREATE TABLE group_members (id integer, created_at datetime, group_id integer, user_id integer, username text, first_name text, last_name text, PRIMARY KEY (id),
  CONSTRAINT fk_mention_groups_members FOREIGN KEY (group_id) REFERENCES mention_groups(id) ON DELETE CASCADE);
CREATE UNIQUE INDEX idx_group_user ON group_members(group_id, user_id);
INSERT INTO mention_groups (id, name, chat_id) VALUES (1, 'devs', 100);
INSERT INTO group_members (id, group_id, user_id, username, first_name, last_name) VALUES (1, 1, 7, 'bob', 'Bob', 'B');
INSERT INTO group_members (id, group_id, user_id, username, first_name, last_name) VALUES (2, 99, 8, 'orphan', 'O', 'O');
`

func TestLegacyDatabaseIsMigrated(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.sqlite")

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := raw.Exec(legacySchema); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}

	_ = raw.Close()

	s, err := New(path)
	if err != nil {
		t.Fatalf("open legacy database: %v", err)
	}

	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()

	group, err := s.GetGroup(ctx, "devs", 100)
	if err != nil {
		t.Fatalf("group lost during migration: %v", err)
	}

	members, err := s.GetGroupMembers(ctx, group.ID)
	if err != nil || len(members) != 1 {
		t.Fatalf("members after migration: %+v err=%v", members, err)
	}

	if members[0].User.Username != "bob" || members[0].User.FirstName != "Bob" {
		t.Fatalf("legacy member data not moved to users: %+v", members[0].User)
	}

	var orphans int64
	s.db.Model(&GroupMember{}).Where("group_id = 99").Count(&orphans)

	if orphans != 0 {
		t.Fatal("orphan member rows should be cleaned up")
	}

	// Adding to the migrated schema must work with foreign keys on.
	if _, err := s.AddMember(ctx, group.ID, mustUser(t, s, 20, "carol")); err != nil {
		t.Fatalf("add after migration: %v", err)
	}
}
