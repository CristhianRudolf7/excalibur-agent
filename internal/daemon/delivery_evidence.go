package daemon

import (
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/CristhianRudolf7/excalibur-agent/internal/tasks"
	"github.com/CristhianRudolf7/excalibur-agent/internal/tools"
)

type deliveryEvidence struct {
	dir             string
	required        []int
	requiredScreens []string
	refresh         func() string
	mu              sync.Mutex
	passed          map[string]bool
	checkedAt       map[string]time.Time
	inspected       map[string]time.Time
	sessions        map[string]string
}

type deliveryManifest struct {
	Requirements []struct {
		ID      int      `json:"id"`
		Feature string   `json:"feature"`
		Files   []string `json:"files"`
		Check   string   `json:"check"`
	} `json:"requirements"`
	Screens []struct {
		Name     string `json:"name"`
		Desktop  string `json:"desktop"`
		Mobile   string `json:"mobile"`
		Reviewed bool   `json:"reviewed"`
	} `json:"screens"`
	DatabaseTotalsCheck string               `json:"database_totals_check"`
	DemoDataCheck       string               `json:"demo_data_check"`
	Assumptions         []string             `json:"assumptions"`
	Formatters          []deliveryFormatter  `json:"formatters"`
	UI                  deliveryUIEvidence   `json:"ui"`
	DataDelivery        deliveryDataEvidence `json:"data_delivery"`
}

var numberedSpecRequirement = regexp.MustCompile(`(?mi)^\s*(?:#{1,6}\s*)?(?:\|\s*)?(?:(?:RF|REQ|R|requirement|requisito|requerimiento)[ #:-]*)?(\d{1,3})(?:[.):-]|\s*\||\s+\pL)`)

func newDeliveryEvidence(dir, source string) *deliveryEvidence {
	seen := map[int]bool{}
	for _, match := range numberedSpecRequirement.FindAllStringSubmatch(source, -1) {
		id, _ := strconv.Atoi(match[1])
		if id > 0 && id <= 100 {
			seen[id] = true
		}
	}
	required := make([]int, 0, len(seen))
	maximum := 0
	for id := range seen {
		if id > maximum {
			maximum = id
		}
	}
	for id := 1; id <= maximum; id++ {
		required = append(required, id)
	}
	screens := []string{"login", "dashboard"}
	lower := foldAccents(strings.ToLower(source))
	for _, module := range []struct {
		name  string
		words []string
	}{
		{"inventory", []string{"inventory", "inventario"}}, {"sales", []string{"sales", "ventas"}}, {"purchases", []string{"purchases", "compras"}},
		{"suppliers", []string{"suppliers", "proveedores"}}, {"customers", []string{"customers", "clientes"}}, {"movements", []string{"stock movements", "movimientos"}},
	} {
		for _, word := range module.words {
			if strings.Contains(lower, word) {
				screens = append(screens, module.name+"-list", module.name+"-form")
				break
			}
		}
	}
	return &deliveryEvidence{requiredScreens: screens, dir: dir, required: required, passed: map[string]bool{}, checkedAt: map[string]time.Time{}, inspected: map[string]time.Time{}, sessions: map[string]string{}}
}

// A worker's prose assertion is not execution evidence. Only successful exit
// receipts observed by the daemon can satisfy a manifest's check command.
func (e *deliveryEvidence) observe(name string, args json.RawMessage, result tools.Result) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if name == "inspect_image" {
		var input struct {
			Path string `json:"path"`
		}
		if json.Unmarshal(args, &input) == nil && input.Path != "" && result.Status == tools.StatusOK {
			e.inspected[e.imageKey(input.Path)] = time.Now()
		}
		return
	}
	if name != "exec" && name != "exec_io" {
		return
	}
	var input struct {
		Command string `json:"command"`
		Session string `json:"session"`
	}
	if json.Unmarshal(args, &input) != nil {
		return
	}
	command := input.Command
	if name == "exec_io" {
		command = e.sessions[input.Session]
	}
	if command == "" {
		return
	}
	if result.ExitCode != nil {
		e.checkedAt[command] = time.Now()
		e.passed[command] = result.Status == tools.StatusOK && *result.ExitCode == 0
	}
	for _, receipt := range result.Sessions {
		e.sessions[receipt.ID] = command
		if receipt.ExitCode != nil {
			e.checkedAt[command] = time.Now()
			e.passed[command] = result.Status == tools.StatusOK && *receipt.ExitCode == 0
		}
	}
}

func (e *deliveryEvidence) check() tasks.GateReport {
	e.mu.Lock()
	defer e.mu.Unlock()
	if len(e.required) == 0 && e.refresh != nil {
		fresh := newDeliveryEvidence(e.dir, e.refresh())
		e.required, e.requiredScreens = fresh.required, fresh.requiredScreens
	}
	pending := []string{}
	add := func(format string, args ...any) { pending = append(pending, fmt.Sprintf(format, args...)) }
	root, err := os.OpenRoot(e.dir)
	if err != nil {
		return tasks.GateReport{Pendings: []string{"delivery workspace unavailable"}}
	}
	defer root.Close()
	manifestFile, err := root.Open("delivery-evidence.json")
	if err != nil {
		return tasks.GateReport{Pendings: []string{"write delivery-evidence.json with every requirement, executed checks and reviewed screenshots"}}
	}
	defer manifestFile.Close()
	var manifest deliveryManifest
	if err = json.NewDecoder(io.LimitReader(manifestFile, 1<<20)).Decode(&manifest); err != nil {
		return tasks.GateReport{Pendings: []string{"invalid delivery-evidence.json: " + err.Error()}}
	}
	exists := func(name string) bool {
		info, err := root.Stat(name)
		return err == nil && info.Mode().IsRegular() && info.Size() > 0
	}
	if !exists("README.md") {
		add("README.md with setup, assumptions, demo access and checks is required")
	}
	if len(manifest.Assumptions) == 0 {
		add("record chosen defaults in assumptions")
	}
	sources, sourcePending := e.checkSourceQuality(root, manifest.Formatters)
	pending = append(pending, sourcePending...)
	pending = append(pending, e.checkUIEvidence(root, manifest, sources)...)
	pending = append(pending, e.checkDataDelivery(root, manifest, sources)...)
	if !e.passed[manifest.DatabaseTotalsCheck] {
		add("execute the database-derived dashboard totals test successfully")
	}
	if !e.passed[manifest.DemoDataCheck] {
		add("execute realistic demo data and category/identifier consistency checks successfully")
	}
	seen := map[int]bool{}
	for _, req := range manifest.Requirements {
		if seen[req.ID] || req.ID < 1 || strings.TrimSpace(req.Feature) == "" || len(req.Files) == 0 {
			add("requirement %d needs a unique ID, implemented feature and source files", req.ID)
		}
		seen[req.ID] = true
		if len(e.required) > 0 && req.ID > e.required[len(e.required)-1] {
			add("requirement %d is not in the numbered source", req.ID)
		}
		for _, file := range req.Files {
			if !exists(file) {
				add("requirement %d has no implemented file %s", req.ID, file)
			} else if info, err := root.Stat(file); err == nil && info.ModTime().After(e.checkedAt[req.Check]) {
				add("requirement %d changed after its check; rerun %s", req.ID, req.Check)
			}
		}
		if !e.passed[req.Check] {
			add("requirement %d needs a successful executed API test or browser check: %s", req.ID, req.Check)
		}
	}
	if len(e.required) == 0 {
		add("numbered source requirements are unavailable; read the complete attached specification before delivery")
	}
	if len(seen) == 0 {
		add("map every numbered source requirement to an implemented feature and executed check")
	}
	for _, id := range e.required {
		if !seen[id] {
			add("source requirement %d is missing", id)
		}
	}
	screens := map[string]bool{}
	for _, screen := range manifest.Screens {
		screens[strings.ToLower(screen.Name)] = true
		if !screen.Reviewed {
			add("inspect and fix screenshots for %s", screen.Name)
		}
		for _, size := range []struct {
			path          string
			width, height int
		}{{screen.Desktop, 1440, 900}, {screen.Mobile, 390, 844}} {
			f, err := root.Open(size.path)
			if err != nil {
				add("screenshot missing for %s: %s", screen.Name, size.path)
				continue
			}
			info, statErr := f.Stat()
			inspectedAt := e.inspected[e.imageKey(size.path)]
			if inspectedAt.IsZero() || statErr != nil || info.ModTime().After(inspectedAt) {
				add("inspect screenshot %s with inspect_image after its latest change", size.path)
			}
			config, _, err := image.DecodeConfig(io.LimitReader(f, 32<<20))
			_ = f.Close()
			if err != nil || config.Width != size.width || config.Height != size.height {
				add("screenshot %s must be a real %dx%d image", size.path, size.width, size.height)
			}
		}
	}
	for _, name := range e.requiredScreens {
		if !screens[name] {
			add("review %s at desktop and 390 px", name)
		}
	}
	return tasks.GateReport{OK: len(pending) == 0, Pendings: pending}
}

// The final delivery report is derived from verified evidence, so a model's
// last response cannot replace delivery with a question or a claimed URL.
func (e *deliveryEvidence) summary(spanish bool) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	file, err := os.OpenRoot(e.dir)
	if err != nil {
		return "Delivery completed with verified evidence in delivery-evidence.json."
	}
	defer file.Close()
	f, err := file.Open("delivery-evidence.json")
	if err != nil {
		return "Delivery completed with verified evidence in delivery-evidence.json."
	}
	defer f.Close()
	var manifest deliveryManifest
	_ = json.NewDecoder(io.LimitReader(f, 1<<20)).Decode(&manifest)
	if spanish {
		return fmt.Sprintf("Implementé los %d requisitos de la especificación. Las comprobaciones de funciones, totales de la base de datos y datos de demostración pasaron. Revisé %d pantallas en escritorio y móvil. README.md contiene las instrucciones y los valores elegidos; delivery-evidence.json contiene la evidencia y las rutas de las capturas.", len(manifest.Requirements), len(manifest.Screens))
	}
	return fmt.Sprintf("Implemented all %d specification requirements. Feature checks, database totals and demo data checks passed. Reviewed %d screens at desktop and mobile sizes. README.md contains setup and assumptions; delivery-evidence.json records the checks and screenshot paths.", len(manifest.Requirements), len(manifest.Screens))
}

func (e *deliveryEvidence) imageKey(path string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(e.dir, path)
	}
	return filepath.Clean(path)
}
