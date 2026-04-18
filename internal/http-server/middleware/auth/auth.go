// Package auth provides JWT authentication and authorization middleware.
package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"
	resp "url-shortener/internal/lib/api/response"
	"url-shortener/internal/lib/logger/sl"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	bearerPrefix   = "Bearer "
	isAdminTimeout = 2 * time.Second
)

type contextKey string

const (
	uidKey     contextKey = "uid"
	isAdminKey contextKey = "is_admin"
	errorKey   contextKey = "auth_error"
)

type PermissionProvider interface {
	IsAdmin(ctx context.Context, userID int64) (bool, error)
}

var (
	ErrInvalidToken       = errors.New("invalid token")
	ErrFailedIsAdminCheck = errors.New("failed to check if user is admin")
	ErrAuthServiceDown    = errors.New("auth service unavailable")
)

func New(
	log *slog.Logger,
	appSecret string,
	expectedAppID int64,
) func(next http.Handler) http.Handler {
	const op = "middleware.auth.New"

	log = log.With(slog.String("op", op))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tokenStr, ok := extractBearerToken(r)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			claims := jwt.MapClaims{}
			token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (any, error) {
				if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
					return nil, ErrInvalidToken
				}

				return []byte(appSecret), nil
			}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}))
			if err != nil || !token.Valid {
				if err != nil {
					log.Warn("failed to parse token", sl.Err(err))
				}

				next.ServeHTTP(w, r.WithContext(withError(r.Context(), ErrInvalidToken)))

				return
			}

			uid, ok := claimInt64(claims["uid"])
			if !ok || uid <= 0 {
				next.ServeHTTP(w, r.WithContext(withError(r.Context(), ErrInvalidToken)))

				return
			}

			appID, ok := claimInt64(claims["app_id"])
			if !ok || appID != expectedAppID {
				next.ServeHTTP(w, r.WithContext(withError(r.Context(), ErrInvalidToken)))

				return
			}

			ctx := context.WithValue(r.Context(), uidKey, uid)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func RequireAuth(log *slog.Logger) func(next http.Handler) http.Handler {
	const op = "middleware.auth.RequireAuth"

	log = log.With(slog.String("op", op))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqLog := log.With(slog.String("request_id", middleware.GetReqID(r.Context())))

			if err, ok := ErrorFromContext(r.Context()); ok {
				respondAuthError(reqLog, w, r, err)

				return
			}

			if _, ok := UIDFromContext(r.Context()); !ok {
				respondJSONError(w, r, http.StatusUnauthorized, "unauthorized")

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func RequireAdmin(log *slog.Logger, permProvider PermissionProvider) func(next http.Handler) http.Handler {
	const op = "middleware.auth.RequireAdmin"

	log = log.With(slog.String("op", op))

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			reqLog := log.With(slog.String("request_id", middleware.GetReqID(r.Context())))

			if err, ok := ErrorFromContext(r.Context()); ok {
				respondAuthError(reqLog, w, r, err)

				return
			}

			uid, ok := UIDFromContext(r.Context())
			if !ok {
				respondJSONError(w, r, http.StatusUnauthorized, "unauthorized")

				return
			}

			if permProvider == nil {
				respondAuthError(reqLog, w, r, ErrFailedIsAdminCheck)

				return
			}

			isAdminCtx, cancel := context.WithTimeout(r.Context(), isAdminTimeout)
			defer cancel()

			isAdmin, err := permProvider.IsAdmin(isAdminCtx, uid)
			if err != nil {
				authErr := ErrFailedIsAdminCheck
				switch status.Code(err) {
				case codes.DeadlineExceeded, codes.Unavailable:
					authErr = ErrAuthServiceDown
				case codes.NotFound:
					authErr = ErrInvalidToken
				}

				respondAuthError(reqLog, w, r, authErr)

				return
			}

			if !isAdmin {
				respondJSONError(w, r, http.StatusForbidden, "forbidden")

				return
			}

			ctx := context.WithValue(r.Context(), isAdminKey, true)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractBearerToken(r *http.Request) (string, bool) {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		return "", false
	}

	token := strings.TrimSpace(strings.TrimPrefix(authHeader, bearerPrefix))
	if token == "" {
		return "", false
	}

	return token, true
}

func claimInt64(raw any) (int64, bool) {
	switch v := raw.(type) {
	case float64:
		iv := int64(v)
		if v != float64(iv) {
			return 0, false
		}

		return iv, true
	case int64:
		return v, true
	case int32:
		return int64(v), true
	case int:
		return int64(v), true
	case string:
		iv, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, false
		}

		return iv, true
	default:
		return 0, false
	}
}

func withError(ctx context.Context, err error) context.Context {
	return context.WithValue(ctx, errorKey, err)
}

func respondAuthError(log *slog.Logger, w http.ResponseWriter, r *http.Request, err error) {
	log.Warn("auth middleware error", sl.Err(err))

	switch {
	case errors.Is(err, ErrInvalidToken):
		respondJSONError(w, r, http.StatusUnauthorized, "invalid token")
	case errors.Is(err, ErrAuthServiceDown):
		respondJSONError(w, r, http.StatusServiceUnavailable, "auth service unavailable")
	case errors.Is(err, ErrFailedIsAdminCheck):
		respondJSONError(w, r, http.StatusInternalServerError, "failed to check permissions")
	default:
		respondJSONError(w, r, http.StatusUnauthorized, "unauthorized")
	}
}

func respondJSONError(w http.ResponseWriter, r *http.Request, statusCode int, message string) {
	render.Status(r, statusCode)
	render.JSON(w, r, resp.Error(message))
}

func UIDFromContext(ctx context.Context) (int64, bool) {
	uid, ok := ctx.Value(uidKey).(int64)
	return uid, ok
}

func IsAdminFromContext(ctx context.Context) (bool, bool) {
	isAdmin, ok := ctx.Value(isAdminKey).(bool)

	return isAdmin, ok
}

func ErrorFromContext(ctx context.Context) (error, bool) {
	err, ok := ctx.Value(errorKey).(error)
	return err, ok
}
