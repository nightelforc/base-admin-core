// Package password 用 bcrypt 哈希并校验密码，并强制执行应用的密码策略。
package password

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/nightelforc/base-admin-core/backend/pkg/apperr"
)

// maxBytes 是 bcrypt 施加的硬限制（72 字节）；更长的输入会被算法静默截断，
// 因此在这里显式拒绝。
const maxBytes = 72

// Hasher 以可配置的 bcrypt cost 哈希并校验密码，并强制执行配置的最小长度。
type Hasher struct {
	cost      int
	minLength int
}

// Options 配置 Hasher。
type Options struct {
	// Cost 是 bcrypt 工作因子；超出范围的值回落到 bcrypt 默认值，使错误的环境变量安全降级。
	Cost int
	// MinLength 是可接受的最短密码；低于 8 的值会被抬高。
	MinLength int
}

// NewHasher 根据配置构建哈希器。
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

// Hash 返回 plain 的 bcrypt 摘要。
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

// Verify 报告 plain 是否与已存储的摘要匹配。
func (h *Hasher) Verify(digest, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(digest), []byte(plain)) == nil
}

// Validate 执行密码策略：长度有界，且至少包含一个字母和一个数字。
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

// Random 生成满足默认策略的密码：始终包含大写、小写、一个数字和一个符号。
// 符号集避开了在 shell 与 .env 文件中需要转义的字符。
func Random(length int) (string, error) {
	if length < 8 {
		length = 8
	}

	alphabet := upperLetters + lowerLetters + digits + symbols
	generated := make([]byte, 0, length)

	// 先保证四类字符各有一个，再随机填充并打乱，使强制出现的字符不落在固定位置。
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
