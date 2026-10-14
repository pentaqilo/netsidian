package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/pentaqilo/netsidian/internal/domain"
	"github.com/pentaqilo/netsidian/internal/ports"
)

func (db *DB) CreateUser(ctx context.Context, u *domain.User) error {
	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO users(id, display_name, email, password_hash, disabled, is_operator, created_at)
		 VALUES(?,?,?,?,?,?,?)`,
		u.ID, u.DisplayName, nullStr(u.Email), u.PasswordHash, b2i(u.Disabled), b2i(u.IsOperator), u.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func scanUser(row interface{ Scan(...any) error }) (*domain.User, error) {
	var u domain.User
	var email sql.NullString
	var disabled, operator int
	var created string
	if err := row.Scan(&u.ID, &u.DisplayName, &email, &u.PasswordHash, &disabled, &operator, &created); err != nil {
		return nil, err
	}
	u.Email = textOf(email)
	u.Disabled = disabled != 0
	u.IsOperator = operator != 0
	u.CreatedAt, _ = parseTime(created)
	return &u, nil
}

const userCols = `id, display_name, email, password_hash, disabled, is_operator, created_at`

func (db *DB) UserByEmail(ctx context.Context, email string) (*domain.User, error) {
	row := db.SQL.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE lower(email)=lower(?)`, email)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func (db *DB) UserByID(ctx context.Context, id string) (*domain.User, error) {
	row := db.SQL.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id=?`, id)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func (db *DB) UserCount(ctx context.Context) (int, error) {
	var n int
	err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

func (db *DB) SetPassword(ctx context.Context, userID, argonHash string) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=?`, argonHash, userID)
	return err
}

// ChangePassword atomically rotates a local account's password: the new
// Argon2id hash, the revocation of every session and API key of the account,
// and the audit event commit in one transaction. A failure anywhere leaves
// the previous password and credentials fully intact.
func (db *DB) ChangePassword(ctx context.Context, userID, expectedHash, argonHash, corr string) (sessionsRevoked, keysRevoked int, err error) {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE users SET password_hash=? WHERE id=? AND password_hash=? AND password_hash<>'' AND disabled=0 AND NOT EXISTS(SELECT 1 FROM totp_enrollments t WHERE t.user_id=users.id AND t.confirmed=1)`, argonHash, userID, expectedHash)
	if err != nil {
		return 0, 0, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return 0, 0, err
	} else if n == 0 {
		return 0, 0, domain.Conflict("account credentials changed; sign in again")
	}
	res, err = tx.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, nowText(), userID)
	if err != nil {
		return 0, 0, err
	}
	nSessions, err := res.RowsAffected()
	if err != nil {
		return 0, 0, err
	}
	sessionsRevoked = int(nSessions)
	res, err = tx.ExecContext(ctx, `UPDATE api_keys SET revoked_at=? WHERE principal_id=? AND revoked_at IS NULL`, nowText(), userID)
	if err != nil {
		return 0, 0, err
	}
	nKeys, err := res.RowsAffected()
	if err != nil {
		return 0, 0, err
	}
	keysRevoked = int(nKeys)
	details, _ := json.Marshal(map[string]any{"sessionsRevoked": sessionsRevoked, "keysRevoked": keysRevoked})
	if _, err = tx.ExecContext(ctx, `INSERT INTO audit_events(id,actor,action,resource,outcome,correlation,details,created_at) VALUES(?,?,?,?,?,?,?,?)`,
		domain.NewID("aud"), userID, "password.change", userID, "success", corr, string(details), nowText()); err != nil {
		return 0, 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, 0, err
	}
	return sessionsRevoked, keysRevoked, nil
}

// SolePrincipal returns the single account used in no-auth mode.
func (db *DB) SolePrincipal(ctx context.Context) (*domain.User, error) {
	row := db.SQL.QueryRowContext(ctx, `SELECT `+userCols+` FROM users ORDER BY created_at LIMIT 1`)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func (db *DB) CreateSession(ctx context.Context, id, userID, tokenHash string, expiresAt time.Time) error {
	result, err := db.SQL.ExecContext(ctx,
		`INSERT INTO sessions(id,user_id,token_hash,expires_at,created_at) SELECT ?,?,?,?,? FROM users WHERE id=? AND disabled=0`,
		id, userID, tokenHash, expiresAt.UTC().Format(time.RFC3339Nano), nowText(), userID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return domain.Unauthenticated("account is unavailable")
	}
	return nil
}

func (db *DB) CreatePasswordSession(ctx context.Context, id, userID, expectedHash, tokenHash string, expiresAt time.Time) error {
	exp := expiresAt.UTC().Format(time.RFC3339Nano)
	res, err := db.SQL.ExecContext(ctx, `INSERT INTO sessions(id,user_id,token_hash,expires_at,created_at)
		SELECT ?,?,?,?,? FROM users WHERE id=? AND disabled=0 AND password_hash=? AND password_hash<>'' AND NOT EXISTS(SELECT 1 FROM totp_enrollments t WHERE t.user_id=users.id AND t.confirmed=1)`,
		id, userID, tokenHash, exp, nowText(), userID, expectedHash)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return domain.Unauthenticated("account credentials changed; sign in again")
	}
	return nil
}

func (db *DB) SessionByTokenHash(ctx context.Context, tokenHash string) (string, time.Time, bool, error) {
	var userID, expires string
	var revoked sql.NullString
	err := db.SQL.QueryRowContext(ctx,
		`SELECT user_id, expires_at, revoked_at FROM sessions WHERE token_hash=?`, tokenHash,
	).Scan(&userID, &expires, &revoked)
	if err == sql.ErrNoRows {
		return "", time.Time{}, false, nil
	}
	if err != nil {
		return "", time.Time{}, false, err
	}
	exp, perr := parseTime(expires)
	if perr != nil {
		return "", time.Time{}, false, perr
	}
	if revoked.Valid || time.Now().After(exp) {
		return userID, exp, false, nil
	}
	return userID, exp, true, nil
}

func (db *DB) RevokeSession(ctx context.Context, id string) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, nowText(), id)
	return err
}

func (db *DB) RevokeSessionByHash(ctx context.Context, tokenHash string) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE token_hash=? AND revoked_at IS NULL`, nowText(), tokenHash)
	return err
}

func (db *DB) RevokeUserSessions(ctx context.Context, userID string) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE sessions SET revoked_at=? WHERE user_id=? AND revoked_at IS NULL`, nowText(), userID)
	return err
}

// ---------- API keys ----------

func (db *DB) CreateKey(ctx context.Context, k *ports.APIKeyRecord) error {
	scopes, _ := json.Marshal(k.Scopes)
	spaces, _ := json.Marshal(k.SpaceIDs)
	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO api_keys(id,label,principal_id,secret_hash,scopes,space_ids,expires_at,last_used_at,revoked_at,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		k.ID, k.Label, k.PrincipalID, k.SecretHash, string(scopes), string(spaces),
		fmtTime(k.ExpiresAt), fmtTime(k.LastUsedAt), fmtTime(k.RevokedAt), k.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func scanKey(row interface{ Scan(...any) error }) (*ports.APIKeyRecord, error) {
	var k ports.APIKeyRecord
	var scopes, spaces string
	var expires, lastUsed, revoked sql.NullString
	var created string
	if err := row.Scan(&k.ID, &k.Label, &k.PrincipalID, &k.SecretHash, &scopes, &spaces,
		&expires, &lastUsed, &revoked, &created); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(scopes), &k.Scopes)
	_ = json.Unmarshal([]byte(spaces), &k.SpaceIDs)
	k.ExpiresAt = scanNullTime(expires)
	k.LastUsedAt = scanNullTime(lastUsed)
	k.RevokedAt = scanNullTime(revoked)
	k.CreatedAt, _ = parseTime(created)
	return &k, nil
}

const keyCols = `id, label, principal_id, secret_hash, scopes, space_ids, expires_at, last_used_at, revoked_at, created_at`

func (db *DB) ListKeys(ctx context.Context, userID string) ([]*ports.APIKeyRecord, error) {
	rows, err := db.SQL.QueryContext(ctx, `SELECT `+keyCols+` FROM api_keys WHERE principal_id=? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ports.APIKeyRecord
	for rows.Next() {
		k, err := scanKey(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (db *DB) KeyByID(ctx context.Context, id string) (*ports.APIKeyRecord, error) {
	row := db.SQL.QueryRowContext(ctx, `SELECT `+keyCols+` FROM api_keys WHERE id=?`, id)
	k, err := scanKey(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return k, err
}

// KeyBySecretHash resolves an authenticating bearer key.
func (db *DB) KeyBySecretHash(ctx context.Context, hash string) (*ports.APIKeyRecord, error) {
	row := db.SQL.QueryRowContext(ctx, `SELECT `+keyCols+` FROM api_keys WHERE secret_hash=?`, hash)
	k, err := scanKey(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return k, err
}

func (db *DB) RevokeKey(ctx context.Context, id string) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE api_keys SET revoked_at=? WHERE id=? AND revoked_at IS NULL`, nowText(), id)
	return err
}

func (db *DB) TouchKey(ctx context.Context, id string, at time.Time) error {
	_, err := db.SQL.ExecContext(ctx, `UPDATE api_keys SET last_used_at=? WHERE id=? AND (last_used_at IS NULL OR last_used_at<?)`, at.UTC().Format(time.RFC3339Nano), id, at.Add(-time.Minute).UTC().Format(time.RFC3339Nano))
	return err
}

// ---------- Preferences ----------

func (db *DB) Preferences(ctx context.Context, userID string) (map[string]any, error) {
	var raw string
	err := db.SQL.QueryRowContext(ctx, `SELECT prefs FROM user_preferences WHERE user_id=?`, userID).Scan(&raw)
	if err == sql.ErrNoRows {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]any{}
	_ = json.Unmarshal([]byte(raw), &out)
	return out, nil
}

func (db *DB) SetPreferences(ctx context.Context, userID string, prefs map[string]any) error {
	raw, _ := json.Marshal(prefs)
	_, err := db.SQL.ExecContext(ctx,
		`INSERT INTO user_preferences(user_id,prefs,updated_at) VALUES(?,?,?)
		 ON CONFLICT(user_id) DO UPDATE SET prefs=excluded.prefs, updated_at=excluded.updated_at`,
		userID, string(raw), nowText())
	return err
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func fmtTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

// ExternalUser looks up the stable issuer/subject identity. Email addresses
// never implicitly link an external login to an existing local account.
func (db *DB) ExternalUser(ctx context.Context, issuer, subject string) (*domain.User, error) {
	row := db.SQL.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id=(SELECT user_id FROM external_identities WHERE issuer=? AND subject=?)`, issuer, subject)
	u, err := scanUser(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func (db *DB) CreateExternalUser(ctx context.Context, issuer, subject string, u *domain.User) error {
	tx, err := db.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, `INSERT INTO users(id,display_name,email,password_hash,disabled,is_operator,created_at) VALUES(?,?,?,?,?,?,?)`, u.ID, u.DisplayName, nullStr(u.Email), "", 0, 0, u.CreatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO external_identities(issuer,subject,user_id) VALUES(?,?,?)`, issuer, subject, u.ID); err != nil {
		return err
	}
	return tx.Commit()
}
