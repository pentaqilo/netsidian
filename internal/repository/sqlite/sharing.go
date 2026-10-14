package sqlite

import (
	"context"
	"github.com/pentaqilo/netsidian/internal/ports"
)

func (db *DB) SharingUsers(ctx context.Context, query string) ([]ports.SharingUser, error) {
	rows, err := db.SQL.QueryContext(ctx, `SELECT id,display_name,coalesce(email,'') FROM users WHERE disabled=0 AND (instr(lower(display_name),lower(?))>0 OR instr(lower(coalesce(email,'')),lower(?))>0) ORDER BY lower(display_name),id LIMIT 20`, query, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ports.SharingUser{}
	for rows.Next() {
		var u ports.SharingUser
		if err := rows.Scan(&u.ID, &u.DisplayName, &u.Email); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}
