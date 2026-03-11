package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/argon2"
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

func (s *Service) Register(ctx context.Context, username string, password []byte, identityKey []byte) (string, error) {
	userID, err := generateID()
	if err != nil {
		return "", fmt.Errorf("generate user id: %w", err)
	}

	hash := hashPassword(password)

	_, err = s.pool.Exec(ctx,
		"INSERT INTO users (id, username, password, identity_key) VALUES ($1, $2, $3, $4)",
		userID, username, hash, identityKey,
	)
	if err != nil {
		return "", fmt.Errorf("insert user: %w", err)
	}

	return userID, nil
}

func (s *Service) Login(ctx context.Context, username string, password []byte) (string, error) {
	var userID string
	var storedHash []byte

	err := s.pool.QueryRow(ctx,
		"SELECT id, password FROM users WHERE username = $1", username,
	).Scan(&userID, &storedHash)
	if err != nil {
		return "", fmt.Errorf("user not found")
	}

	if !verifyPassword(password, storedHash) {
		return "", fmt.Errorf("invalid password")
	}

	token, err := generateToken()
	if err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}

	_, err = s.pool.Exec(ctx,
		"INSERT INTO sessions (token, user_id) VALUES ($1, $2)",
		token, userID,
	)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}

	return token, nil
}

func (s *Service) Authenticate(ctx context.Context, token string) (string, error) {
	var userID string
	err := s.pool.QueryRow(ctx,
		"SELECT user_id FROM sessions WHERE token = $1", token,
	).Scan(&userID)
	if err != nil {
		return "", fmt.Errorf("invalid session")
	}
	return userID, nil
}

func (s *Service) Logout(ctx context.Context, token string) error {
	_, err := s.pool.Exec(ctx, "DELETE FROM sessions WHERE token = $1", token)
	return err
}

const (
	argonTime    = 1
	argonMemory  = 64 * 1024
	argonThreads = 4
	argonKeyLen  = 32
	saltLen      = 16
)

func hashPassword(password []byte) []byte {
	salt := make([]byte, saltLen)
	rand.Read(salt)
	hash := argon2.IDKey(password, salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	// salt + hash
	return append(salt, hash...)
}

func verifyPassword(password, storedHash []byte) bool {
	if len(storedHash) < saltLen {
		return false
	}
	salt := storedHash[:saltLen]
	expected := storedHash[saltLen:]
	hash := argon2.IDKey(password, salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	if len(hash) != len(expected) {
		return false
	}
	var diff byte
	for i := range hash {
		diff |= hash[i] ^ expected[i]
	}
	return diff == 0
}

func generateID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
