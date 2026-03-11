package contacts

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Contact struct {
	UserID   string
	Username string
}

type Service struct {
	pool *pgxpool.Pool
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool}
}

// AddContact adds a contact by username. Returns the contact's user_id and username.
func (s *Service) AddContact(ctx context.Context, ownerID, contactUsername string) (string, string, error) {
	var contactID string
	err := s.pool.QueryRow(ctx, "SELECT id FROM users WHERE username = $1", contactUsername).Scan(&contactID)
	if err != nil {
		return "", "", fmt.Errorf("user not found")
	}

	if contactID == ownerID {
		return "", "", fmt.Errorf("cannot add yourself")
	}

	_, err = s.pool.Exec(ctx,
		"INSERT INTO contacts (owner_id, contact_id) VALUES ($1, $2) ON CONFLICT DO NOTHING",
		ownerID, contactID,
	)
	if err != nil {
		return "", "", fmt.Errorf("add contact: %w", err)
	}

	return contactID, contactUsername, nil
}

// RemoveContact removes a contact.
func (s *Service) RemoveContact(ctx context.Context, ownerID, contactID string) error {
	_, err := s.pool.Exec(ctx,
		"DELETE FROM contacts WHERE owner_id = $1 AND contact_id = $2",
		ownerID, contactID,
	)
	return err
}

// ListContacts returns all contacts for a user.
func (s *Service) ListContacts(ctx context.Context, ownerID string) ([]Contact, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT u.id, u.username FROM contacts c
		 JOIN users u ON u.id = c.contact_id
		 WHERE c.owner_id = $1
		 ORDER BY u.username`,
		ownerID,
	)
	if err != nil {
		return nil, fmt.Errorf("list contacts: %w", err)
	}
	defer rows.Close()

	var contacts []Contact
	for rows.Next() {
		var c Contact
		if err := rows.Scan(&c.UserID, &c.Username); err != nil {
			return nil, fmt.Errorf("scan contact: %w", err)
		}
		contacts = append(contacts, c)
	}
	return contacts, nil
}

// GetContactOwners returns all users who have the given userID in their contact list.
func (s *Service) GetContactOwners(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.pool.Query(ctx,
		"SELECT owner_id FROM contacts WHERE contact_id = $1",
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("get contact owners: %w", err)
	}
	defer rows.Close()

	var owners []string
	for rows.Next() {
		var ownerID string
		if err := rows.Scan(&ownerID); err != nil {
			return nil, fmt.Errorf("scan owner: %w", err)
		}
		owners = append(owners, ownerID)
	}
	return owners, nil
}

// TotalUsers returns total registered user count.
func (s *Service) TotalUsers(ctx context.Context) (uint32, error) {
	var count uint32
	err := s.pool.QueryRow(ctx, "SELECT COUNT(*) FROM users").Scan(&count)
	return count, err
}
