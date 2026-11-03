package daemon

import (
	"context"
	"errors"
	"github.com/CristhianRudolf7/excalibur-agent/internal/memory"
	"log/slog"
	"os"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/platform"
	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

type principalStore interface {
	store.Writer
	store.ReadPool
}

// PrincipalService reads the local identity over the daemon's existing database.
type PrincipalService struct {
	db      principalStore
	enabled bool
}

// NewPrincipalService seeds an empty table best effort through one porter command.
// The error result is retained for compatibility; initialization failures are
// logged and never prevent unrelated daemon services from starting.
func NewPrincipalService(ctx context.Context, db principalStore) (*PrincipalService, error) {
	return newPrincipalService(ctx, db, platform.CurrentUserName, slog.Default())
}

func newPrincipalService(ctx context.Context, db principalStore, lookup func() (string, error), logger *slog.Logger) (*PrincipalService, error) {
	s := &PrincipalService{db: db, enabled: os.Getenv("EXCALIBUR_PRINCIPAL_ENABLED") != "0" && !strings.EqualFold(os.Getenv("EXCALIBUR_PRINCIPAL_ENABLED"), "false")}
	if !s.enabled {
		return s, nil
	}
	if db == nil {
		logger.Warn("local profile store unavailable; continuing daemon startup")
		return s, nil
	}
	// Resolve host defaults outside the write transaction, and only for an empty table.
	var count int
	if err := db.ReadInteractive(ctx, func(ctx context.Context, q store.Queryer) error {
		return q.QueryRow(ctx, `SELECT count(*) FROM principals`).Scan(&count)
	}); err != nil {
		logger.Warn("could not inspect local profile; continuing daemon startup", "error", err)
		return s, nil
	}
	if count != 0 {
		return s, nil
	}
	name, _ := principalName(os.Getenv, lookup)
	zone := "UTC"
	language := principalLanguage(os.Getenv)
	// The conditional insert remains atomic even when another start seeded after the read.
	err := db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		_, err := ex.Exec(ctx, `INSERT INTO principals(id,name,timezone,language) SELECT 'local',?,?,? WHERE NOT EXISTS (SELECT 1 FROM principals) ON CONFLICT DO NOTHING`, name, zone, language)
		return err
	})
	if err != nil {
		logger.Warn("could not seed local profile; continuing daemon startup", "error", err)
	}
	return s, nil
}

func principalName(env func(string) string, lookup func() (string, error)) (string, error) {
	if name := strings.TrimSpace(env("EXCALIBUR_USER_NAME")); name != "" {
		return name, nil
	}
	if name, err := lookup(); err == nil && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name), nil
	}
	for _, key := range []string{"USER", "LOGNAME"} {
		if name := strings.TrimSpace(env(key)); name != "" {
			return name, nil
		}
	}
	return "User", nil
}
func principalLanguage(env func(string) string) string {
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if locale := strings.ToLower(env(key)); locale != "" {
			if locale == "es" || strings.HasPrefix(locale, "es_") || strings.HasPrefix(locale, "es-") || strings.HasPrefix(locale, "es.") {
				return "es"
			}
			return "en"
		}
	}
	return "en"
}

// Get never edits a principal or assigns principal IDs to existing records.
func (s *PrincipalService) Get(ctx context.Context) (api.PrincipalGetResult, error) {
	var result api.PrincipalGetResult
	if s == nil {
		return result, &api.Error{Code: api.CodeInternal, Message: "could not read local profile"}
	}
	if !s.enabled {
		return result, &api.Error{Code: api.CodeMethodNotFound, Message: "principal profile is disabled"}
	}
	if s.db == nil {
		return result, &api.Error{Code: api.CodeInternal, Message: "could not read local profile"}
	}
	p, err := memory.NewPrincipalService(s.db).Get(ctx)
	if err != nil {
		return result, &api.Error{Code: api.CodeInternal, Message: "could not read local profile"}
	}
	return principalResult(p), nil
}

func principalResult(p memory.Principal) api.PrincipalGetResult {
	return api.PrincipalGetResult{ID: p.ID, Name: p.Name, Timezone: p.Timezone, Language: p.Language, TimezoneSource: p.TimezoneSource}
}

// Update applies validated preferences; startup reports cannot replace manual zones.
func (s *PrincipalService) Update(ctx context.Context, params api.PrincipalUpdateParams) (api.PrincipalGetResult, error) {
	if s == nil || s.db == nil || !s.enabled {
		_, err := s.Get(ctx)
		return api.PrincipalGetResult{}, err
	}
	p, err := memory.NewPrincipalService(s.db).Update(ctx, memory.PrincipalTimezoneUpdate{Timezone: params.Timezone, TimezoneSource: params.TimezoneSource, EnableAuto: params.EnableAuto})
	if errors.Is(err, memory.ErrInvalidPrincipalTimezone) {
		return api.PrincipalGetResult{}, &api.Error{Code: api.CodeInvalidParams, Message: err.Error()}
	}
	if err != nil {
		return api.PrincipalGetResult{}, &api.Error{Code: api.CodeInternal, Message: "could not update local profile"}
	}
	return principalResult(p), nil
}
