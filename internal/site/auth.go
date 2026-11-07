package site

import (
	"errors"
	"net/http"

	"github.com/abdulovia/cookcompass/internal/users"
	"github.com/abdulovia/cookcompass/internal/web"
)

// SessionCookie holds the login session token in multi-user mode.
const SessionCookie = "session"

func (h *Handler) loginForm(w http.ResponseWriter, r *http.Request) {
	if !h.App.Cfg.Auth.MultiUser {
		web.Redirect(w, r, "/", "")
		return
	}
	h.R.Page(w, http.StatusOK, "pages/login", web.NewPage(r, "auth.title"))
}

func (h *Handler) startSession(w http.ResponseWriter, r *http.Request, userID int64) bool {
	token, err := h.App.Users.NewSession(r.Context(), userID)
	if err != nil {
		h.Fail(w, r, err)
		return false
	}
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: token, Path: "/", MaxAge: int(users.SessionTTL.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.App.Cfg.Auth.SecureCookies})
	return true
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	if !h.App.Cfg.Auth.MultiUser {
		web.Redirect(w, r, "/", "")
		return
	}
	id, err := h.App.Users.Authenticate(r.Context(), r.FormValue("username"), r.FormValue("password"))
	if errors.Is(err, users.ErrBadLogin) {
		p := web.NewPage(r, "auth.title")
		p.Errors = map[string]string{"login": "auth.bad_login"}
		h.R.Page(w, http.StatusUnauthorized, "pages/login", p)
		return
	}
	if err != nil {
		h.Fail(w, r, err)
		return
	}
	if h.startSession(w, r, id) {
		web.Redirect(w, r, "/", "")
	}
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	if !h.App.Cfg.Auth.MultiUser {
		web.Redirect(w, r, "/", "")
		return
	}
	id, err := h.App.Users.Create(r.Context(), r.FormValue("username"), r.FormValue("password"), h.App.Cfg.Defaults.Country)
	if err != nil {
		p := web.NewPage(r, "auth.title")
		msg := "auth.bad_register"
		if errors.Is(err, users.ErrUsernameUsed) {
			msg = "auth.username_taken"
		}
		p.Errors = map[string]string{"register": msg}
		h.R.Page(w, http.StatusUnprocessableEntity, "pages/login", p)
		return
	}
	if h.startSession(w, r, id) {
		web.Redirect(w, r, "/settings", "")
	}
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil {
		_ = h.App.Users.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1})
	web.Redirect(w, r, "/", "")
}
