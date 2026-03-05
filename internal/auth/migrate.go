package auth

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MigrateFromLegacy reads legacy users.conf, shadow.conf, and force_change.list
// and creates corresponding entries in the FileStore. All migrated users are
// forced to change passwords since SHA-512 hashes cannot be converted to bcrypt.
func MigrateFromLegacy(legacyDir string, store *FileStore) error {
	usersPath := filepath.Join(legacyDir, "users.conf")
	if _, err := os.Stat(usersPath); os.IsNotExist(err) {
		return nil // nothing to migrate
	}

	usernames, err := readLegacyUsers(usersPath)
	if err != nil {
		return fmt.Errorf("reading legacy users: %w", err)
	}
	if len(usernames) == 0 {
		return nil
	}

	for _, username := range usernames {
		tempPwd, err := GenerateTemporaryPassword()
		if err != nil {
			return fmt.Errorf("generating temp password for %q: %w", username, err)
		}
		if err := store.Add(username, tempPwd); err != nil {
			// Skip users that already exist
			if strings.Contains(err.Error(), "already exists") {
				continue
			}
			return fmt.Errorf("adding migrated user %q: %w", username, err)
		}
		fmt.Printf("Migrated user %q (new temp password: %s)\n", username, tempPwd)
	}

	// Rename legacy files so migration doesn't run again
	for _, name := range []string{"users.conf", "shadow.conf", "force_change.list"} {
		old := filepath.Join(legacyDir, name)
		if _, err := os.Stat(old); err == nil {
			os.Rename(old, old+".migrated")
		}
	}

	return nil
}

func readLegacyUsers(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var users []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Format: username: or just username
		username := strings.TrimSuffix(line, ":")
		username = strings.TrimSpace(username)
		if username != "" {
			users = append(users, username)
		}
	}
	return users, scanner.Err()
}
