package relay

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"nhooyr.io/websocket"
)

type Service struct {
	pool    *pgxpool.Pool
	mu      sync.RWMutex
	clients map[string]*websocket.Conn // userID -> connection
}

func NewService(pool *pgxpool.Pool) *Service {
	return &Service{
		pool:    pool,
		clients: make(map[string]*websocket.Conn),
	}
}

func (s *Service) Register(userID string, conn *websocket.Conn) {
	s.mu.Lock()
	s.clients[userID] = conn
	s.mu.Unlock()
}

func (s *Service) Unregister(userID string) {
	s.mu.Lock()
	delete(s.clients, userID)
	s.mu.Unlock()
}

func (s *Service) getConn(userID string) *websocket.Conn {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.clients[userID]
}

// Enqueue stores a message for offline delivery.
func (s *Service) Enqueue(ctx context.Context, recipientID string, envelope []byte) error {
	_, err := s.pool.Exec(ctx,
		"INSERT INTO message_queue (recipient_id, envelope) VALUES ($1, $2)",
		recipientID, envelope,
	)
	if err != nil {
		return fmt.Errorf("enqueue message: %w", err)
	}
	return nil
}

// Deliver tries to send the envelope directly. If recipient is offline, enqueues it.
func (s *Service) Deliver(ctx context.Context, recipientID string, envelope []byte) error {
	conn := s.getConn(recipientID)
	if conn == nil {
		return s.Enqueue(ctx, recipientID, envelope)
	}

	err := conn.Write(ctx, websocket.MessageBinary, envelope)
	if err != nil {
		// Connection broken, enqueue instead
		s.Unregister(recipientID)
		return s.Enqueue(ctx, recipientID, envelope)
	}

	return nil
}

// FlushQueue sends all queued messages to a newly connected user.
func (s *Service) FlushQueue(ctx context.Context, userID string, conn *websocket.Conn) error {
	rows, err := s.pool.Query(ctx,
		"SELECT id, envelope FROM message_queue WHERE recipient_id = $1 ORDER BY id", userID,
	)
	if err != nil {
		return fmt.Errorf("query queue: %w", err)
	}
	defer rows.Close()

	var delivered []int
	for rows.Next() {
		var id int
		var envelope []byte
		if err := rows.Scan(&id, &envelope); err != nil {
			return fmt.Errorf("scan message: %w", err)
		}

		if err := conn.Write(ctx, websocket.MessageBinary, envelope); err != nil {
			return fmt.Errorf("write message: %w", err)
		}
		delivered = append(delivered, id)
	}

	if len(delivered) > 0 {
		_, err := s.pool.Exec(ctx,
			"DELETE FROM message_queue WHERE id = ANY($1)", delivered,
		)
		if err != nil {
			return fmt.Errorf("delete delivered: %w", err)
		}
	}

	return nil
}
