package redirect_test

import (
	"errors"
	"net/http/httptest"
	"testing"
	"url-shortener/internal/http-server/handlers/redirect"
	"url-shortener/internal/http-server/handlers/redirect/mocks"
	"url-shortener/internal/lib/api"
	"url-shortener/internal/lib/logger/handlers/slogdiscard"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	ErrAliasNotFound      = errors.New("alias not found")
	ErrDatabaseConnection = errors.New("database err")
	ErrInvalidAlias       = errors.New("bad request")
)

func TestRedirectHandler(t *testing.T) {
	cases := []struct {
		name      string
		alias     string
		url       string
		respError string
		mockError error
	}{
		// Успешные случаи
		{
			name:  "Success - basic alias",
			alias: "test_alias",
			url:   "https://www.google.com/",
		},
		{
			name:  "Success - with query params",
			alias: "gsearch",
			url:   "https://www.google.com/search?q=golang+testing",
		},
		{
			name:  "Success - https with port",
			alias: "api",
			url:   "https://api.example.com:8443/v1/users",
		},
		{
			name:  "Success - http without www",
			alias: "blog",
			url:   "http://myblog.com/post/123",
		},
		{
			name:  "Success - trailing slash in URL",
			alias: "home",
			url:   "https://example.com/path/",
		},
		{
			name:  "Success - alias with numbers and hyphens",
			alias: "promo-2024-summer",
			url:   "https://example.com/promo",
		},
		{
			name:  "Success - alias with underscore",
			alias: "user_profile",
			url:   "https://example.com/profile",
		},
		{
			name:  "Success - empty URL (edge case, if allowed)",
			alias: "empty",
			url:   "/",
		},
		{
			name:  "Success - very long URL",
			alias: "long",
			url:   "https://example.com/very/long/path/with/many/segments/and/query/params?foo=bar&baz=qux&abc=def&xyz=123&test=hello",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			urlGetterMock := mocks.NewMockURLGetter(t)

			if tc.respError == "" || tc.mockError != nil {
				urlGetterMock.On("GetURL", tc.alias).
					Return(tc.url, tc.mockError).Once()
			}

			r := chi.NewRouter()

			r.Get("/{alias}",
				redirect.New(slogdiscard.NewDiscardLogger(), urlGetterMock))

			ts := httptest.NewServer(r)
			defer ts.Close()

			redirectedToURL, err := api.GetRedirect(ts.URL + "/" + tc.alias)
			require.NoError(t, err)

			assert.Equal(t, tc.url, redirectedToURL)
		})
	}
}
