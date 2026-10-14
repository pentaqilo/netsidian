package httpapi

import (
	"net/http"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/service"
)

// handleTOTPStatus reports the caller's second-factor state for the account UI.
func (s *Server) handleTOTPStatus(w http.ResponseWriter, r *http.Request) {
	if actorOf(r).Key != nil {
		writeErr(w, domain.Forbidden("human session required"))
		return
	}
	st, err := s.Svc.TOTPStatus(r.Context(), actorOf(r).UserID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, st)
}

// handleTOTPEnroll starts enrollment and returns the secret + recovery codes
// exactly once. The factor is not enforced until it is confirmed.
func (s *Server) handleTOTPEnroll(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	view, err := s.Svc.StartTOTPEnrollment(r.Context(), actorOf(r), in.Password)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, view)
}

// handleTOTPConfirm activates a pending enrollment after a valid code.
func (s *Server) handleTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	st, err := s.Svc.ConfirmTOTPEnrollment(r.Context(), actorOf(r), in.Code)
	if err != nil {
		writeErr(w, err)
		return
	}
	clearSessionCookie(w)
	writeJSON(w, 200, st)
}

// handleTOTPDisable removes the factor; it needs the current password and a
// valid current code.
func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.Svc.DisableTOTP(r.Context(), actorOf(r), in.Password, in.Code); err != nil {
		writeErr(w, err)
		return
	}
	clearSessionCookie(w)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// handleTOTPRecovery regenerates recovery codes; it needs the current password
// and a valid current code. The new codes are shown once.
func (s *Server) handleTOTPRecovery(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	codes, err := s.Svc.RegenerateRecoveryCodes(r.Context(), actorOf(r), in.Password, in.Code)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"recoveryCodes": codes})
}

// completeChallenge finishes a pending login challenge and issues the session.
// It is a pre-auth endpoint: the challenge token, not a cookie, authorizes it.
func (s *Server) completeChallenge(w http.ResponseWriter, r *http.Request, recovery bool) {
	var in struct {
		ChallengeToken string `json:"challengeToken"`
		Code           string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	var out *service.LoginOutcome
	var err error
	if recovery {
		out, err = s.Svc.CompleteChallengeWithRecovery(r.Context(), in.ChallengeToken, in.Code)
	} else {
		out, err = s.Svc.CompleteChallengeWithTOTP(r.Context(), in.ChallengeToken, in.Code)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	setSessionCookie(w, r, out.SessionToken, out.SessionExpires, s.TrustedProxy)
	writeJSON(w, 200, map[string]any{"userId": out.User.ID, "displayName": out.User.DisplayName})
}

func (s *Server) handleChallengeTOTP(w http.ResponseWriter, r *http.Request) {
	s.completeChallenge(w, r, false)
}

func (s *Server) handleChallengeRecovery(w http.ResponseWriter, r *http.Request) {
	s.completeChallenge(w, r, true)
}

// handleAdminTOTPReset is the documented operator-recovery path: it removes a
// user's second factor so they can sign in with their password and re-enroll.
func (s *Server) handleAdminTOTPReset(w http.ResponseWriter, r *http.Request) {
	if err := s.Svc.RequireAdministrator(r.Context(), actorOf(r)); err != nil {
		writeErr(w, err)
		return
	}
	var in struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.Svc.ResetUserTOTP(r.Context(), actorOf(r), r.PathValue("id"), in.Password, in.Code); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
