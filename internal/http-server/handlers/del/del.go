package del

import (
	"errors"
	"log/slog"
	"net/http"
	"url-shortener/internal/lib/api/response"
	resp "url-shortener/internal/lib/api/response"
	"url-shortener/internal/lib/logger/sl"
	"url-shortener/internal/storage/postgres"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"
)

type Response struct {
	resp.Response
	Msg   string `json:"msg"`
	Alias string `json:"alias,omitempty"`
}

type URLDeleter interface {
	DeleteURL(alias string) error
}

func New(log *slog.Logger, urlDeleter URLDeleter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		op := "handlers.delete.New"

		log := log.With(
			slog.String("op", op),
			slog.String("request_id", middleware.GetReqID(r.Context())),
		)

		alias := chi.URLParam(r, "alias")

		if alias == "" {
			log.Info("alias is empty")

			render.JSON(w, r, "invalid request")

			return
		}

		err := urlDeleter.DeleteURL(alias)

		if errors.Is(err, postgres.ErrURLNotFound) {
			log.Info("url is not found", "alias", alias)

			render.JSON(w, r, "not found")

			return
		}

		if err != nil {
			log.Info("failed to get url", sl.Err(err))

			render.JSON(w, r, "internal error")

			return
		}

		log.Info("url deleted succesfully", "alias", alias)
		responseOK(w, r, alias)
	}
}

func responseOK(w http.ResponseWriter, r *http.Request, alias string) {
	render.JSON(w, r, Response{
		Response: response.OK(),
		Msg:      "url deleted succesfully",
		Alias:    alias,
	})
}
