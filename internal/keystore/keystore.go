package keystore

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

type PreKeyBundle struct {
	IdentityKey          []byte
	SignedPreKey         []byte
	SignedPreKeySignature []byte
	OneTimePreKey        []byte // may be nil if exhausted
}

const maxOneTimeKeys = 100

func (s *Service) UploadPreKeys(ctx context.Context, userID string, signedPreKey, signature []byte, oneTimeKeys [][]byte) error {
	if len(oneTimeKeys) > maxOneTimeKeys {
		return fmt.Errorf("too many one-time keys (max %d)", maxOneTimeKeys)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Upsert signed pre-key
	_, err = tx.Exec(ctx, `
		INSERT INTO pre_keys (user_id, signed_pre_key, signed_pre_key_signature)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET
			signed_pre_key = EXCLUDED.signed_pre_key,
			signed_pre_key_signature = EXCLUDED.signed_pre_key_signature
	`, userID, signedPreKey, signature)
	if err != nil {
		return fmt.Errorf("upsert signed pre-key: %w", err)
	}

	// Insert one-time pre-keys
	for _, key := range oneTimeKeys {
		_, err = tx.Exec(ctx,
			"INSERT INTO one_time_pre_keys (user_id, key_data) VALUES ($1, $2)",
			userID, key,
		)
		if err != nil {
			return fmt.Errorf("insert one-time key: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (s *Service) FetchPreKey(ctx context.Context, userID string) (*PreKeyBundle, error) {
	bundle := &PreKeyBundle{}

	// Get identity key
	err := s.pool.QueryRow(ctx,
		"SELECT identity_key FROM users WHERE id = $1", userID,
	).Scan(&bundle.IdentityKey)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}

	// Get signed pre-key
	err = s.pool.QueryRow(ctx,
		"SELECT signed_pre_key, signed_pre_key_signature FROM pre_keys WHERE user_id = $1", userID,
	).Scan(&bundle.SignedPreKey, &bundle.SignedPreKeySignature)
	if err != nil {
		return nil, fmt.Errorf("pre-key not found")
	}

	// Try to consume one one-time pre-key (FOR UPDATE SKIP LOCKED prevents races)
	err = s.pool.QueryRow(ctx, `
		DELETE FROM one_time_pre_keys
		WHERE id = (SELECT id FROM one_time_pre_keys WHERE user_id = $1 ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED)
		RETURNING key_data
	`, userID).Scan(&bundle.OneTimePreKey)
	if err != nil {
		// No one-time keys left — that's ok
		bundle.OneTimePreKey = nil
	}

	return bundle, nil
}
