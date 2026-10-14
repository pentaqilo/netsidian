package sqlite

import (
	"context"
	"database/sql"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

const spaceCols = `id, slug, name, kind, storage_binding, archived_at, permission_version, created_at`

func scanSpace(row interface{ Scan(...any) error }) (*domain.Space, error) {
	var s domain.Space
	var archived sql.NullString
	var created string
	if err := row.Scan(&s.ID, &s.Slug, &s.Name, &s.Kind, &s.StorageBinding, &archived, &s.PermissionVersion, &created); err != nil {
		return nil, err
	}
	s.ArchivedAt = scanNullTime(archived)
	s.CreatedAt, _ = parseTime(created)
	return &s, nil
}

func (db *DB) CreateSpace(ctx context.Context, s *domain.Space) error {
	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO spaces(id,slug,name,kind,storage_binding,archived_at,permission_version,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		s.ID, s.Slug, s.Name, s.Kind, s.StorageBinding, fmtTime(s.ArchivedAt), s.PermissionVersion, s.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (db *DB) Space(ctx context.Context, id string) (*domain.Space, error) {
	row := db.SQL.QueryRowContext(ctx, `SELECT `+spaceCols+` FROM spaces WHERE id=?`, id)
	s, err := scanSpace(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
}

func (db *DB) SpacesForUser(ctx context.Context, userID string) ([]*domain.Space, []domain.Role, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT `+prefixCols("s", spaceCols)+`, m.role
		 FROM spaces s JOIN memberships m ON m.space_id=s.id
		 WHERE m.user_id=? ORDER BY s.created_at ASC`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var spaces []*domain.Space
	var roles []domain.Role
	for rows.Next() {
		var s domain.Space
		var archived sql.NullString
		var created, role string
		if err := rows.Scan(&s.ID, &s.Slug, &s.Name, &s.Kind, &s.StorageBinding, &archived, &s.PermissionVersion, &created, &role); err != nil {
			return nil, nil, err
		}
		s.ArchivedAt = scanNullTime(archived)
		s.CreatedAt, _ = parseTime(created)
		spaces = append(spaces, &s)
		roles = append(roles, domain.Role(role))
	}
	return spaces, roles, rows.Err()
}

func prefixCols(p, cols string) string {
	out := ""
	for i, c := range splitCSV(cols) {
		if i > 0 {
			out += ", "
		}
		out += p + "." + c
	}
	return out
}

func splitCSV(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		switch r {
		case ',':
			out = append(out, trim(cur))
			cur = ""
		default:
			cur += string(r)
		}
	}
	out = append(out, trim(cur))
	return out
}

func trim(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ') {
		start++
	}
	for end > start && (s[end-1] == ' ') {
		end--
	}
	return s[start:end]
}

func (db *DB) SlugTaken(ctx context.Context, slug string) (bool, error) {
	var n int
	err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM spaces WHERE slug=?`, slug).Scan(&n)
	return n > 0, err
}

func (db *DB) ArchiveSpace(ctx context.Context, id string, at *time.Time) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE spaces SET archived_at=? WHERE id=?`, fmtTime(at), id)
	return err
}

func (db *DB) RenameSpace(ctx context.Context, id, name string) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE spaces SET name=? WHERE id=?`, name, id)
	return err
}

func (db *DB) SetStorageBinding(ctx context.Context, id, binding string) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE spaces SET storage_binding=? WHERE id=?`, binding, id)
	return err
}

func (db *DB) DeleteSpace(ctx context.Context, id string) error {
	_, err := db.SQL.ExecContext(ctx, `DELETE FROM spaces WHERE id=?`, id)
	return err
}

func (db *DB) AddMember(ctx context.Context, m *domain.Membership) error {
	_, err := db.SQL.ExecContext(ctx, `INSERT INTO memberships(id,space_id,user_id,role) VALUES(?,?,?,?)`,
		m.ID, m.SpaceID, m.UserID, string(m.Role))
	return err
}

func (db *DB) Members(ctx context.Context, spaceID string) ([]*ports.MemberWithUser, error) {
	rows, err := db.SQL.QueryContext(ctx,
		`SELECT m.id, m.space_id, m.user_id, m.role, u.display_name, u.email
		 FROM memberships m JOIN users u ON u.id=m.user_id
		 WHERE m.space_id=? ORDER BY CASE m.role WHEN 'owner' THEN 0 WHEN 'editor' THEN 1 ELSE 2 END, u.display_name`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ports.MemberWithUser
	for rows.Next() {
		var m domain.Membership
		var displayName string
		var email sql.NullString
		if err := rows.Scan(&m.ID, &m.SpaceID, &m.UserID, &m.Role, &displayName, &email); err != nil {
			return nil, err
		}
		out = append(out, &ports.MemberWithUser{
			Membership: &m, UserID: m.UserID, DisplayName: displayName,
			Email: textOf(email), Role: m.Role,
		})
	}
	return out, rows.Err()
}

func (db *DB) Membership(ctx context.Context, spaceID, userID string) (*domain.Membership, error) {
	row := db.SQL.QueryRowContext(ctx, `SELECT id, space_id, user_id, role FROM memberships WHERE space_id=? AND user_id=?`, spaceID, userID)
	var m domain.Membership
	if err := row.Scan(&m.ID, &m.SpaceID, &m.UserID, &m.Role); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &m, nil
}

func (db *DB) SetMemberRole(ctx context.Context, spaceID, memberID string, role domain.Role) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE memberships SET role=? WHERE id=? AND space_id=?`, string(role), memberID, spaceID)
	return err
}

func (db *DB) RemoveMember(ctx context.Context, spaceID, memberID string) error {
	_, err := db.SQL.ExecContext(ctx, `DELETE FROM memberships WHERE id=? AND space_id=?`, memberID, spaceID)
	return err
}

func (db *DB) OwnerCount(ctx context.Context, spaceID string) (int, error) {
	var n int
	err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM memberships WHERE space_id=? AND role='owner'`, spaceID).Scan(&n)
	return n, err
}

func (db *DB) BumpPermissionVersion(ctx context.Context, spaceID string) (int64, error) {
	_, err := db.SQL.ExecContext(ctx, `UPDATE spaces SET permission_version=permission_version+1 WHERE id=?`, spaceID)
	if err != nil {
		return 0, err
	}
	var v int64
	err = db.SQL.QueryRowContext(ctx, `SELECT permission_version FROM spaces WHERE id=?`, spaceID).Scan(&v)
	return v, err
}

// ---------- Invitations ----------

const invCols = `id, space_id, email, role, token_hash, issued_by, expires_at, consumed_at, revoked_at, created_at`

func scanInv(row interface{ Scan(...any) error }) (*ports.Invitation, error) {
	var i ports.Invitation
	var expires, created string
	var consumed, revoked sql.NullString
	if err := row.Scan(&i.ID, &i.SpaceID, &i.Email, &i.Role, &i.TokenHash, &i.IssuedBy, &expires, &consumed, &revoked, &created); err != nil {
		return nil, err
	}
	i.ExpiresAt, _ = parseTime(expires)
	i.ConsumedAt = scanNullTime(consumed)
	i.RevokedAt = scanNullTime(revoked)
	i.CreatedAt, _ = parseTime(created)
	return &i, nil
}

func (db *DB) CreateInvitation(ctx context.Context, inv *ports.Invitation) error {
	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO invitations(id,space_id,email,role,token_hash,issued_by,expires_at,consumed_at,revoked_at,created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		inv.ID, inv.SpaceID, inv.Email, string(inv.Role), inv.TokenHash, inv.IssuedBy,
		inv.ExpiresAt.UTC().Format(time.RFC3339Nano), fmtTime(inv.ConsumedAt), fmtTime(inv.RevokedAt), inv.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (db *DB) InvitationsForSpace(ctx context.Context, spaceID string) ([]*ports.Invitation, error) {
	rows, err := db.SQL.QueryContext(ctx, `SELECT `+invCols+` FROM invitations WHERE space_id=? ORDER BY created_at DESC`, spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ports.Invitation
	for rows.Next() {
		i, err := scanInv(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func (db *DB) InvitationByTokenHash(ctx context.Context, tokenHash string) (*ports.Invitation, error) {
	row := db.SQL.QueryRowContext(ctx, `SELECT `+invCols+` FROM invitations WHERE token_hash=?`, tokenHash)
	i, err := scanInv(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return i, err
}

func (db *DB) ConsumeInvitation(ctx context.Context, id string, at time.Time) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE invitations SET consumed_at=? WHERE id=? AND consumed_at IS NULL AND revoked_at IS NULL`, at.UTC().Format(time.RFC3339Nano), id)
	return err
}

func (db *DB) RevokeInvitation(ctx context.Context, id string) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE invitations SET revoked_at=? WHERE id=?`, nowText(), id)
	return err
}

func (db *DB) AllSpaceIDs(ctx context.Context) ([]string, error) {
	rows, err := db.SQL.QueryContext(ctx, `SELECT id FROM spaces WHERE archived_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
