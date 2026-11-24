package storage

import (
	"context"
	"database/sql"
	"errors"
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

func memberIDs(t *testing.T, s *Storage, groupID uint) map[int64]bool {
	t.Helper()

	members, err := s.GetGroupMembers(context.Background(), groupID)
	if err != nil {
		t.Fatal(err)
	}

	ids := make(map[int64]bool)
	for _, m := range members {
		ids[m.UserID] = true
	}

	return ids
}

func TestMigrateChatGroupsMovesGroups(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	group := mustGroup(t, s, "devs", -1)
	_, _ = s.AddMember(ctx, group.ID, mustUser(t, s, 10, "alice"))

	if err := s.MigrateChatGroups(ctx, -1, -100); err != nil {
		t.Fatal(err)
	}

	moved, err := s.GetGroup(ctx, "devs", -100)
	if err != nil || moved.ID != group.ID {
		t.Fatalf("group should keep its ID under the new chat: %+v err=%v", moved, err)
	}

	if !memberIDs(t, s, moved.ID)[10] {
		t.Fatal("members must be preserved")
	}

	// Idempotent: the second service message is a no-op.
	if err := s.MigrateChatGroups(ctx, -1, -100); err != nil {
		t.Fatalf("repeat migration: %v", err)
	}

	if groups, _ := s.GetGroupsByChat(ctx, -100); len(groups) != 1 {
		t.Fatalf("expected one group after repeat, got %d", len(groups))
	}
}

func TestMigrateChatGroupsMergesNameConflicts(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	old := mustGroup(t, s, "devs", -1)
	existing := mustGroup(t, s, "devs", -100)
	mustGroup(t, s, "qa", -1)

	_, _ = s.AddMember(ctx, old.ID, mustUser(t, s, 10, "alice"))
	_, _ = s.AddMember(ctx, old.ID, mustUser(t, s, 11, "bob"))
	_, _ = s.AddMember(ctx, existing.ID, mustUser(t, s, 11, "bob"))
	_, _ = s.AddMember(ctx, existing.ID, mustUser(t, s, 12, "carol"))

	if err := s.MigrateChatGroups(ctx, -1, -100); err != nil {
		t.Fatalf("merge migration: %v", err)
	}

	merged, err := s.GetGroup(ctx, "devs", -100)
	if err != nil || merged.ID != existing.ID {
		t.Fatalf("the existing target group should survive: %+v err=%v", merged, err)
	}

	ids := memberIDs(t, s, merged.ID)
	if len(ids) != 3 || !ids[10] || !ids[11] || !ids[12] {
		t.Fatalf("members must be merged without duplicates: %v", ids)
	}

	if _, err := s.GetGroup(ctx, "qa", -100); err != nil {
		t.Fatalf("non-conflicting group must move: %v", err)
	}

	if groups, _ := s.GetGroupsByChat(ctx, -1); len(groups) != 0 {
		t.Fatalf("old chat must be empty, got %+v", groups)
	}
}

func TestNamesAreCaseInsensitive(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	if err := s.CreateGroup(ctx, "Devs", 1); err != nil {
		t.Fatal(err)
	}

	if err := s.CreateGroup(ctx, "DEVS", 1); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("duplicate in another case must be rejected, got %v", err)
	}

	group, err := s.GetGroup(ctx, "dEvS", 1)
	if err != nil || group.Name != "devs" {
		t.Fatalf("lookup must be case-insensitive and stored lowercased: %+v err=%v", group, err)
	}

	found, err := s.FindGroupsByChatAndNamesWithMembers(ctx, 1, []string{"DEVS"})
	if err != nil || len(found) != 1 {
		t.Fatalf("find must be case-insensitive: %+v err=%v", found, err)
	}
}

func TestRenameGroup(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	group := mustGroup(t, s, "devs", 1)
	mustGroup(t, s, "qa", 1)
	mustGroup(t, s, "devs", 2)
	_, _ = s.AddMember(ctx, group.ID, mustUser(t, s, 10, "alice"))

	if err := s.RenameGroup(ctx, 1, "devs", "QA"); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("rename onto an existing name: %v", err)
	}

	if err := s.RenameGroup(ctx, 1, "ghosts", "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("rename of an unknown group: %v", err)
	}

	if err := s.RenameGroup(ctx, 1, "Devs", "Engineers"); err != nil {
		t.Fatal(err)
	}

	renamed, err := s.GetGroup(ctx, "engineers", 1)
	if err != nil || renamed.ID != group.ID || !memberIDs(t, s, renamed.ID)[10] {
		t.Fatalf("rename must keep ID and members: %+v err=%v", renamed, err)
	}

	if _, err := s.GetGroup(ctx, "devs", 2); err != nil {
		t.Fatalf("other chats must be unaffected: %v", err)
	}
}

func TestAliases(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	group := mustGroup(t, s, "developers", 1)
	mustGroup(t, s, "qa", 1)
	_, _ = s.AddMember(ctx, group.ID, mustUser(t, s, 10, "alice"))

	if err := s.AddAlias(ctx, 1, "ghosts", "g"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("alias for unknown group: %v", err)
	}

	if err := s.AddAlias(ctx, 1, "developers", "QA"); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("alias equal to a group name: %v", err)
	}

	if err := s.AddAlias(ctx, 1, "developers", "Dev"); err != nil {
		t.Fatal(err)
	}

	if err := s.AddAlias(ctx, 1, "qa", "dev"); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("alias already taken: %v", err)
	}

	if err := s.CreateGroup(ctx, "dev", 1); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("group named like an alias: %v", err)
	}

	// the same alias can exist in another chat
	mustGroup(t, s, "developers", 2)

	if err := s.AddAlias(ctx, 2, "developers", "dev"); err != nil {
		t.Fatalf("aliases are unique per chat only: %v", err)
	}

	found, err := s.FindGroupsByChatAndNamesWithMembers(ctx, 1, []string{"DEV", "developers"})
	if err != nil || len(found) != 1 || found[0].ID != group.ID || len(found[0].Members) != 1 {
		t.Fatalf("alias and name must resolve to one group: %+v err=%v", found, err)
	}

	aliases, err := s.GetAliasesByChat(ctx, 1)
	if err != nil || len(aliases[group.ID]) != 1 || aliases[group.ID][0] != "dev" {
		t.Fatalf("unexpected aliases %v err=%v", aliases, err)
	}

	if removed, err := s.RemoveAlias(ctx, 1, "DEV"); err != nil || !removed {
		t.Fatalf("remove alias: %v %v", removed, err)
	}

	if removed, _ := s.RemoveAlias(ctx, 1, "dev"); removed {
		t.Fatal("second removal must report false")
	}
}

func TestAliasesFollowChatMigrationAndGroupDeletion(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	mustGroup(t, s, "developers", -1)
	other := mustGroup(t, s, "developers", -100)

	_ = s.AddAlias(ctx, -1, "developers", "dev")
	_ = s.AddAlias(ctx, -1, "developers", "d")
	_ = s.AddAlias(ctx, -100, "developers", "dev") // clashes after the merge

	if err := s.MigrateChatGroups(ctx, -1, -100); err != nil {
		t.Fatal(err)
	}

	aliases, _ := s.GetAliasesByChat(ctx, -100)
	if len(aliases[other.ID]) != 2 {
		t.Fatalf("merged group should own both aliases, got %v", aliases)
	}

	if old, _ := s.GetAliasesByChat(ctx, -1); len(old) != 0 {
		t.Fatalf("old chat must have no aliases: %v", old)
	}

	if err := s.DeleteGroup(ctx, other.ID); err != nil {
		t.Fatal(err)
	}

	if after, _ := s.GetAliasesByChat(ctx, -100); len(after) != 0 {
		t.Fatalf("aliases must be deleted with their group: %v", after)
	}
}

// v1Schema is the schema as it was after migration 1 (before case-insensitive names).
const v1Schema = `
CREATE TABLE users (id INTEGER PRIMARY KEY, username TEXT, first_name TEXT, last_name TEXT);
CREATE TABLE mention_groups (id INTEGER PRIMARY KEY, name TEXT, chat_id INTEGER);
CREATE TABLE group_members (id INTEGER PRIMARY KEY, group_id INTEGER, user_id INTEGER,
  CONSTRAINT fk_mention_groups_members FOREIGN KEY (group_id) REFERENCES mention_groups(id) ON DELETE CASCADE,
  CONSTRAINT fk_group_members_user FOREIGN KEY (user_id) REFERENCES users(id));
CREATE UNIQUE INDEX idx_chat_group ON mention_groups (name, chat_id);
CREATE UNIQUE INDEX idx_group_user ON group_members (group_id, user_id);
INSERT INTO users (id, username) VALUES (1, 'a'), (2, 'b'), (3, 'c');
INSERT INTO mention_groups (id, name, chat_id) VALUES (1, 'Devs', 5), (2, 'devs', 5), (3, 'QA', 5), (4, 'Devs', 6);
INSERT INTO group_members (group_id, user_id) VALUES (1, 1), (1, 2), (2, 2), (2, 3), (3, 1), (4, 3);
PRAGMA user_version = 1;
`

func TestCaseInsensitiveMigrationMergesCollisions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "v1.sqlite")

	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := raw.Exec(v1Schema); err != nil {
		t.Fatalf("seed v1 schema: %v", err)
	}

	_ = raw.Close()

	s, err := New(path)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}

	t.Cleanup(func() { _ = s.Close() })

	ctx := context.Background()

	groups, _ := s.GetGroupsByChat(ctx, 5)
	if len(groups) != 2 {
		t.Fatalf("Devs/devs must merge into one group, QA stays: %+v", groups)
	}

	devs, err := s.GetGroup(ctx, "devs", 5)
	if err != nil {
		t.Fatal(err)
	}

	if ids := memberIDs(t, s, devs.ID); len(ids) != 3 || !ids[1] || !ids[2] || !ids[3] {
		t.Fatalf("merged members must be the union without duplicates: %v", ids)
	}

	if _, err := s.GetGroup(ctx, "qa", 5); err != nil {
		t.Fatalf("QA must be lowercased: %v", err)
	}

	if _, err := s.GetGroup(ctx, "devs", 6); err != nil {
		t.Fatalf("other chats are migrated independently: %v", err)
	}
}
