package capabilities

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// Vision capability (Section 14.4): vision_extract over images or
// document pages with text/layout/describe modes. The model choice,
// resolution and crop live in the vision role config; the harness owns
// the contract, per-page provenance and the reported model.

// VisionManifest publishes vision_extract.
func VisionManifest() Manifest {
	return Manifest{
		ID: "vision", Version: "1",
		Operations: []Operation{
			Op("vision_extract", "Send images or document pages to the vision model and get text, boxes or a description per page", "read_only", "pure", "immediate", false, "media", []string{"vision", "image", "extract", "ocr", "describe"}, ObjSchema([]string{"input"}, map[string]string{"input": "string", "mode": "string", "pages": "string"})),
		},
		Platforms: []string{"linux"},
		Profiles:  []string{"default", "delegate", "eval"},
	}
}

// VisionFake is the deterministic vision simulation: placeholder
// extraction with input hash and per-page provenance, reporting the
// configured model.
type VisionFake struct {
	mu    sync.Mutex
	model string
	calls int
}

// NewVisionFake returns a vision simulation reporting model.
func NewVisionFake(model string) *VisionFake {
	if model == "" {
		model = "vision-fake-1"
	}
	return &VisionFake{model: model}
}

// Manifest implements Capability.
func (v *VisionFake) Manifest() Manifest { return VisionManifest() }

// Invoke implements Capability.
func (v *VisionFake) Invoke(operation string, args json.RawMessage, _ string) (TaskResult, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.calls++
	if operation != "vision_extract" {
		return Failed("", fmt.Sprintf("vision: unknown operation %q", operation), ErrInput), nil
	}
	mode := argString(args, "mode")
	if mode == "" {
		mode = "text"
	}
	switch mode {
	case "text", "layout", "describe":
	default:
		return Failed("", fmt.Sprintf("vision: unknown mode %q (want text, layout or describe)", mode), ErrInput), nil
	}
	input := argString(args, "input")
	if input == "" {
		return Failed("", "vision: input is required", ErrInput), nil
	}
	pages := []map[string]any{
		{"page": 1, "text": "extracted text of " + input, "model": v.model, "source": input},
	}
	if mode == "layout" {
		pages[0]["boxes"] = []string{"r1:(0,0,100,20)"}
	}
	if mode == "describe" {
		pages[0]["description"] = "a placeholder description of " + input
	}
	return Completed("vision-1", map[string]any{"model": v.model, "pages": pages}), nil
}

// Poll implements Capability.
func (v *VisionFake) Poll(taskID string) (TaskResult, error) {
	return Completed(taskID, map[string]any{"ok": true}), nil
}

// Cancel implements Capability.
func (v *VisionFake) Cancel(taskID string) (TaskResult, error) {
	return TaskResult{TaskID: taskID, State: StateCancelled}, nil
}

// Update implements Capability.
func (v *VisionFake) Update(taskID string, _ json.RawMessage) (TaskResult, error) {
	return v.Poll(taskID)
}

// Voice capability (Section 14.5). stt_stream and tts_stream belong to
// the voice channel, not to the agent; the agent tools are
// audio_transcribe (files) and the meeting transcription behind the
// meeting tool. The simulation enforces the consent rule: meeting
// audio without recording consent is never transcribed.

// VoiceManifest publishes the voice operations.
func VoiceManifest() Manifest {
	return Manifest{
		ID: "voice", Version: "1",
		Operations: []Operation{
			Op("audio_transcribe", "Transcribe an audio or video file with timestamps and speakers", "read_only", "pure", "immediate", false, "media", []string{"audio", "transcribe", "speech", "subtitles"}, ObjSchema([]string{"path"}, map[string]string{"path": "string", "language": "string"})),
			Op("meeting_transcribe", "Transcribe meeting audio into speaker segments once recording consent is confirmed", "read_only", "keyed", "cooperative", false, "media", []string{"meeting", "transcribe", "consent", "speakers"}, ObjSchema([]string{"meeting_id", "consent"}, map[string]string{"meeting_id": "string", "consent": "string"})),
		},
		Platforms: []string{"linux"},
		Profiles:  []string{"default", "delegate", "eval"},
	}
}

// VoiceFake is the deterministic voice simulation.
type VoiceFake struct {
	mu     sync.Mutex
	dedup  KeyDedup
	calls  map[string]int
	joined map[string]bool
}

// NewVoiceFake returns a voice simulation.
func NewVoiceFake() *VoiceFake {
	return &VoiceFake{calls: map[string]int{}, joined: map[string]bool{}}
}

// Manifest implements Capability.
func (v *VoiceFake) Manifest() Manifest { return VoiceManifest() }

// JoinMeeting marks a meeting as joined with consent state for tests.
func (v *VoiceFake) JoinMeeting(id string, consent bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.joined[id] = consent
}

// Invoke implements Capability.
func (v *VoiceFake) Invoke(operation string, args json.RawMessage, key string) (TaskResult, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.calls[operation]++
	switch operation {
	case "audio_transcribe":
		path := argString(args, "path")
		if path == "" {
			return Failed("", "voice: path is required", ErrInput), nil
		}
		return Completed("stt-1", map[string]any{
			"segments": []map[string]any{{"speaker": "S1", "start": "00:00", "text": "transcript of " + path}},
		}), nil
	case "meeting_transcribe":
		id := argString(args, "meeting_id")
		if id == "" {
			return Failed("", "voice: meeting_id is required", ErrInput), nil
		}
		if !v.dedup.First(operation + "|" + key) {
			return Completed("stt-"+id, map[string]any{"meeting_id": id, "cached": true}), nil
		}
		consent := strings.EqualFold(argString(args, "consent"), "yes") || v.joined[id]
		if !consent {
			return Failed("stt-"+id, "voice: recording consent is required before transcribing a meeting", ErrInput), nil
		}
		return Completed("stt-"+id, map[string]any{
			"meeting_id": id,
			"segments":   []map[string]any{{"speaker": "host", "start": "00:01", "text": "welcome"}},
		}), nil
	default:
		return Failed("", fmt.Sprintf("voice: unknown operation %q", operation), ErrInput), nil
	}
}

// Poll implements Capability.
func (v *VoiceFake) Poll(taskID string) (TaskResult, error) {
	return Completed(taskID, map[string]any{"ok": true}), nil
}

// Cancel implements Capability.
func (v *VoiceFake) Cancel(taskID string) (TaskResult, error) {
	return TaskResult{TaskID: taskID, State: StateCancelled}, nil
}

// Update implements Capability.
func (v *VoiceFake) Update(taskID string, _ json.RawMessage) (TaskResult, error) {
	return v.Poll(taskID)
}

// Device capability (Section 14.6): screen capture, accessibility
// tree, input and app windows on the Linux desktop. Each node declares
// which operations it supports; the harness never assumes universal
// control.

// DeviceManifest publishes the device operations.
func DeviceManifest() Manifest {
	return Manifest{
		ID: "device", Version: "1",
		Operations: []Operation{
			Op("device_screen_capture", "Capture the screen or one window of the device", "read_only", "pure", "immediate", false, "device", []string{"device", "screen", "capture", "screenshot"}, ObjSchema(nil, map[string]string{"window": "string"})),
			Op("device_ui_tree", "Read the accessibility tree of the interface when the platform offers it", "read_only", "pure", "immediate", false, "device", []string{"device", "ui", "tree", "accessibility"}, ObjSchema(nil, map[string]string{"window": "string"})),
			Op("device_input", "Click, type and send keys or shortcuts on the device", "exec", "keyed", "cooperative", false, "device", []string{"device", "input", "click", "type", "keys"}, ObjSchema([]string{"action"}, map[string]string{"action": "string", "ref": "string", "text": "string"})),
			Op("device_app", "Launch, focus and list applications and windows", "exec", "keyed", "cooperative", false, "device", []string{"device", "app", "launch", "focus", "windows"}, ObjSchema([]string{"action"}, map[string]string{"action": "string", "app": "string"})),
		},
		Platforms: []string{"linux"},
		Profiles:  []string{"default", "delegate", "eval"},
	}
}

// DeviceFake is the deterministic device simulation. Supported lists
// the operations this node offers; unsupported ones fail with an
// explicit input error instead of hanging.
type DeviceFake struct {
	mu        sync.Mutex
	supported map[string]bool
	dedup     KeyDedup
	calls     map[string]int
	effects   map[string]int
}

// NewDeviceFake returns a device simulation supporting every operation;
// Restrict limits it to the named operations.
func NewDeviceFake(supported ...string) *DeviceFake {
	d := &DeviceFake{supported: map[string]bool{}, calls: map[string]int{}, effects: map[string]int{}}
	if len(supported) == 0 {
		for _, op := range DeviceManifest().Operations {
			d.supported[op.Name] = true
		}
		return d
	}
	for _, op := range supported {
		d.supported[op] = true
	}
	return d
}

// Manifest implements Capability.
func (d *DeviceFake) Manifest() Manifest { return DeviceManifest() }

// Effects counts first-effect executions per operation.
func (d *DeviceFake) Effects(op string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.effects[op]
}

// Invoke implements Capability.
func (d *DeviceFake) Invoke(operation string, args json.RawMessage, key string) (TaskResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls[operation]++
	if !d.supported[operation] {
		return Failed("", fmt.Sprintf("device: operation %q is not supported by this node", operation), ErrInput), nil
	}
	switch operation {
	case "device_screen_capture":
		d.seq("device_screen_capture")
		return Completed("shot-1", map[string]any{"capture_ref": "blob:device-shot-1"}), nil
	case "device_ui_tree":
		return Completed("uitree-1", map[string]any{"nodes": []map[string]any{{"role": "window", "name": "desktop"}}}), nil
	case "device_input", "device_app":
		if d.dedup.First(operation + "|" + key) {
			d.effects[operation]++
		}
		action := argString(args, "action")
		if action == "" {
			action = "list"
		}
		return Completed("dev-1", map[string]any{"action": action, "ok": true}), nil
	default:
		return Failed("", fmt.Sprintf("device: unknown operation %q", operation), ErrInput), nil
	}
}

func (d *DeviceFake) seq(_ string) {}

// Poll implements Capability.
func (d *DeviceFake) Poll(taskID string) (TaskResult, error) {
	return Completed(taskID, map[string]any{"ok": true}), nil
}

// Cancel implements Capability.
func (d *DeviceFake) Cancel(taskID string) (TaskResult, error) {
	return TaskResult{TaskID: taskID, State: StateCancelled}, nil
}

// Update implements Capability.
func (d *DeviceFake) Update(taskID string, _ json.RawMessage) (TaskResult, error) {
	return d.Poll(taskID)
}
