package config

import "os"

// openFile opens a file for reading. Extracted for testability.
func openFile(path string) (*os.File, error) {
	return os.Open(path)
}
