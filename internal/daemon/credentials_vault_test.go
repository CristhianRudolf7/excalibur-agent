package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/gateway"
	"github.com/CristhianRudolf7/excalibur-agent/internal/telemetry"
	"github.com/CristhianRudolf7/excalibur-agent/internal/types"
	"github.com/CristhianRudolf7/excalibur-agent/internal/vault"
)

func credentialTestRegistry(t *testing.T) (*CredentialRegistry, *vault.Vault, string) {
	t.Helper()
	v, pf := openTestVault(t)
	r := newCredentialRegistry(telemetry.NewBus(nil))
	r.vault = v
	t.Cleanup(r.Close)
	return r, v, filepath.Dir(pf)
}
func credentialEntry(ref string) string {
	return strings.ReplaceAll(strings.TrimPrefix(ref, "secret://"), ".", "-")
}
func credentialTestValues() map[string][]byte {
	return map[string][]byte{"username": []byte("credential-user-sentinel"), "password": []byte("credential-password-sentinel")}
}

func TestCredentialSubmitRememberReuseAndHostPolicy(t *testing.T) {
	r, v, dir := credentialTestRegistry(t)
	ctx := context.Background()
	event, result := startCredentialRequest(t, r, ctx, credentialArgs{Site: "example.com", Purpose: "Sign in", RememberDefault: true})
	values := credentialTestValues()
	user, password := values["username"], values["password"]
	if err := r.Submit(ctx, event.RequestID, values, true); err != nil {
		t.Fatal(err)
	}
	out := <-result
	if out.Status != "provided" || len(out.Refs) != 2 {
		t.Fatalf("result: %+v", out)
	}
	if len(values) != 0 || strings.Trim(string(user), "\x00") != "" || strings.Trim(string(password), "\x00") != "" {
		t.Fatal("owned values not zeroed")
	}
	for field, ref := range out.Refs {
		if !strings.HasSuffix(ref, "."+field) {
			t.Fatal("field reference missing")
		}
		name := credentialEntry(ref)
		md, err := v.Describe(ctx, name)
		if err != nil {
			t.Fatal(err)
		}
		if len(md.Domains) != 1 || md.Domains[0] != "example.com" || len(md.AllowedTools) != 1 || md.AllowedTools[0] != "browser_act" || md.ExpiresAt != nil {
			t.Fatalf("policy: %+v", md)
		}
		for _, host := range []string{"example.com.evil.test", "sub.example.com", "evil.test"} {
			if _, err := v.CheckFormPolicy(ctx, name, "browser_act", "run", "", host); !errors.Is(err, vault.ErrDenied) {
				t.Errorf("wrong host %q accepted: %v", host, err)
			}
		}
		resolved, err := v.CheckFormPolicy(ctx, name, "browser_act", "run", "", "example.com")
		if err != nil {
			t.Fatal(err)
		}
		if string(resolved.Value) != "credential-"+map[string]string{"username": "user", "password": "password"}[field]+"-sentinel" {
			t.Fatal("wrong field value")
		}
		vault.ZeroBytes(resolved.Value)
		if _, err := v.Resolve(ctx, vault.ResolveRequest{Ref: name, Tool: "exec", Dest: vault.DestEnv}); !errors.Is(err, vault.ErrDenied) {
			t.Fatal("credential available to exec")
		}
	}
	before := r.bus.Len()
	origin := context.WithValue(ctx, credentialOriginKey{}, credentialOrigin{conversation: "conversation", run: "next-run"})
	stored, err := r.request(origin, credentialArgs{Site: "EXAMPLE.COM"})
	if err != nil || stored.Status != "stored" || len(stored.Refs) != 2 {
		t.Fatalf("reuse: %+v %v", stored, err)
	}
	if r.bus.Len() != before || len(r.Pending("conversation").Requests) != 0 {
		t.Fatal("remembered reuse asked again")
	}
	r.EndRun("run")
	for _, ref := range stored.Refs {
		if _, err := v.Describe(ctx, credentialEntry(ref)); err != nil {
			t.Fatal("remembered entry deleted at run end")
		}
	}
	encoded, _ := json.Marshal([]any{out, stored, r.bus.Replay(types.Cursor{})})
	for _, secret := range []string{"credential-user-sentinel", "credential-password-sentinel"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("value leaked into tool output or events")
		}
		if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || filepath.Base(path) == "passphrase" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(data), secret) {
				t.Errorf("plaintext in %s", path)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCredentialEphemeralCleanupAndOTP(t *testing.T) {
	for _, mode := range []string{"run_end", "expiry", "restart", "close", "otp"} {
		t.Run(mode, func(t *testing.T) {
			r, v, _ := credentialTestRegistry(t)
			ctx := context.Background()
			args := credentialArgs{Site: "example.com"}
			values := credentialTestValues()
			remember := false
			if mode == "otp" {
				args.Fields = []string{"password", "otp"}
				values = map[string][]byte{"password": []byte("remembered-password-sentinel"), "otp": []byte("ephemeral-otp-sentinel")}
				remember = true
			}
			event, result := startCredentialRequest(t, r, ctx, args)
			if err := r.Submit(ctx, event.RequestID, values, remember); err != nil {
				t.Fatal(err)
			}
			out := <-result
			r.EndRun("other-run")
			for field, ref := range out.Refs {
				md, err := v.Describe(ctx, credentialEntry(ref))
				if err != nil {
					t.Fatal("other run deleted credential")
				}
				if field == "otp" || !remember {
					if md.ExpiresAt == nil || time.Until(*md.ExpiresAt) > time.Hour || md.Meta["credential_remembered"] != "false" {
						t.Fatal("missing ephemeral expiry")
					}
				}
			}
			switch mode {
			case "run_end", "otp":
				r.EndRun("run")
			case "expiry":
				r.cleanup("", time.Now().Add(time.Hour+time.Second))
			case "restart":
				if err := r.recoverEphemeral(ctx); err != nil {
					t.Fatal(err)
				}
			case "close":
				r.Close()
			}
			for field, ref := range out.Refs {
				_, err := v.Describe(ctx, credentialEntry(ref))
				if mode == "otp" && field == "password" {
					if err != nil {
						t.Fatal("remembered password deleted")
					}
				} else if !errors.Is(err, vault.ErrNotFound) {
					t.Errorf("ephemeral %s retained: %v", field, err)
				}
			}
		})
	}
}

func TestCredentialSubmitLockedValidationRetryAndZero(t *testing.T) {
	r, v, _ := credentialTestRegistry(t)
	ctx := context.Background()
	event, result := startCredentialRequest(t, r, ctx, credentialArgs{Site: "example.com"})
	v.LockVault()
	values := credentialTestValues()
	owned := values["password"]
	err := r.Submit(ctx, event.RequestID, values, false)
	var rpcErr *api.Error
	if !errors.As(err, &rpcErr) || rpcErr.Message != "vault_locked" || rpcErr.Data.(map[string]string)["code"] != "vault_locked" {
		t.Fatalf("locked error: %v", err)
	}
	if len(values) != 0 || strings.Trim(string(owned), "\x00") != "" {
		t.Fatal("locked path retained values")
	}
	if len(r.Pending("conversation").Requests) != 1 {
		t.Fatal("locked submit consumed request")
	}
	if err := v.UnlockVault(ctx, []byte(testPassphrase)); err != nil {
		t.Fatal(err)
	}
	for _, values := range []map[string][]byte{{"password": []byte("sentinel")}, {"username": []byte("u"), "password": []byte{}}, {"username": []byte("u"), "password": []byte(strings.Repeat("x", 16385))}} {
		if err := r.Submit(ctx, event.RequestID, values, false); err == nil || len(values) != 0 {
			t.Fatal("invalid submit accepted or retained")
		}
	}
	if err := r.Submit(ctx, event.RequestID, credentialTestValues(), false); err != nil {
		t.Fatal(err)
	}
	if (<-result).Status != "provided" {
		t.Fatal("retry did not resume")
	}
	late := credentialTestValues()
	if err := r.Submit(ctx, event.RequestID, late, false); err == nil || len(late) != 0 {
		t.Fatal("late submit accepted or retained")
	}
}

func TestCredentialRPCGateway(t *testing.T) {
	r, _, _ := credentialTestRegistry(t)
	ctx := context.Background()
	backend := NewDaemonBackend(nil, nil, r.bus)
	backend.credentials = r
	gate := gateway.New(gateway.DefaultConfig(), backend, gateway.NewMemoryBlobs(), r.bus)
	addr, err := gate.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gate.Stop(ctx) })
	c := gateway.Dial(addr, "credential-test")
	event, result := startCredentialRequest(t, r, ctx, credentialArgs{Site: "example.com"})
	for i := 0; i < 2; i++ {
		raw, err := c.Call(ctx, "pending", api.MethodCredentialsPending, api.CredentialsPendingParams{ConversationID: "conversation"})
		if err != nil {
			t.Fatal(err)
		}
		var pending api.CredentialsPendingResult
		_ = json.Unmarshal(raw, &pending)
		if len(pending.Requests) != 1 || pending.Requests[0].ID != event.RequestID {
			t.Fatalf("pending: %s", raw)
		}
	}
	raw, err := c.Call(ctx, "submit", api.MethodCredentialsSubmit, api.CredentialsSubmitParams{RequestID: event.RequestID, Values: map[string]string{"username": "credential-user-sentinel", "password": "credential-password-sentinel"}})
	if err != nil || !strings.Contains(string(raw), `"ok":true`) || (<-result).Status != "provided" {
		t.Fatalf("submit: %s %v", raw, err)
	}
	event, result = startCredentialRequest(t, r, ctx, credentialArgs{Site: "example.com"})
	if _, err := c.Call(ctx, "cancel", api.MethodCredentialsCancel, api.CredentialsCancelParams{RequestID: event.RequestID}); err != nil {
		t.Fatal(err)
	}
	if (<-result).Status != "cancelled" {
		t.Fatal("RPC cancellation did not wake tool")
	}
	events, _ := json.Marshal(r.bus.Replay(types.Cursor{}))
	for _, secret := range []string{"credential-user-sentinel", "credential-password-sentinel"} {
		if strings.Contains(string(events), secret) {
			t.Fatal("RPC values reached events")
		}
	}
}

func TestCredentialCancellationAndTimeoutRejectValues(t *testing.T) {
	for _, mode := range []string{"cancel", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			r, v, _ := credentialTestRegistry(t)
			r.wait = 25 * time.Millisecond
			event, result := startCredentialRequest(t, r, context.Background(), credentialArgs{Site: "example.com"})
			if mode == "cancel" {
				if err := r.Cancel(event.RequestID); err != nil {
					t.Fatal(err)
				}
			}
			<-result
			values := credentialTestValues()
			owned := values["password"]
			if err := r.Submit(context.Background(), event.RequestID, values, true); err == nil {
				t.Fatal("resolved request accepted values")
			}
			if len(values) != 0 || strings.Trim(string(owned), "\x00") != "" {
				t.Fatal("rejected values were retained")
			}
			entries, err := v.ListMetadata(context.Background())
			if err != nil || len(entries) != 0 {
				t.Fatal("rejected values stored in vault")
			}
		})
	}
}

func TestCredentialLockedVaultCleanup(t *testing.T) {
	r, v, _ := credentialTestRegistry(t)
	event, result := startCredentialRequest(t, r, context.Background(), credentialArgs{Site: "example.com"})
	if err := r.Submit(context.Background(), event.RequestID, credentialTestValues(), false); err != nil {
		t.Fatal(err)
	}
	out := <-result
	v.LockVault()
	if err := r.recoverEphemeral(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, ref := range out.Refs {
		if _, err := v.Describe(context.Background(), credentialEntry(ref)); !errors.Is(err, vault.ErrNotFound) {
			t.Fatal("locked vault retained orphan")
		}
	}
}

func TestCredentialPendingAllConversationsRPC(t *testing.T) {
	r, _, _ := credentialTestRegistry(t)
	b := NewDaemonBackend(nil, nil, r.bus)
	b.credentials = r
	for _, conv := range []string{"web-conversation", "telegram-conversation"} {
		r.pending[conv] = &credentialWait{request: api.CredentialRequest{ID: conv, ConversationID: conv, Status: "pending"}, done: make(chan struct{})}
	}
	for _, filter := range []string{"", "web-conversation", "telegram-conversation", "unknown"} {
		raw, _ := json.Marshal(api.CredentialsPendingParams{ConversationID: filter})
		out, err := b.Passthrough(context.Background(), api.MethodCredentialsPending, raw)
		if err != nil {
			t.Fatal(err)
		}
		want := 1
		if filter == "" {
			want = 2
		}
		if filter == "unknown" {
			want = 0
		}
		requests := out.(api.CredentialsPendingResult).Requests
		if len(requests) != want {
			t.Fatalf("filter %q: got %d requests", filter, len(requests))
		}
		for _, request := range requests {
			if filter != "" && request.ConversationID != filter {
				t.Fatal("conversation filter ignored")
			}
		}
	}
	for _, raw := range []string{`{}`, `{"conversation_id":null}`, `{"conversation_id":1}`} {
		if _, err := b.Passthrough(context.Background(), api.MethodCredentialsPending, json.RawMessage(raw)); err == nil {
			t.Fatal("missing or invalid filter accepted")
		}
	}
}

func TestVaultMetadataReportsLockState(t *testing.T) {
	v, _ := openTestVault(t)
	for _, locked := range []bool{false, true, false} {
		if locked {
			v.LockVault()
		} else if err := v.UnlockVault(context.Background(), []byte(testPassphrase)); err != nil {
			t.Fatal(err)
		}
		out, err := VaultOp(context.Background(), v, api.MethodVaultListMetadata, json.RawMessage(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		state, ok := out.(map[string]any)["locked"].(bool)
		if !ok || state != locked {
			t.Fatal("metadata omitted or misreported vault lock state")
		}
	}
}
