package memory

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/store"
)

// Principal is the persisted local profile, shared by chat and temporal intake.
type Principal struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Timezone       string `json:"timezone"`
	Language       string `json:"language"`
	TimezoneSource string `json:"timezone_source"`
}

// PrincipalTimezoneUpdate changes only the profile's zone preferences.
type PrincipalTimezoneUpdate struct {
	Timezone       string `json:"timezone"`
	TimezoneSource string `json:"timezone_source"`
	// EnableAuto is explicit user consent to leave manual mode. Startup detection
	// must omit it, so a late browser report cannot replace a manual selection.
	EnableAuto bool `json:"enable_auto,omitempty"`
}

var ErrInvalidPrincipalTimezone = errors.New("invalid principal timezone")

func (p PrincipalTimezoneUpdate) Validate() error {
	if p.Timezone == "" || p.Timezone == "Local" {
		return fmt.Errorf("%w: an IANA name is required", ErrInvalidPrincipalTimezone)
	}
	if _, err := time.LoadLocation(p.Timezone); err != nil {
		return fmt.Errorf("%w: unknown IANA name", ErrInvalidPrincipalTimezone)
	}
	switch p.TimezoneSource {
	case "default", "auto", "manual":
	default:
		return fmt.Errorf("%w: source must be default, auto or manual", ErrInvalidPrincipalTimezone)
	}
	if p.EnableAuto && p.TimezoneSource != "auto" {
		return fmt.Errorf("%w: enable_auto requires source auto", ErrInvalidPrincipalTimezone)
	}
	return nil
}

type PrincipalStore interface {
	store.Writer
	store.ReadPool
}

// PrincipalService uses the existing principal row, never a second profile.
type PrincipalService struct{ db PrincipalStore }

func NewPrincipalService(db PrincipalStore) *PrincipalService { return &PrincipalService{db: db} }

func (s *PrincipalService) Get(ctx context.Context) (Principal, error) {
	var p Principal
	err := s.db.ReadInteractive(ctx, func(ctx context.Context, q store.Queryer) error {
		return q.QueryRow(ctx, `SELECT id,name,timezone,language,timezone_source FROM principals ORDER BY (id='local') DESC,id LIMIT 1`).Scan(&p.ID, &p.Name, &p.Timezone, &p.Language, &p.TimezoneSource)
	})
	return p, err
}

func (s *PrincipalService) Update(ctx context.Context, p PrincipalTimezoneUpdate) (Principal, error) {
	if err := p.Validate(); err != nil {
		return Principal{}, err
	}
	err := s.db.Write(ctx, store.PriorityInteractive, func(ctx context.Context, ex store.Execer) error {
		// Test and update inside one porter transaction, including concurrent tabs.
		_, err := ex.Exec(ctx, `UPDATE principals SET timezone=?,timezone_source=?,updated_at=? WHERE id=(SELECT id FROM principals ORDER BY (id='local') DESC,id LIMIT 1) AND (? != 'auto' OR timezone_source != 'manual' OR ?)`, p.Timezone, p.TimezoneSource, time.Now().UnixMilli(), p.TimezoneSource, p.EnableAuto)
		return err
	})
	if err != nil {
		return Principal{}, err
	}
	return s.Get(ctx)
}
