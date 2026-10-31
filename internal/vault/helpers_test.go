package vault

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// dumpTables returns a textual dump of vault tables for cleartext scans.
func dumpTables(t *testing.T, v *Vault) string {
	t.Helper()
	var b strings.Builder
	write := func(vals ...any) {
		for _, val := range vals {
			switch c := val.(type) {
			case []byte:
				b.Write(c)
				b.WriteByte('|')
			case string:
				b.WriteString(c)
				b.WriteByte('|')
			case int64:
				fmt.Fprintf(&b, "%d|", c)
			case int:
				fmt.Fprintf(&b, "%d|", c)
			case nil:
			default:
				fmt.Fprintf(&b, "%v|", c)
			}
		}
	}
	err := v.db.Read(context.Background(), func(ctx context.Context, q store.Queryer) error {
		rows, err := q.Query(ctx, `SELECT id,name,type,metadata,scopes,allowed_tools,domains,ciphertext,nonce,entry_version,dek_version FROM secrets`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, name, typ string
			var meta, scopes, tools, domains string
			var ct, nonce []byte
			var ev, dv int
			if err := rows.Scan(&id, &name, &typ, &meta, &scopes, &tools, &domains, &ct, &nonce, &ev, &dv); err != nil {
				_ = rows.Close()
				return err
			}
			write(id, name, typ, meta, scopes, tools, domains, ct, nonce, ev, dv)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = q.Query(ctx, `SELECT dek_version,wrapped,wrap_nonce,kek_kind,salt FROM vault_keys`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var dv int
			var wrapped, nonce, salt []byte
			var kind string
			if err := rows.Scan(&dv, &wrapped, &nonce, &kind, &salt); err != nil {
				_ = rows.Close()
				return err
			}
			write(dv, wrapped, nonce, kind, salt)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = q.Query(ctx, `SELECT secret_name,tool,run_id,dest,project,at FROM secret_uses`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var a, c, d, e, f, g string
			if err := rows.Scan(&a, &c, &d, &e, &f, &g); err != nil {
				_ = rows.Close()
				return err
			}
			write(a, c, d, e, f, g)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		rows, err = q.Query(ctx, `SELECT id,created_at,ciphertext,nonce,dek_version,hint FROM drafts`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, created, hint string
			var ct, nonce []byte
			var dv int
			if err := rows.Scan(&id, &created, &ct, &nonce, &dv, &hint); err != nil {
				_ = rows.Close()
				return err
			}
			write(id, created, ct, nonce, dv, hint)
		}
		_ = rows.Close()
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("dump: %v", err)
	}
	return b.String()
}
