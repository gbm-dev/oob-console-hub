package auth

import (
	"crypto/rand"
	"math/big"

	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost    = 12
	tempPwdLength = 12
	tempPwdChars  = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// HashPassword returns a bcrypt hash of the password.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

// CheckPassword compares a plaintext password against a bcrypt hash.
func CheckPassword(password, hash string) (bool, error) {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err == bcrypt.ErrMismatchedHashAndPassword {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GenerateTemporaryPassword returns a random 12-char alphanumeric string.
func GenerateTemporaryPassword() (string, error) {
	result := make([]byte, tempPwdLength)
	for i := range result {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(tempPwdChars))))
		if err != nil {
			return "", err
		}
		result[i] = tempPwdChars[n.Int64()]
	}
	return string(result), nil
}
