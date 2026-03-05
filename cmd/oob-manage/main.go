package main

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/gbm-dev/oob-console-hub/internal/auth"
)

const userDataDir = "/data/users"

func usage() {
	fmt.Fprintf(os.Stderr, `Usage: oob-manage <command> [args]

Commands:
  add <username>      Create a new user with a temporary password
  remove <username>   Remove a user
  list                List all users
  lock <username>     Lock a user account
  unlock <username>   Unlock a user account
  reset <username>    Reset a user's password
`)
	os.Exit(1)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	store, err := auth.NewFileStore(userDataDir)
	if err != nil {
		fatalf("initializing user store: %v", err)
	}

	cmd := os.Args[1]
	switch cmd {
	case "add":
		requireArg(2, "username")
		cmdAdd(store, os.Args[2])
	case "remove":
		requireArg(2, "username")
		cmdRemove(store, os.Args[2])
	case "list":
		cmdList(store)
	case "lock":
		requireArg(2, "username")
		cmdLock(store, os.Args[2])
	case "unlock":
		requireArg(2, "username")
		cmdUnlock(store, os.Args[2])
	case "reset":
		requireArg(2, "username")
		cmdReset(store, os.Args[2])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		usage()
	}
}

func cmdAdd(store *auth.FileStore, username string) {
	if !validUsername(username) {
		fatalf("invalid username %q: must be 2-32 lowercase alphanumeric chars, dots, or hyphens", username)
	}
	tempPwd, err := auth.GenerateTemporaryPassword()
	if err != nil {
		fatalf("generating password: %v", err)
	}
	if err := store.Add(username, tempPwd); err != nil {
		fatalf("adding user: %v", err)
	}
	fmt.Printf("User %q created.\n", username)
	fmt.Printf("Temporary password: %s\n", tempPwd)
	fmt.Println("User must change password on first login.")
}

func cmdRemove(store *auth.FileStore, username string) {
	if err := store.Remove(username); err != nil {
		fatalf("removing user: %v", err)
	}
	fmt.Printf("User %q removed.\n", username)
}

func cmdList(store *auth.FileStore) {
	users, err := store.List()
	if err != nil {
		fatalf("listing users: %v", err)
	}
	if len(users) == 0 {
		fmt.Println("No users.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "USERNAME\tSTATUS\tLAST LOGIN\tPASSWORD CHANGE")
	for _, u := range users {
		status := "active"
		if u.Locked {
			status = "locked"
		}
		lastLogin := "never"
		if !u.LastLogin.IsZero() {
			lastLogin = u.LastLogin.Format(time.RFC3339)
		}
		pwChange := ""
		if u.ForceChange {
			pwChange = "required"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", u.Username, status, lastLogin, pwChange)
	}
	w.Flush()
}

func cmdLock(store *auth.FileStore, username string) {
	if err := store.Lock(username); err != nil {
		fatalf("locking user: %v", err)
	}
	fmt.Printf("User %q locked.\n", username)
}

func cmdUnlock(store *auth.FileStore, username string) {
	if err := store.Unlock(username); err != nil {
		fatalf("unlocking user: %v", err)
	}
	fmt.Printf("User %q unlocked.\n", username)
}

func cmdReset(store *auth.FileStore, username string) {
	tempPwd, err := auth.GenerateTemporaryPassword()
	if err != nil {
		fatalf("generating password: %v", err)
	}
	if err := store.Reset(username, tempPwd); err != nil {
		fatalf("resetting user: %v", err)
	}
	fmt.Printf("Password reset for %q.\n", username)
	fmt.Printf("Temporary password: %s\n", tempPwd)
	fmt.Println("User must change password on next login.")
}

func validUsername(s string) bool {
	if len(s) < 2 || len(s) > 32 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '.' || c == '-') {
			return false
		}
	}
	return true
}

func requireArg(idx int, name string) {
	if len(os.Args) <= idx {
		fatalf("missing required argument: %s", name)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(1)
}
