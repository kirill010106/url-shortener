package auth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
	auth "url-shortener/internal/http-server/middleware/auth"
	"url-shortener/internal/lib/logger/handlers/slogdiscard"

	"github.com/go-chi/chi/v5"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	testSecret = "test-secret"
	testAppID  = int64(1)
	testUID    = int64(42)
)

type permissionProviderStub struct {
	isAdmin bool
	err     error
	called  bool
	gotUID  int64
}

func (s *permissionProviderStub) IsAdmin(_ context.Context, userID int64) (bool, error) {
	s.called = true
	s.gotUID = userID

	return s.isAdmin, s.err
}

func TestRequireAuth_AllowsValidToken(t *testing.T) {
	t.Parallel()

	r := chi.NewRouter()
	r.Use(auth.New(slogdiscard.NewDiscardLogger(), testSecret, testAppID))
	r.With(auth.RequireAuth(slogdiscard.NewDiscardLogger())).Post("/url", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/url", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken(t, testSecret, testUID, testAppID, time.Now().Add(time.Hour)))
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
}

func TestRequireAuth_RejectsMissingToken(t *testing.T) {
	t.Parallel()

	r := chi.NewRouter()
	r.Use(auth.New(slogdiscard.NewDiscardLogger(), testSecret, testAppID))
	r.With(auth.RequireAuth(slogdiscard.NewDiscardLogger())).Post("/url", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/url", nil)
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestRequireAdmin_RejectsNonAdmin(t *testing.T) {
	t.Parallel()

	perm := &permissionProviderStub{isAdmin: false}

	r := chi.NewRouter()
	r.Use(auth.New(slogdiscard.NewDiscardLogger(), testSecret, testAppID))
	r.With(auth.RequireAdmin(slogdiscard.NewDiscardLogger(), perm)).Delete("/url/{alias}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodDelete, "/url/test", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken(t, testSecret, testUID, testAppID, time.Now().Add(time.Hour)))
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	require.True(t, perm.called)
	require.Equal(t, testUID, perm.gotUID)
	require.Equal(t, http.StatusForbidden, rr.Code)
}

func TestRequireAdmin_AllowsAdmin(t *testing.T) {
	t.Parallel()

	perm := &permissionProviderStub{isAdmin: true}

	r := chi.NewRouter()
	r.Use(auth.New(slogdiscard.NewDiscardLogger(), testSecret, testAppID))
	r.With(auth.RequireAdmin(slogdiscard.NewDiscardLogger(), perm)).Delete("/url/{alias}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodDelete, "/url/test", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken(t, testSecret, testUID, testAppID, time.Now().Add(time.Hour)))
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	require.True(t, perm.called)
	require.Equal(t, testUID, perm.gotUID)
	require.Equal(t, http.StatusOK, rr.Code)
}

func TestRequireAdmin_ReturnsServiceUnavailableOnSSODown(t *testing.T) {
	t.Parallel()

	perm := &permissionProviderStub{err: status.Error(codes.Unavailable, "sso unavailable")}

	r := chi.NewRouter()
	r.Use(auth.New(slogdiscard.NewDiscardLogger(), testSecret, testAppID))
	r.With(auth.RequireAdmin(slogdiscard.NewDiscardLogger(), perm)).Delete("/url/{alias}", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodDelete, "/url/test", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken(t, testSecret, testUID, testAppID, time.Now().Add(time.Hour)))
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusServiceUnavailable, rr.Code)
}

func makeToken(t *testing.T, secret string, uid int64, appID int64, exp time.Time) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"uid":    uid,
		"app_id": appID,
		"exp":    exp.Unix(),
	})

	tokenString, err := token.SignedString([]byte(secret))
	require.NoError(t, err)

	return tokenString
}
