// Package password hashes and verifies passwords with bcrypt and enforces the
// application's password policy.
package password

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/base-admin/base-admin-core/backend/pkg/apperr"
)

// maxBytes is the hard limit imposed by bcrypt (72 bytes); longer input is
// silently truncated by the algorithm, so it is rejected explicitly.
const maxBytes = 72

// Hasher hashes and verifies passwords with a configurable bcrypt cost and
// enforces the configured minimum length.
type Hasher struct {
	cost      int
	minLength int
}

// Options configures a Hasher.
type Options struct {
	// Cost is the bcrypt work factor; out-of-range values fall back to the
	// bcrypt default so a bad environment variable degrades safely.
	Cost int
	// MinLength is the shortest accepted password; values below 8 are raised.
	MinLength int
}

// NewHasher builds a hasher from configuration.
func NewHasher(opts Options) *Hasher {
	cost := opts.Cost
	if cost < bcrypt.MinCost || cost > bcrypt.MaxCost {
		cost = bcrypt.DefaultCost
	}
	minLength := opts.MinLength
	if minLength < 8 {
		minLength = 8
	}
	return &Hasher{cost: cost, minLength: minLength}
}

// Hash returns the bcrypt digest of plain.
func (h *Hasher) Hash(plain string) (string, error) {
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
func (h *Hasher) Verify(digest, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(digest), []byte(plain)) == nil
}

// Validate enforces the password policy: bounded length and at least one letter
// and one digit.
func (h *Hasher) Validate(plain string) error {
	if utf8.RuneCountInString(plain) < h.minLength {
		return apperr.ErrPasswordTooWeak.WithMessage("密码长度不能少于 %d 位", h.minLength)
	}
	if len(plain) > maxBytes {
		return apperr.ErrPasswordTooWeak.WithMessage("密码长度不能超过 %d 个字节", maxBytes)
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

const (
	upperLetters = "ABCDEFGHJKLMNPQRSTUVWXYZ"
	lowerLetters = "abcdefghijkmnopqrstuvwxyz"
	digits       = "23456789"
	symbols      = "!@#$%^&*"
)

// Random generates a password that satisfies the default policy: it always
// contains upper case, lower case, a digit and a symbol. The symbol set avoids
// characters that need escaping in shells and .env files.
func Random(length int) (string, error) {
	if length < 8 {
		length = 8
	}

	alphabet := upperLetters + lowerLetters + digits + symbols
	generated := make([]byte, 0, length)

	// Guarantee the four character classes first, then fill at random and
	// shuffle so the mandated characters are not in fixed positions.
	for _, class := range []string{upperLetters, lowerLetters, digits, symbols} {
		index, err := randomIndex(len(class))
		if err != nil {
			return "", err
		}
		generated = append(generated, class[index])
	}
	for len(generated) < length {
		index, err := randomIndex(len(alphabet))
		if err != nil {
			return "", err
		}
		generated = append(generated, alphabet[index])
	}

	for i := len(generated) - 1; i > 0; i-- {
		index, err := randomIndex(i + 1)
		if err != nil {
			return "", err
		}
		generated[i], generated[index] = generated[index], generated[i]
	}
	return string(generated), nil
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
