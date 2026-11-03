// Package robotlab runs Robot Lab: physics scenarios, reinforcement-learning
// experiments and live robot control, served by an embedded Python sidecar.
//
// The sidecar protocol and JSON shapes are fixed by the Robot Lab contract;
// this file holds the seams shared by the service (process lifecycle, client
// API, events) and the model tool (agent actions, vision loop).
package robotlab

import (
	"context"
	"encoding/json"
	"os"
	"strings"
)

// Sidecar is a running Robot Lab runtime. Call sends one request and waits for
// its result; Events delivers sidecar events (frame, metrics, experiment, log,
// teleop) until the sidecar stops. Implementations start the process lazily
// and restart it after a crash.
type Sidecar interface {
	Call(ctx context.Context, method string, params any) (json.RawMessage, error)
	Subscribe(fn func(Event)) (unsubscribe func())
	Status(ctx context.Context) Status
}

// Event is one sidecar event, forwarded to the bus as "robotlab.<Name>".
type Event struct {
	Name string          `json:"event"`
	Data json.RawMessage `json:"data"`
}

// Status reports whether Robot Lab can run on this host.
type Status struct {
	Enabled      bool         `json:"enabled"`
	Installed    bool         `json:"installed"`
	Python       string       `json:"python"`
	Version      string       `json:"version,omitempty"`
	Running      bool         `json:"running"`
	Error        string       `json:"error,omitempty"`
	Requirements Requirements `json:"requirements"`
}

// Requirements lists host prerequisites shown on the install card.
type Requirements struct {
	Python string `json:"python"`
	OSMesa bool   `json:"osmesa"`
}

// Publisher emits client events (robotlab.frame, robotlab.vision, robotlab.focus, ...).
type Publisher interface {
	Publish(event string, payload any)
}

// VisionAsker sends one image plus a prompt to the configured vision model and
// returns its text answer. The daemon binds it to the vision models gateway.
type VisionAsker interface {
	AskImage(ctx context.Context, prompt string, pngBase64 string) (answer string, model string, err error)
}

// Enabled reports the EXCALIBUR_ROBOTLAB switch (default on).
func Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("EXCALIBUR_ROBOTLAB"))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}
