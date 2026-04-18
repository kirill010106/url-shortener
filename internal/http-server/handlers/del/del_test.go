package del_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"url-shortener/internal/http-server/handlers/del"
	"url-shortener/internal/http-server/handlers/del/mocks"
	"url-shortener/internal/lib/logger/handlers/slogdiscard"
	"url-shortener/internal/storage/postgres"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

func TestDelHandler(t *testing.T) {
	cases := []struct {
		name      string
		alias     string
		url       string
		respError string
		mockError error
	}{
		{
			name:  "valid request",
			alias: "short",
			url:   "/shorten",
		},
		{
			name:      "not found",
			alias:     "missing-alias",
			url:       "/invalid",
			respError: "not found",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			urlDeleterMock := mocks.NewMockURLDeleter(t)
			if tc.alias != "" {
				retErr := tc.mockError
				if tc.respError == "not found" {
					retErr = postgres.ErrURLNotFound
				}
				urlDeleterMock.On("DeleteURL", tc.alias).Return(retErr).Once()
			}

			r := chi.NewRouter()

			r.Delete("/url/{alias}",
				del.New(slogdiscard.NewDiscardLogger(), urlDeleterMock))

			path := "/url/" + tc.alias
			req := httptest.NewRequest(http.MethodDelete, path, nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)

			if tc.respError == "" {
				require.Equal(t, http.StatusOK, rr.Code)
			}

			if tc.respError != "" {
				require.Contains(t, rr.Body.String(), tc.respError)
				return
			}

			var resp del.Response
			require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
			require.Equal(t, "OK", resp.Status)
			require.Equal(t, "url deleted succesfully", resp.Msg)
			require.Equal(t, tc.alias, resp.Alias)
		})
	}
}
