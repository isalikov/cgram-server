package ws

import (
	"context"
	"log"
	"net/http"

	"nhooyr.io/websocket"

	"github.com/isalikov/cgram-server/internal/auth"
	"github.com/isalikov/cgram-server/internal/keystore"
	"github.com/isalikov/cgram-server/internal/relay"
)

type Handler struct {
	auth     *auth.Service
	keys     *keystore.Service
	relay    *relay.Service
	router   *Router
}

func NewHandler(auth *auth.Service, keys *keystore.Service, relay *relay.Service) *Handler {
	h := &Handler{
		auth:  auth,
		keys:  keys,
		relay: relay,
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

	ctx := r.Context()
	session := &Session{conn: conn}

	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure {
				break
			}
			log.Printf("read: %v", err)
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
