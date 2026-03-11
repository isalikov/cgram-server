package ws

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"

	"nhooyr.io/websocket"

	"github.com/isalikov/cgram-server/internal/auth"
	"github.com/isalikov/cgram-server/internal/contacts"
	"github.com/isalikov/cgram-server/internal/keystore"
	"github.com/isalikov/cgram-server/internal/relay"
)

type Handler struct {
	auth     *auth.Service
	keys     *keystore.Service
	relay    *relay.Service
	contacts *contacts.Service
	router   *Router
}

func NewHandler(auth *auth.Service, keys *keystore.Service, relay *relay.Service, contacts *contacts.Service) *Handler {
	h := &Handler{
		auth:     auth,
		keys:     keys,
		relay:    relay,
		contacts: contacts,
	}
	h.router = NewRouter(h)
	return h
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		log.Printf("websocket accept: %v", err)
		return
	}
	defer conn.CloseNow()

	// Limit incoming message size to 64 KB to prevent memory abuse
	conn.SetReadLimit(64 * 1024)

	ctx := r.Context()
	session := &Session{conn: conn}

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				break
			}
			if !isClientDisconnect(err) {
				log.Printf("read: %v", err)
			}
			break
		}

		if err := h.router.Handle(ctx, session, data); err != nil {
			log.Printf("handle: %v", err)
		}
	}

	if session.userID != "" {
		h.relay.Unregister(session.userID)
	}
}

type Session struct {
	conn   *websocket.Conn
	userID string
	token  string
}

func (s *Session) Send(ctx context.Context, data []byte) error {
	return s.conn.Write(ctx, websocket.MessageBinary, data)
}

// isClientDisconnect returns true for errors caused by the client
// closing the connection without a proper WebSocket close handshake.
func isClientDisconnect(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, context.Canceled) {
		return true
	}
	var netErr *net.OpError
	if errors.As(err, &netErr) {
		return true
	}
	return websocket.CloseStatus(err) != -1
}
