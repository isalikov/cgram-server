package ws

import (
	"context"
	"fmt"

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
		return r.sendError(ctx, session, frame.RequestId, 400, "invalid frame")
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
		fmt.Printf("flush queue: %v\n", err)
	}

	return r.sendFrame(ctx, session, &pb.Frame{
		RequestId: reqID,
		Payload:   &pb.Frame_LoginResponse{LoginResponse: &pb.LoginResponse{SessionToken: token}},
	})
}

func (r *Router) handleLogout(ctx context.Context, session *Session, reqID string, req *pb.LogoutRequest) error {
	r.handler.auth.Logout(ctx, req.SessionToken)
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
		Payload:   &pb.Frame_Ack{Ack: &pb.Ack{MessageId: env.RecipientId, Delivered: true}},
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
