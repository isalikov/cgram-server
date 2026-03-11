package ws

import (
	"context"
	"fmt"
	"log"

	pb "github.com/isalikov/cgram-proto/gen/proto"
	"google.golang.org/protobuf/proto"
)

type Router struct {
	handler *Handler
}

func NewRouter(h *Handler) *Router {
	return &Router{handler: h}
}

func (r *Router) Handle(ctx context.Context, session *Session, data []byte) error {
	frame := &pb.Frame{}
	if err := proto.Unmarshal(data, frame); err != nil {
		return r.sendError(ctx, session, "", 400, "invalid frame")
	}

	switch p := frame.Payload.(type) {
	case *pb.Frame_RegisterRequest:
		return r.handleRegister(ctx, session, frame.RequestId, p.RegisterRequest)
	case *pb.Frame_LoginRequest:
		return r.handleLogin(ctx, session, frame.RequestId, p.LoginRequest)
	case *pb.Frame_LogoutRequest:
		return r.handleLogout(ctx, session, frame.RequestId, p.LogoutRequest)
	case *pb.Frame_UploadPreKeysRequest:
		return r.handleUploadPreKeys(ctx, session, frame.RequestId, p.UploadPreKeysRequest)
	case *pb.Frame_FetchPreKeyRequest:
		return r.handleFetchPreKey(ctx, session, frame.RequestId, p.FetchPreKeyRequest)
	case *pb.Frame_Envelope:
		return r.handleEnvelope(ctx, session, frame.RequestId, p.Envelope)
	case *pb.Frame_ResolveUsernameRequest:
		return r.handleResolveUsername(ctx, session, frame.RequestId, p.ResolveUsernameRequest)
	case *pb.Frame_AddContactRequest:
		return r.handleAddContact(ctx, session, frame.RequestId, p.AddContactRequest)
	case *pb.Frame_RemoveContactRequest:
		return r.handleRemoveContact(ctx, session, frame.RequestId, p.RemoveContactRequest)
	case *pb.Frame_ListContactsRequest:
		return r.handleListContacts(ctx, session, frame.RequestId)
	case *pb.Frame_StatsRequest:
		return r.handleStats(ctx, session, frame.RequestId)
	default:
		return r.sendError(ctx, session, frame.RequestId, 400, "unknown payload type")
	}
}

func (r *Router) handleRegister(ctx context.Context, session *Session, reqID string, req *pb.RegisterRequest) error {
	userID, err := r.handler.auth.Register(ctx, req.Username, req.PasswordVerifier, req.PublicIdentityKey)
	if err != nil {
		return r.sendError(ctx, session, reqID, 409, "registration failed")
	}

	return r.sendFrame(ctx, session, &pb.Frame{
		RequestId: reqID,
		Payload:   &pb.Frame_RegisterResponse{RegisterResponse: &pb.RegisterResponse{UserId: userID}},
	})
}

func (r *Router) handleLogin(ctx context.Context, session *Session, reqID string, req *pb.LoginRequest) error {
	token, err := r.handler.auth.Login(ctx, req.Username, req.AuthMessage)
	if err != nil {
		return r.sendError(ctx, session, reqID, 401, "login failed")
	}

	userID, _ := r.handler.auth.Authenticate(ctx, token)
	session.userID = userID
	session.token = token

	// Register connection for message delivery
	r.handler.relay.Register(userID, session.conn)

	// Flush queued messages
	if err := r.handler.relay.FlushQueue(ctx, userID, session.conn); err != nil {
		log.Printf("flush queue: %v", err)
	}

	return r.sendFrame(ctx, session, &pb.Frame{
		RequestId: reqID,
		Payload:   &pb.Frame_LoginResponse{LoginResponse: &pb.LoginResponse{SessionToken: token}},
	})
}

func (r *Router) handleLogout(ctx context.Context, session *Session, reqID string, _ *pb.LogoutRequest) error {
	if session.userID == "" {
		return r.sendError(ctx, session, reqID, 401, "not authenticated")
	}

	r.handler.auth.Logout(ctx, session.token)
	r.handler.relay.Unregister(session.userID)
	session.userID = ""
	session.token = ""

	return r.sendFrame(ctx, session, &pb.Frame{
		RequestId: reqID,
		Payload:   &pb.Frame_LogoutResponse{LogoutResponse: &pb.LogoutResponse{}},
	})
}

func (r *Router) handleUploadPreKeys(ctx context.Context, session *Session, reqID string, req *pb.UploadPreKeysRequest) error {
	if session.userID == "" {
		return r.sendError(ctx, session, reqID, 401, "not authenticated")
	}

	err := r.handler.keys.UploadPreKeys(ctx, session.userID,
		req.Bundle.SignedPreKey,
		req.Bundle.SignedPreKeySignature,
		req.Bundle.OneTimePreKeys,
	)
	if err != nil {
		return r.sendError(ctx, session, reqID, 500, "failed to upload keys")
	}

	return r.sendFrame(ctx, session, &pb.Frame{
		RequestId: reqID,
		Payload:   &pb.Frame_UploadPreKeysResponse{UploadPreKeysResponse: &pb.UploadPreKeysResponse{}},
	})
}

func (r *Router) handleFetchPreKey(ctx context.Context, session *Session, reqID string, req *pb.FetchPreKeyRequest) error {
	if session.userID == "" {
		return r.sendError(ctx, session, reqID, 401, "not authenticated")
	}

	bundle, err := r.handler.keys.FetchPreKey(ctx, req.UserId)
	if err != nil {
		return r.sendError(ctx, session, reqID, 404, "user not found")
	}

	return r.sendFrame(ctx, session, &pb.Frame{
		RequestId: reqID,
		Payload: &pb.Frame_FetchPreKeyResponse{FetchPreKeyResponse: &pb.FetchPreKeyResponse{
			IdentityKey:          bundle.IdentityKey,
			SignedPreKey:         bundle.SignedPreKey,
			SignedPreKeySignature: bundle.SignedPreKeySignature,
			OneTimePreKey:        bundle.OneTimePreKey,
		}},
	})
}

func (r *Router) handleEnvelope(ctx context.Context, session *Session, reqID string, env *pb.Envelope) error {
	if session.userID == "" {
		return r.sendError(ctx, session, reqID, 401, "not authenticated")
	}

	if env.RecipientId == "" {
		return r.sendError(ctx, session, reqID, 400, "recipient_id is required")
	}

	// Serialize the envelope to deliver as-is
	envData, err := proto.Marshal(&pb.Frame{
		Payload: &pb.Frame_Envelope{Envelope: env},
	})
	if err != nil {
		return r.sendError(ctx, session, reqID, 500, "internal error")
	}

	if err := r.handler.relay.Deliver(ctx, env.RecipientId, envData); err != nil {
		return r.sendError(ctx, session, reqID, 500, "delivery failed")
	}

	return r.sendFrame(ctx, session, &pb.Frame{
		RequestId: reqID,
		Payload:   &pb.Frame_Ack{Ack: &pb.Ack{MessageId: reqID, Delivered: true}},
	})
}

func (r *Router) handleResolveUsername(ctx context.Context, session *Session, reqID string, req *pb.ResolveUsernameRequest) error {
	if session.userID == "" {
		return r.sendError(ctx, session, reqID, 401, "not authenticated")
	}

	userID, err := r.handler.auth.ResolveUsername(ctx, req.Username)
	if err != nil {
		return r.sendError(ctx, session, reqID, 404, "user not found")
	}

	return r.sendFrame(ctx, session, &pb.Frame{
		RequestId: reqID,
		Payload:   &pb.Frame_ResolveUsernameResponse{ResolveUsernameResponse: &pb.ResolveUsernameResponse{UserId: userID}},
	})
}

func (r *Router) handleAddContact(ctx context.Context, session *Session, reqID string, req *pb.AddContactRequest) error {
	if session.userID == "" {
		return r.sendError(ctx, session, reqID, 401, "not authenticated")
	}

	userID, username, err := r.handler.contacts.AddContact(ctx, session.userID, req.Username)
	if err != nil {
		return r.sendError(ctx, session, reqID, 404, err.Error())
	}

	return r.sendFrame(ctx, session, &pb.Frame{
		RequestId: reqID,
		Payload: &pb.Frame_AddContactResponse{AddContactResponse: &pb.AddContactResponse{
			UserId:   userID,
			Username: username,
		}},
	})
}

func (r *Router) handleRemoveContact(ctx context.Context, session *Session, reqID string, req *pb.RemoveContactRequest) error {
	if session.userID == "" {
		return r.sendError(ctx, session, reqID, 401, "not authenticated")
	}

	if err := r.handler.contacts.RemoveContact(ctx, session.userID, req.UserId); err != nil {
		return r.sendError(ctx, session, reqID, 500, "failed to remove contact")
	}

	return r.sendFrame(ctx, session, &pb.Frame{
		RequestId: reqID,
		Payload:   &pb.Frame_RemoveContactResponse{RemoveContactResponse: &pb.RemoveContactResponse{}},
	})
}

func (r *Router) handleListContacts(ctx context.Context, session *Session, reqID string) error {
	if session.userID == "" {
		return r.sendError(ctx, session, reqID, 401, "not authenticated")
	}

	contacts, err := r.handler.contacts.ListContacts(ctx, session.userID)
	if err != nil {
		return r.sendError(ctx, session, reqID, 500, "failed to list contacts")
	}

	var pbContacts []*pb.Contact
	for _, c := range contacts {
		pbContacts = append(pbContacts, &pb.Contact{
			UserId:   c.UserID,
			Username: c.Username,
			Online:   r.handler.relay.IsOnline(c.UserID),
		})
	}

	return r.sendFrame(ctx, session, &pb.Frame{
		RequestId: reqID,
		Payload:   &pb.Frame_ListContactsResponse{ListContactsResponse: &pb.ListContactsResponse{Contacts: pbContacts}},
	})
}

func (r *Router) handleStats(ctx context.Context, session *Session, reqID string) error {
	if session.userID == "" {
		return r.sendError(ctx, session, reqID, 401, "not authenticated")
	}

	totalUsers, err := r.handler.contacts.TotalUsers(ctx)
	if err != nil {
		return r.sendError(ctx, session, reqID, 500, "failed to get stats")
	}

	return r.sendFrame(ctx, session, &pb.Frame{
		RequestId: reqID,
		Payload: &pb.Frame_StatsResponse{StatsResponse: &pb.StatsResponse{
			TotalUsers:  totalUsers,
			OnlineUsers: r.handler.relay.OnlineCount(),
		}},
	})
}

func (r *Router) sendFrame(ctx context.Context, session *Session, frame *pb.Frame) error {
	data, err := proto.Marshal(frame)
	if err != nil {
		return fmt.Errorf("marshal frame: %w", err)
	}
	return session.Send(ctx, data)
}

func (r *Router) sendError(ctx context.Context, session *Session, reqID string, code uint32, msg string) error {
	return r.sendFrame(ctx, session, &pb.Frame{
		RequestId: reqID,
		Payload:   &pb.Frame_Error{Error: &pb.Error{Code: code, Message: msg}},
	})
}
