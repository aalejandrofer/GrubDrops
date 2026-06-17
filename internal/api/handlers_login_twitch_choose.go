package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

type loginTwitchChooseDeps struct {
	q *gen.Queries
	t Renderer
}

type loginTwitchChoosePageData struct {
	AccountID   string
	DisplayName string
}

func (d *loginTwitchChooseDeps) get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	render(w, r, d.t, "login_twitch_choose.html", templateData{
		AuthedAdmin: true, CSRFToken: csrfToken(r),
		Page: loginTwitchChoosePageData{
			AccountID:   id,
			DisplayName: acc.DisplayName,
		},
	})
}
