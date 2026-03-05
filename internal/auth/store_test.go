package auth

import (
	"os"
	"path/filepath"
	"testing"
)

func newTestStore(t *testing.T) *FileStore {
	t.Helper()
	dir := t.TempDir()
	s, err := NewFileStore(dir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return s
}

func TestAddAndAuthenticate(t *testing.T) {
	s := newTestStore(t)

	if err := s.Add("alice", "secret123"); err != nil {
		t.Fatalf("Add: %v", err)
	}

	ok, err := s.Authenticate("alice", "secret123")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !ok {
		t.Error("expected authentication to succeed")
	}

	ok, err = s.Authenticate("alice", "wrong")
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if ok {
		t.Error("expected authentication to fail with wrong password")
	}
}

func TestAddDuplicate(t *testing.T) {
	s := newTestStore(t)
	s.Add("alice", "pw")
	if err := s.Add("alice", "pw2"); err == nil {
		t.Error("expected error adding duplicate user")
	}
}

func TestRemove(t *testing.T) {
	s := newTestStore(t)
	s.Add("alice", "pw")
	if err := s.Remove("alice"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	ok, _ := s.Authenticate("alice", "pw")
	if ok {
		t.Error("expected removed user to fail auth")
	}
}

func TestRemoveNotFound(t *testing.T) {
	s := newTestStore(t)
	if err := s.Remove("ghost"); err == nil {
		t.Error("expected error removing nonexistent user")
	}
}

func TestList(t *testing.T) {
	s := newTestStore(t)
	s.Add("alice", "pw")
	s.Add("bob", "pw")

	users, err := s.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(users))
	}
	if users[0].Username != "alice" || users[1].Username != "bob" {
		t.Errorf("unexpected users: %+v", users)
	}
}

func TestLockUnlock(t *testing.T) {
	s := newTestStore(t)
	s.Add("alice", "pw")

	s.Lock("alice")
	ok, _ := s.Authenticate("alice", "pw")
	if ok {
		t.Error("expected locked user to fail auth")
	}

	s.Unlock("alice")
	ok, _ = s.Authenticate("alice", "pw")
	if !ok {
		t.Error("expected unlocked user to succeed auth")
	}
}

func TestResetPassword(t *testing.T) {
	s := newTestStore(t)
	s.Add("alice", "oldpw")

	s.Reset("alice", "newpw")

	ok, _ := s.Authenticate("alice", "newpw")
	if !ok {
		t.Error("expected new password to work after reset")
	}

	must, _ := s.MustChangePassword("alice")
	if !must {
		t.Error("expected force change after reset")
	}
}

func TestSetPassword(t *testing.T) {
	s := newTestStore(t)
	s.Add("alice", "temp")

	s.SetPassword("alice", "permanent")

	ok, _ := s.Authenticate("alice", "permanent")
	if !ok {
		t.Error("expected new password to work")
	}

	must, _ := s.MustChangePassword("alice")
	if must {
		t.Error("expected force change to be cleared after SetPassword")
	}
}

func TestForceChangeOnAdd(t *testing.T) {
	s := newTestStore(t)
	s.Add("alice", "temp")

	must, _ := s.MustChangePassword("alice")
	if !must {
		t.Error("expected force change on newly added user")
	}
}

func TestUpdateLastLogin(t *testing.T) {
	s := newTestStore(t)
	s.Add("alice", "pw")

	s.UpdateLastLogin("alice")

	users, _ := s.List()
	if users[0].LastLogin.IsZero() {
		t.Error("expected last login to be set")
	}
}

func TestPasswordHashing(t *testing.T) {
	hash, err := HashPassword("test123")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	ok, err := CheckPassword("test123", hash)
	if err != nil {
		t.Fatalf("CheckPassword: %v", err)
	}
	if !ok {
		t.Error("expected password check to pass")
	}
	ok, _ = CheckPassword("wrong", hash)
	if ok {
		t.Error("expected password check to fail")
	}
}

func TestGenerateTemporaryPassword(t *testing.T) {
	pw, err := GenerateTemporaryPassword()
	if err != nil {
		t.Fatalf("GenerateTemporaryPassword: %v", err)
	}
	if len(pw) != 12 {
		t.Errorf("expected 12 chars, got %d", len(pw))
	}
	// Check uniqueness (probabilistic)
	pw2, _ := GenerateTemporaryPassword()
	if pw == pw2 {
		t.Error("expected different passwords on successive calls")
	}
}

func TestMigrateFromLegacy(t *testing.T) {
	legacyDir := t.TempDir()
	storeDir := t.TempDir()

	// Create legacy files
	os.WriteFile(filepath.Join(legacyDir, "users.conf"), []byte("alice:\nbob:\n"), 0600)
	os.WriteFile(filepath.Join(legacyDir, "shadow.conf"), []byte("alice:$6$hash1:\nbob:$6$hash2:\n"), 0600)
	os.WriteFile(filepath.Join(legacyDir, "force_change.list"), []byte("alice\n"), 0600)

	store, err := NewFileStore(storeDir)
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}

	if err := MigrateFromLegacy(legacyDir, store); err != nil {
		t.Fatalf("MigrateFromLegacy: %v", err)
	}

	users, _ := store.List()
	if len(users) != 2 {
		t.Fatalf("expected 2 migrated users, got %d", len(users))
	}

	// All migrated users should need password change
	for _, u := range users {
		if !u.ForceChange {
			t.Errorf("expected %q to have force_change set", u.Username)
		}
	}

	// Legacy files should be renamed
	if _, err := os.Stat(filepath.Join(legacyDir, "users.conf")); !os.IsNotExist(err) {
		t.Error("expected users.conf to be renamed")
	}
	if _, err := os.Stat(filepath.Join(legacyDir, "users.conf.migrated")); err != nil {
		t.Error("expected users.conf.migrated to exist")
	}
}

func TestMigrateNoLegacyFiles(t *testing.T) {
	store, _ := NewFileStore(t.TempDir())
	// Should be a no-op when no legacy files exist
	if err := MigrateFromLegacy(t.TempDir(), store); err != nil {
		t.Fatalf("expected no error: %v", err)
	}
}
