// Package security holds the cryptographic primitives of the application:
// password hashing and JWT issuing/verification.
package security

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/base-admin/base-admin-core/backend/pkg/apperr"
)

// maxPasswordBytes is the hard limit imposed by bcrypt (72 bytes); longer input
// is silently truncated by the algorithm, so we reject it explicitly.
const maxPasswordBytes = 72

// PasswordHasher hashes and verifies passwords with bcrypt using a configurable
// cost, and enforces the configured password policy.
type PasswordHasher struct {
	cost      int
	minLength int
}

// NewPasswordHasher builds a hasher. Values outside the sane range are clamped
// so a bad environment variable degrades safely instead of breaking logins.
func NewPasswordHasher(cost, minLength int) *PasswordHasher {
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		cost = bcrypt.DefaultCost
	}
	if minLength < 8 {
		minLength = 8
	}
	return &PasswordHasher{cost: cost, minLength: minLength}
}

// Hash returns the bcrypt digest of plain.
func (h *PasswordHasher) Hash(plain string) (string, error) {
	if err := h.Validate(plain); err != nil {
		return "", err
	}
	digest, err := bcrypt.GenerateFromPassword([]byte(plain), h.cost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(digest), nil
}

// Verify reports whether plain matches the stored digest.
func (h *PasswordHasher) Verify(digest, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(digest), []byte(plain)) == nil
}

// Validate enforces the password policy: bounded length and at least one letter
// and one digit.
func (h *PasswordHasher) Validate(plain string) error {
	if utf8.RuneCountInString(plain) < h.minLength {
		return apperr.ErrPasswordTooWeak.WithMessage("密码长度不能少于 %d 位", h.minLength)
	}
	if len(plain) > maxPasswordBytes {
		return apperr.ErrPasswordTooWeak.WithMessage("密码长度不能超过 %d 个字节", maxPasswordBytes)
	}

	var hasLetter, hasDigit bool
	for _, r := range plain {
		switch {
		case unicode.IsLetter(r):
			hasLetter = true
		case unicode.IsDigit(r):
			hasDigit = true
		}
	}
	if !hasLetter || !hasDigit {
		return apperr.ErrPasswordTooWeak.WithMessage("密码必须同时包含字母和数字")
	}
	return nil
}

// MinLength exposes the effective minimum password length (used by the frontend
// contract and by validation messages).
func (h *PasswordHasher) MinLength() int { return h.minLength }

// NewSessionID returns a random, URL-safe session identifier.
func NewSessionID() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// ErrEmptyPassword is returned when no password is supplied at all.
var ErrEmptyPassword = errors.New("password is empty")

const (
	upperLetters = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	lowerLetters = "abcdefghijkmnopqrstuvwxyz"
	digits       = "23456789"
	symbols      = "!@#$%^&*"
)

// RandomPassword generates a password that satisfies the default policy: it
// always contains upper case, lower case, a digit and a symbol. The symbol set
// avoids characters that need escaping in shells and .env files.
func RandomPassword(length int) (string, error) {
	if length < 8 {
		length = 8
	}

	alphabet := upperLetters + lowerLetters + digits + symbols
	password := make([]byte, 0, length)

	// Guarantee the four character classes first, then fill at random and
	// shuffle so the mandated characters are not in fixed positions.
	required := []string{upperLetters, lowerLetters, digits, symbols}
	for _, class := range required {
		index, err := randomIndex(len(class))
		if err != nil {
			return "", err
		}
		password = append(password, class[index])
	}
	for len(password) < length {
		index, err := randomIndex(len(alphabet))
		if err != nil {
			return "", err
		}
		password = append(password, alphabet[index])
	}

	for i := len(password) - 1; i > 0; i-- {
		index, err := randomIndex(i + 1)
		if err != nil {
			return "", err
		}
		password[i], password[index] = password[index], password[i]
	}
	return string(password), nil
}

func randomIndex(limit int) (int, error) {
	if limit <= 0 {
		return 0, errors.New("invalid alphabet size")
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(limit)))
	if err != nil {
		return 0, fmt.Errorf("generate random value: %w", err)
	}
	return int(value.Int64()), nil
}
