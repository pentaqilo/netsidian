package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/service"
)

const sessionCookie = "nsd_session"

// identify resolves the request identity: bearer key > session cookie >
// explicit no-auth local principal. A browser-supplied user id is never
// authorization.
func (s *Server) identify(r *http.Request) (*service.Actor, error) {
	if actor := desktopMCPActor(r.Context()); actor != nil {
		return actor, nil
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		k, u, err := s.Svc.AuthenticateKey(r.Context(), strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			return nil, err
		}
		if err := s.Svc.CheckUserAccess(r.Context(), u); err != nil {
			// Long-lived PATs revalidate external access through the server-held
			// refresh grant; they never extend a browser session or bypass policy.
			if s.Svc.RefreshExternalAccess == nil || s.Svc.RefreshExternalAccess(r.Context(), u.ID) != nil {
				return nil, err
			}
			u, err = s.Svc.Users.UserByID(r.Context(), u.ID)
			if err != nil {
				return nil, err
			}
			if err = s.Svc.CheckUserAccess(r.Context(), u); err != nil {
				return nil, err
			}
		}
		return &service.Actor{
			UserID: u.ID, DisplayName: u.DisplayName, IsOperator: u.IsOperator,
			Key: k, ExecutionTarget: s.executionTarget(),
		}, nil
	}
	if s.Svc.Cfg.AuthMode == "none" {
		u, err := s.Svc.EnsureNoAuthPrincipal(r.Context())
		if err != nil {
			return nil, domain.Unavailable("instance unavailable")
		}
		return &service.Actor{
			UserID: u.ID, DisplayName: u.DisplayName, IsOperator: true,
			Local: true, ExecutionTarget: s.executionTarget(),
		}, nil
	}
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		userID, ok, err := s.Svc.ResolveSession(r.Context(), c.Value)
		if err == nil && ok {
			u, err := s.Svc.Users.UserByID(r.Context(), userID)
			if err == nil && u != nil && !u.Disabled {
				if err := s.Svc.CheckUserAccess(r.Context(), u); err != nil {
					return nil, err
				}
				return &service.Actor{
					UserID: u.ID, DisplayName: u.DisplayName, IsOperator: u.IsOperator,
					ExecutionTarget: s.executionTarget(),
				}, nil
			}
		}
	}
	return nil, domain.Unauthenticated("authentication required")
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time, trusted bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   schemeOf(r, trusted) == "https",
		SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

// handleSetup is only reachable while local mode has zero users.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"displayName"`
	}
	if !decode(w, r, &in) {
		return
	}
	if s.Svc.Cfg.AuthMode != "local" && s.Svc.Cfg.AuthMode != "mixed" {
		writeErr(w, domain.Unsupported("setup only applies to local auth mode"))
		return
	}
	u, err := s.Svc.SetupAccount(r.Context(), in.Email, in.Password, in.DisplayName)
	if err != nil {
		writeErr(w, err)
		return
	}
	// ensure the operator owns a personal space
	a := &service.Actor{UserID: u.ID, IsOperator: true}
	if spaces, _, err := s.Svc.Spaces.SpacesForUser(r.Context(), u.ID); err == nil && len(spaces) == 0 {
		if _, err := s.Svc.CreateSpace(r.Context(), a, "Personal", "personal"); err != nil {
			writeErr(w, err)
			return
		}
	}
	token, exp, err := s.Svc.NewSession(r.Context(), u.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	setSessionCookie(w, r, token, exp, s.TrustedProxy)
	writeJSON(w, 201, map[string]any{"userId": u.ID, "displayName": u.DisplayName})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if s.Svc.Cfg.AuthMode == "external" {
		writeErr(w, domain.Unsupported("use the configured sign-in provider"))
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if s.Svc.Cfg.AuthMode != "local" && s.Svc.Cfg.AuthMode != "mixed" {
		writeErr(w, domain.Unsupported("login not available in this auth mode"))
		return
	}
	out, err := s.Svc.LoginWithFactor(r.Context(), in.Email, in.Password)
	if err != nil {
		writeErr(w, err)
		return
	}
	// A confirmed second factor yields a pending challenge instead of a
	// session: no cookie is set, and the challenge token authorizes only the
	// challenge-completion endpoints.
	if out.ChallengeToken != "" {
		writeJSON(w, 200, map[string]any{
			"challenge": true, "challengeToken": out.ChallengeToken,
			"userId": out.User.ID, "displayName": out.User.DisplayName,
		})
		return
	}
	setSessionCookie(w, r, out.SessionToken, out.SessionExpires, s.TrustedProxy)
	writeJSON(w, 200, map[string]any{"userId": out.User.ID, "displayName": out.User.DisplayName})
}

func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email       string `json:"email"`
		Password    string `json:"password"`
		DisplayName string `json:"displayName"`
	}
	if !decode(w, r, &in) {
		return
	}
	if s.Svc.Cfg.AuthMode != "local" && s.Svc.Cfg.AuthMode != "mixed" || !s.Svc.Cfg.SignupEnabled {
		// route existence is not advertised when disabled
		writeErr(w, domain.NotFound("not found"))
		return
	}
	u, err := s.Svc.SignupAccount(r.Context(), in.Email, in.Password, in.DisplayName)
	if err != nil {
		writeErr(w, err)
		return
	}
	token, exp, err := s.Svc.NewSession(r.Context(), u.ID)
	if err != nil {
		writeErr(w, err)
		return
	}
	setSessionCookie(w, r, token, exp, s.TrustedProxy)
	writeJSON(w, 201, map[string]any{"userId": u.ID, "displayName": u.DisplayName})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.Svc.Logout(r.Context(), c.Value)
	}
	clearSessionCookie(w)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// handleChangePassword rotates the caller's local-account password. The
// revocation policy (see docs/authentication.md) ends every session and
// personal API key of the account atomically, so the current cookie is
// cleared and the client must sign in again with the new password.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
		Code            string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	res, err := s.Svc.ChangePassword(r.Context(), actorOf(r), in.CurrentPassword, in.NewPassword, in.Code)
	if err != nil {
		writeErr(w, err)
		return
	}
	clearSessionCookie(w)
	writeJSON(w, 200, map[string]any{
		"ok": true, "reauthRequired": true,
		"sessionsRevoked": res.SessionsRevoked, "keysRevoked": res.KeysRevoked,
	})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	canChange := false
	if a.Key == nil && (s.Svc.Cfg.AuthMode == "local" || s.Svc.Cfg.AuthMode == "mixed") {
		u, err := s.Svc.Users.UserByID(r.Context(), a.UserID)
		if err != nil {
			writeErr(w, err)
			return
		}
		canChange = u != nil && !u.Disabled && u.PasswordHash != ""
	}
	totp, err := s.Svc.TOTPStatus(r.Context(), a.UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{
		"userId": a.UserID, "displayName": a.DisplayName, "isOperator": a.IsOperator,
		"isKey": a.Key != nil, "canChangePassword": canChange,
		"totpEnabled": totp.Enabled, "totpPending": totp.Pending,
		"recoveryCodesRemaining": totp.RecoveryCodesRemaining,
	})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	st, err := s.Svc.GetSettings(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, struct {
		*service.Settings
		Runtime RuntimeCapabilities `json:"runtime"`
	}{st, s.runtimeCapabilities()})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if err := s.Svc.Health(r.Context()); err != nil {
		writeJSON(w, 503, map[string]any{"status": "unavailable", "time": time.Now().UTC()})
		return
	}
	writeJSON(w, 200, map[string]any{"status": "ok", "version": service.Version, "time": time.Now().UTC()})
}

func (s *Server) handlePrefsGet(w http.ResponseWriter, r *http.Request) {
	p, err := s.Svc.GetPreferences(r.Context(), actorOf(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, p)
}

func (s *Server) handlePrefsPut(w http.ResponseWriter, r *http.Request) {
	var prefs map[string]any
	if !decode(w, r, &prefs) {
		return
	}
	if err := s.Svc.SetPreferences(r.Context(), actorOf(r), prefs); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, prefs)
}

func (s *Server) handleKeysList(w http.ResponseWriter, r *http.Request) {
	keys, err := s.Svc.ListKeys(r.Context(), actorOf(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"keys": keys})
}

func (s *Server) handleKeysCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Label         string   `json:"label"`
		SpaceIDs      []string `json:"spaceIds"`
		Scopes        []string `json:"scopes"`
		ExpiresInDays int      `json:"expiresInDays"`
	}
	if !decode(w, r, &in) {
		return
	}
	if actorOf(r).Key != nil {
		writeErr(w, domain.Forbidden("key management requires a human session"))
		return
	}
	if len(in.SpaceIDs) == 0 || len(in.SpaceIDs) > 100 || len(in.Scopes) == 0 || len(in.Scopes) > 20 {
		writeErr(w, domain.Invalid("explicit vaults and scopes are required"))
		return
	}
	for _, scope := range in.Scopes {
		if strings.HasPrefix(scope, "memory:") {
			writeErr(w, domain.Invalid("memory uses ordinary document scopes"))
			return
		}
	}
	rec, secret, err := s.Svc.CreateKey(r.Context(), actorOf(r), in.Label, in.SpaceIDs, in.Scopes, in.ExpiresInDays)
	if err != nil {
		writeErr(w, err)
		return
	}
	// returned exactly once; only the hash persists
	writeJSON(w, 201, map[string]any{
		"id": rec.ID, "label": rec.Label, "scopes": rec.Scopes, "spaceIds": rec.SpaceIDs,
		"expiresAt": rec.ExpiresAt, "secret": secret,
	})
}

func (s *Server) handleKeysRevoke(w http.ResponseWriter, r *http.Request) {
	if err := s.Svc.RevokeKey(r.Context(), actorOf(r), r.PathValue("id")); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleInviteAccept(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !decode(w, r, &in) {
		return
	}
	sp, err := s.Svc.AcceptInvitation(r.Context(), actorOf(r), in.Token)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"spaceId": sp.ID, "name": sp.Name})
}

// handleIntegrations exposes only configuration flags to human operators.
func (s *Server) handleIntegrations(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	if a.Key != nil || !a.IsOperator {
		writeErr(w, domain.Forbidden("integration diagnostics require a human operator"))
		return
	}
	storage := "local"
	if strings.HasPrefix(s.Svc.Cfg.StorageBinding, "s3") {
		storage = "s3"
	}
	result := map[string]any{"storage": storage, "oauthConfigured": s.OAuth != nil, "backendReachable": s.Svc.Health(r.Context()) == nil}
	if s.OAuth != nil {
		result["oauthLabel"] = s.OAuth.label
	}
	writeJSON(w, 200, result)
}
