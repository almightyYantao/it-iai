package api

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/iai/vibedeploy/internal/auth"
)

type actorKind string

const (
	actorUser  actorKind = "user"
	actorToken actorKind = "token"
)

type Actor struct {
	Kind        actorKind
	UserID      uuid.UUID
	Email       string
	Name        string
	IsAdmin     bool
	Groups      []string
	TokenID     uuid.UUID
	TokenScopes []string
	// When set, the token can only operate on this project.
	TokenProjectID *uuid.UUID
	// The user who issued this token. Used to attribute project ownership
	// when a project is created via deploy token rather than a browser session.
	TokenCreatedBy *uuid.UUID
	AllowDB        bool
}

type ctxKey int

const ctxActor ctxKey = 1

func WithActor(ctx context.Context, a Actor) context.Context {
	return context.WithValue(ctx, ctxActor, a)
}

func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(ctxActor).(Actor)
	return a, ok
}

// AuthMiddleware verifies either a Deploy Token (vbd_…) or a Keycloak JWT.
// It populates the request context with an Actor. Public endpoints (`/healthz`,
// `/internal/auth/check`) are routed outside this middleware.
func (s *Server) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			writeError(w, http.StatusUnauthorized, "missing_bearer", "Authorization: Bearer <token> required")
			return
		}
		tok := strings.TrimPrefix(authHeader, "Bearer ")

		if auth.LooksLikeDeployToken(tok) {
			actor, err := s.verifyDeployToken(ctx, tok)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "bad_token", err.Error())
				return
			}
			next.ServeHTTP(w, r.WithContext(WithActor(ctx, actor)))
			return
		}

		// JWT path
		jwt := s.JWT()
		if !jwt.Enabled() {
			writeError(w, http.StatusUnauthorized, "jwt_disabled", "this instance only accepts deploy tokens; set CP_KC_* to enable JWT")
			return
		}
		claims, err := jwt.Verify(ctx, tok)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "bad_jwt", err.Error())
			return
		}
		u, err := s.store.UpsertUserByEmail(ctx, claims.Email, claims.DisplayName())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "user_upsert", err.Error())
			return
		}
		actor := Actor{
			Kind:    actorUser,
			UserID:  u.ID,
			Email:   u.Email,
			Name:    u.Name,
			IsAdmin: u.IsAdmin,
			Groups:  claims.Groups,
		}
		next.ServeHTTP(w, r.WithContext(WithActor(ctx, actor)))
	})
}

func (s *Server) verifyDeployToken(ctx context.Context, tok string) (Actor, error) {
	hash := auth.HashToken(tok)
	rec, err := s.store.LookupTokenByHash(ctx, hash)
	if err != nil {
		return Actor{}, err
	}
	if rec.RevokedAt != nil {
		return Actor{}, errRevoked
	}
	if time.Now().After(rec.ExpiresAt) {
		return Actor{}, errExpired
	}
	go s.store.TouchTokenLastUsed(context.Background(), rec.ID)
	return Actor{
		Kind:           actorToken,
		TokenID:        rec.ID,
		TokenScopes:    rec.Scopes,
		TokenProjectID: rec.ProjectID,
		TokenCreatedBy: rec.CreatedBy,
		AllowDB:        rec.AllowDB,
	}, nil
}

var (
	errRevoked = httpError{Code: "revoked", Msg: "token has been revoked"}
	errExpired = httpError{Code: "expired", Msg: "token has expired"}
)

type httpError struct {
	Code string
	Msg  string
}

func (e httpError) Error() string { return e.Msg }
