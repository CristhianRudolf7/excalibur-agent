package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"strings"
)

type deliveryDataEvidence struct {
	Check  string `json:"check"`
	Report string `json:"report"`
}

type deliveryVerificationStorage struct {
	Check   string `json:"check"`
	Storage string `json:"storage"`
}

type deliveryDataReport struct {
	DeliveryStorage      string                        `json:"delivery_storage"`
	SeedFingerprint      string                        `json:"seed_fingerprint"`
	DeliveredFingerprint string                        `json:"delivered_fingerprint"`
	Verification         []deliveryVerificationStorage `json:"verification"`
	Dashboard            deliveryDashboardQuality      `json:"dashboard"`
}

var dataFingerprint = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Storage identifiers are credential-free, canonical identities supplied by the
// application's verifier. The daemon checks receipts and proof structure; the
// verifier must actually isolate checks and hash the complete persisted seed.
func (e *deliveryEvidence) checkDataDelivery(root *os.Root, manifest deliveryManifest, sources []deliverySource) []string {
	evidence := manifest.DataDelivery
	if !e.passed[evidence.Check] {
		return []string{"data_delivery.check must successfully reset/reseed delivery data or prove it equals the clean seed after verification"}
	}
	if !fs.ValidPath(evidence.Report) {
		return []string{"data_delivery.report must name a generated JSON report inside the workspace"}
	}
	file, err := root.Open(evidence.Report)
	if err != nil {
		return []string{"data delivery report unavailable"}
	}
	defer file.Close()
	var report deliveryDataReport
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&report); err != nil || decoder.Decode(new(any)) != io.EOF {
		return []string{"invalid data delivery report"}
	}
	var pending []string
	add := func(format string, args ...any) { pending = append(pending, fmt.Sprintf(format, args...)) }
	checked := e.checkedAt[evidence.Check]
	info, err := file.Stat()
	if err != nil || info.ModTime().After(checked) {
		add("data delivery report changed after its check; regenerate and rerun %s", evidence.Check)
	}
	for _, source := range sources {
		if source.changed.After(checked) || err == nil && source.changed.After(info.ModTime()) {
			add("%s:1: source changed after clean seed proof; rerun %s", source.path, evidence.Check)
		}
	}
	if strings.TrimSpace(report.DeliveryStorage) == "" {
		add("data delivery report needs a credential-free canonical delivery_storage identity")
	}
	if !dataFingerprint.MatchString(report.SeedFingerprint) || report.DeliveredFingerprint != report.SeedFingerprint {
		add("delivered data must equal the clean seed: matching SHA-256 seed_fingerprint and delivered_fingerprint required")
	}
	checks := map[string]bool{}
	for _, verification := range report.Verification {
		if checks[verification.Check] || strings.TrimSpace(verification.Storage) == "" ||
			strings.TrimSpace(verification.Storage) == strings.TrimSpace(report.DeliveryStorage) {
			add("verification needs unique checks and isolated storage distinct from delivery_storage")
		}
		checks[verification.Check] = true
		if !e.passed[verification.Check] || !e.checkedAt[verification.Check].Before(checked) {
			add("clean seed proof must run after successful verification: %s", verification.Check)
		}
	}
	required := []string{manifest.DatabaseTotalsCheck, manifest.DemoDataCheck, manifest.UI.Check}
	for _, requirement := range manifest.Requirements {
		required = append(required, requirement.Check)
	}
	for _, command := range required {
		if !checks[command] {
			add("record isolated verification storage for %s in data_delivery.report", command)
			checks[command] = true
		}
	}
	pending = append(pending, checkDashboardQuality(report.Dashboard)...)
	return pending
}
