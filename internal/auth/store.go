package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// UserStore defines the interface for user management operations.
type UserStore interface {
	Authenticate(username, password string) (bool, error)
	Add(username, tempPassword string) error
	Remove(username string) error
	List() ([]UserInfo, error)
	Lock(username string) error
	Unlock(username string) error
	Reset(username, newPassword string) error
	SetPassword(username, newPassword string) error
	MustChangePassword(username string) (bool, error)
	UpdateLastLogin(username string) error
}

// UserInfo is the public view of a user for listing.
type UserInfo struct {
	Username    string    `json:"username"`
	Locked      bool      `json:"locked"`
	LastLogin   time.Time `json:"last_login,omitempty"`
	ForceChange bool      `json:"force_change"`
}

// user is the internal representation stored in users.json.
type user struct {
	Username     string    `json:"username"`
	PasswordHash string    `json:"password_hash"`
	Locked       bool      `json:"locked"`
	ForceChange  bool      `json:"force_change"`
	LastLogin    time.Time `json:"last_login,omitempty"`
}

// usersFile is the top-level structure in users.json.
type usersFile struct {
	Users []user `json:"users"`
}

// FileStore is a file-backed UserStore using users.json.
type FileStore struct {
	path string
	mu   sync.RWMutex
}

// NewFileStore creates a FileStore backed by the given directory.
// It creates the directory and an empty users.json if they don't exist.
func NewFileStore(dir string) (*FileStore, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, fmt.Errorf("creating user data dir: %w", err)
	}
	s := &FileStore{path: filepath.Join(dir, "users.json")}
	// Create file if it doesn't exist
	if _, err := os.Stat(s.path); os.IsNotExist(err) {
		if err := s.write(&usersFile{}); err != nil {
			return nil, fmt.Errorf("creating initial users.json: %w", err)
		}
	}
	return s, nil
}

func (s *FileStore) Authenticate(username, password string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := s.readLocked()
	if err != nil {
		return false, err
	}
	u := findUser(data, username)
	if u == nil || u.Locked {
		return false, nil
	}
	return CheckPassword(password, u.PasswordHash)
}

func (s *FileStore) Add(username, tempPassword string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.withFileLock(func() error {
		data, err := s.readLocked()
		if err != nil {
			return err
		}
		if findUser(data, username) != nil {
			return fmt.Errorf("user %q already exists", username)
		}
		hash, err := HashPassword(tempPassword)
		if err != nil {
			return err
		}
		data.Users = append(data.Users, user{
			Username:     username,
			PasswordHash: hash,
			ForceChange:  true,
		})
		return s.write(data)
	})
}

func (s *FileStore) Remove(username string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.withFileLock(func() error {
		data, err := s.readLocked()
		if err != nil {
			return err
		}
		idx := findUserIdx(data, username)
		if idx < 0 {
			return fmt.Errorf("user %q not found", username)
		}
		data.Users = append(data.Users[:idx], data.Users[idx+1:]...)
		return s.write(data)
	})
}

func (s *FileStore) List() ([]UserInfo, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := s.readLocked()
	if err != nil {
		return nil, err
	}
	infos := make([]UserInfo, len(data.Users))
	for i, u := range data.Users {
		infos[i] = UserInfo{
			Username:    u.Username,
			Locked:      u.Locked,
			LastLogin:   u.LastLogin,
			ForceChange: u.ForceChange,
		}
	}
	return infos, nil
}

func (s *FileStore) Lock(username string) error {
	return s.modifyUser(username, func(u *user) { u.Locked = true })
}

func (s *FileStore) Unlock(username string) error {
	return s.modifyUser(username, func(u *user) { u.Locked = false })
}

func (s *FileStore) Reset(username, newPassword string) error {
	return s.modifyUser(username, func(u *user) {
		hash, err := HashPassword(newPassword)
		if err != nil {
			return
		}
		u.PasswordHash = hash
		u.ForceChange = true
	})
}

func (s *FileStore) SetPassword(username, newPassword string) error {
	return s.modifyUser(username, func(u *user) {
		hash, err := HashPassword(newPassword)
		if err != nil {
			return
		}
		u.PasswordHash = hash
		u.ForceChange = false
	})
}

func (s *FileStore) MustChangePassword(username string) (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := s.readLocked()
	if err != nil {
		return false, err
	}
	u := findUser(data, username)
	if u == nil {
		return false, fmt.Errorf("user %q not found", username)
	}
	return u.ForceChange, nil
}

func (s *FileStore) UpdateLastLogin(username string) error {
	return s.modifyUser(username, func(u *user) {
		u.LastLogin = time.Now().UTC()
	})
}

// modifyUser applies fn to the named user under write lock + file lock.
func (s *FileStore) modifyUser(username string, fn func(*user)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.withFileLock(func() error {
		data, err := s.readLocked()
		if err != nil {
			return err
		}
		idx := findUserIdx(data, username)
		if idx < 0 {
			return fmt.Errorf("user %q not found", username)
		}
		fn(&data.Users[idx])
		return s.write(data)
	})
}

// readLocked reads the users file. Caller must hold at least s.mu.RLock.
func (s *FileStore) readLocked() (*usersFile, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		return nil, fmt.Errorf("reading users file: %w", err)
	}
	var data usersFile
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("parsing users file: %w", err)
	}
	return &data, nil
}

// write atomically writes the users file via temp+rename.
func (s *FileStore) write(data *usersFile) error {
	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling users: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}

// withFileLock acquires an exclusive file lock for cross-process safety.
func (s *FileStore) withFileLock(fn func() error) error {
	lockPath := s.path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("opening lock file: %w", err)
	}
	defer f.Close()

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("acquiring file lock: %w", err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	return fn()
}

func findUser(data *usersFile, username string) *user {
	idx := findUserIdx(data, username)
	if idx < 0 {
		return nil
	}
	return &data.Users[idx]
}

func findUserIdx(data *usersFile, username string) int {
	for i, u := range data.Users {
		if u.Username == username {
			return i
		}
	}
	return -1
}
