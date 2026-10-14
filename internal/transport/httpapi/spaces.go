package httpapi

import (
	"net/http"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

func (s *Server) handleSpacesList(w http.ResponseWriter, r *http.Request) {
	spaces, err := s.Svc.ListSpaces(r.Context(), actorOf(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"spaces": spaces})
}

func (s *Server) handleSpacesCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name    string               `json:"name"`
		Kind    string               `json:"kind"`
		Storage *ports.StorageConfig `json:"storage,omitempty"`
	}
	if !decode(w, r, &in) {
		return
	}
	sp, err := s.Svc.CreateSpace(r.Context(), actorOf(r), in.Name, in.Kind)
	if err != nil {
		writeErr(w, err)
		return
	}
	if in.Storage != nil && in.Storage.Provider != "base" {
		if err = s.Svc.ConfigureVaultStorage(r.Context(), actorOf(r), sp.ID, *in.Storage); err != nil {
			_ = s.Svc.Spaces.DeleteSpace(r.Context(), sp.ID)
			writeErr(w, err)
			return
		}
		sp, err = s.Svc.Spaces.Space(r.Context(), sp.ID)
		if err != nil {
			writeErr(w, err)
			return
		}
	}
	writeJSON(w, 201, map[string]any{"space": sp, "role": "owner"})
}

func (s *Server) handleSpaceGet(w http.ResponseWriter, r *http.Request) {
	d, err := s.Svc.GetSpaceDetail(r.Context(), actorOf(r), spaceID(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, d)
}

func (s *Server) handleSpaceArchive(archive bool) http.HandlerFunc {
	return s.wrap2(func(w http.ResponseWriter, r *http.Request) {
		if err := s.Svc.ArchiveSpace(r.Context(), actorOf(r), spaceID(r), archive); err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
}

func (s *Server) handleSpaceDelete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Confirmation string `json:"confirmation"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.Svc.DeleteVault(r.Context(), actorOf(r), spaceID(r), in.Confirmation); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// wrap2 exists so parameterized handlers share identity middleware.
func (s *Server) wrap2(h func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return s.wrap(h, false)
}

func (s *Server) handleInviteCreate(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string      `json:"email"`
		Role  domain.Role `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	url, inv, err := s.Svc.Invite(r.Context(), actorOf(r), spaceID(r), in.Email, in.Role)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{
		"invitation": map[string]any{"id": inv.ID, "email": inv.Email, "role": inv.Role, "expiresAt": inv.ExpiresAt},
		"inviteUrl":  url,
	})
}

func (s *Server) handleInviteRevoke(w http.ResponseWriter, r *http.Request) {
	a := actorOf(r)
	if _, err := s.Svc.Require(r.Context(), a, spaceID(r), domain.ActionMembers); err != nil {
		writeErr(w, err)
		return
	}
	invs, err := s.Svc.Invites.InvitationsForSpace(r.Context(), spaceID(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	found := false
	for _, iv := range invs {
		if iv.ID == r.PathValue("id") {
			found = true
		}
	}
	if !found {
		writeErr(w, domain.NotFound("invitation not found"))
		return
	}
	if err := s.Svc.Invites.RevokeInvitation(r.Context(), r.PathValue("id")); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleMemberRole(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role domain.Role `json:"role"`
	}
	if !decode(w, r, &in) {
		return
	}
	if err := s.Svc.SetMemberRole(r.Context(), actorOf(r), spaceID(r), r.PathValue("member"), in.Role); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) handleMemberRemove(w http.ResponseWriter, r *http.Request) {
	if err := s.Svc.RemoveMember(r.Context(), actorOf(r), spaceID(r), r.PathValue("member")); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
