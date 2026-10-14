package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

func (db *DB) ExternalAuth(ctx context.Context, id string) (*ports.ExternalAuth, error) {
	var proof ports.ExternalAuth
	var claims, expires string
	err := db.SQL.QueryRowContext(ctx, `SELECT issuer,verified_claims,claims_expires_at FROM external_identities WHERE user_id=?`, id).Scan(&proof.Issuer, &claims, &expires)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(claims), &proof.Claims); err != nil {
		return nil, err
	}
	proof.ExpiresAt, _ = parseTime(expires)
	return &proof, nil
}

func (db *DB) UpdateExternalAuth(ctx context.Context, id string, claims map[string]any, expires time.Time, operator bool) error {
	raw, err := json.Marshal(claims)
	if err != nil {
		return err
	}
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Take the SQLite write lock before reading a role to avoid concurrent grants.
	if _, err = tx.ExecContext(ctx, `UPDATE users SET is_operator=is_operator WHERE id=?`, id); err != nil {
		return err
	}
	var previous bool
	if err = tx.QueryRowContext(ctx, `SELECT is_operator FROM users WHERE id=?`, id).Scan(&previous); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE external_identities SET verified_claims=?,claims_expires_at=? WHERE user_id=?`, string(raw), expires.UTC().Format(time.RFC3339Nano), id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET is_operator=? WHERE id=?`, b2i(operator), id); err != nil {
		return err
	}
	if previous != operator {
		if _, err = tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, nowText(), id); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(id,actor,action,resource,outcome,details,created_at) VALUES(?,?,?,?,?,?,?)`, domain.NewID("aud"), id, "oidc.role", id, "success", `{"isOperator":`+map[bool]string{true: "true", false: "false"}[operator]+`}`, nowText()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) AdminUsers(ctx context.Context, query string, offset, limit int) ([]ports.AdminUser, int, error) {
	where := ` WHERE instr(lower(coalesce(email,'')),lower(?))>0 OR instr(lower(display_name),lower(?))>0 OR instr(id,?)>0`
	var total int
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM users`+where, query, query, query).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := db.SQL.QueryContext(ctx, `SELECT `+userCols+`,EXISTS(SELECT 1 FROM external_identities e WHERE e.user_id=users.id) FROM users`+where+` ORDER BY created_at,id LIMIT ? OFFSET ?`, query, query, query, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []ports.AdminUser{}
	for rows.Next() {
		var u ports.AdminUser
		var email sql.NullString
		var password, created string
		if err := rows.Scan(&u.ID, &u.DisplayName, &email, &password, &u.Disabled, &u.IsOperator, &created, &u.RoleManaged); err != nil {
			return nil, 0, err
		}
		u.Email = textOf(email)
		u.CreatedAt, _ = parseTime(created)
		out = append(out, u)
	}
	return out, total, rows.Err()
}

func (db *DB) AdminUpdateUser(ctx context.Context, actorID, id string, change ports.AdminChange, corr string) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE users SET is_operator=is_operator WHERE id=?`, actorID); err != nil {
		return err
	}
	var actorAdmin, actorDisabled bool
	if err = tx.QueryRowContext(ctx, `SELECT is_operator,disabled FROM users WHERE id=?`, actorID).Scan(&actorAdmin, &actorDisabled); err != nil {
		return err
	}
	if !actorAdmin || actorDisabled {
		return domain.Forbidden("administrator access required")
	}
	var disabled, admin, managed bool
	err = tx.QueryRowContext(ctx, `SELECT disabled,is_operator,EXISTS(SELECT 1 FROM external_identities e WHERE e.user_id=users.id) FROM users WHERE id=?`, id).Scan(&disabled, &admin, &managed)
	if err == sql.ErrNoRows {
		return domain.NotFound("account not found")
	}
	if err != nil {
		return err
	}
	if change.IsOperator != nil && managed {
		return domain.Conflict("this account's role is managed by the identity provider")
	}
	if change.Disabled != nil {
		disabled = *change.Disabled
	}
	if change.IsOperator != nil {
		admin = *change.IsOperator
	}
	if actorID == id && (disabled || !admin) {
		return domain.Conflict("you cannot disable or demote your own account")
	}
	if disabled || !admin {
		// A durable local administrator cannot be replaced by a temporary IdP grant.
		condition := ""
		if !managed {
			condition = ` AND NOT EXISTS(SELECT 1 FROM external_identities e WHERE e.user_id=users.id)`
		}
		var remaining int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE disabled=0 AND is_operator=1 AND id<>?`+condition, id).Scan(&remaining); err != nil {
			return err
		}
		var wasAdmin bool
		if err = tx.QueryRowContext(ctx, `SELECT is_operator AND NOT disabled FROM users WHERE id=?`, id).Scan(&wasAdmin); err != nil {
			return err
		}
		if wasAdmin && remaining == 0 {
			return domain.Conflict("the last enabled administrator must remain active")
		}
	}
	var oldDisabled, oldAdmin bool
	if err = tx.QueryRowContext(ctx, `SELECT disabled,is_operator FROM users WHERE id=?`, id).Scan(&oldDisabled, &oldAdmin); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE users SET disabled=?,is_operator=? WHERE id=?`, b2i(disabled), b2i(admin), id); err != nil {
		return err
	}
	if disabled || oldAdmin != admin || change.RevokeSessions {
		if _, err = tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, nowText(), id); err != nil {
			return err
		}
	}
	if disabled {
		if _, err = tx.ExecContext(ctx, `UPDATE api_keys SET revoked_at=? WHERE principal_id=? AND revoked_at IS NULL`, nowText(), id); err != nil {
			return err
		}
	}
	details, _ := json.Marshal(map[string]any{"disabled": disabled, "isOperator": admin, "previousDisabled": oldDisabled, "previousIsOperator": oldAdmin, "revokeSessions": change.RevokeSessions})
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(id,actor,action,resource,outcome,correlation,details,created_at) VALUES(?,?,?,?,?,?,?,?)`, domain.NewID("aud"), actorID, "admin.user.update", id, "success", corr, string(details), nowText()); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) AdminAudit(ctx context.Context, offset int) ([]ports.AuditEvent, error) {
	rows, err := db.SQL.QueryContext(ctx, `SELECT id,actor,action,resource,outcome,correlation,details,created_at FROM audit_events WHERE action LIKE 'admin.%' OR action LIKE 'oidc.%' OR action LIKE 'password.%' OR action LIKE 'totp.%' ORDER BY created_at DESC,id DESC LIMIT 50 OFFSET ?`, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.AuditEvent{}
	for rows.Next() {
		var a ports.AuditEvent
		var created string
		if err := rows.Scan(&a.ID, &a.Actor, &a.Action, &a.Resource, &a.Outcome, &a.Corr, &a.Details, &created); err != nil {
			return nil, err
		}
		a.CreatedAt, _ = parseTime(created)
		out = append(out, a)
	}
	return out, rows.Err()
}
