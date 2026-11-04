package daemon

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/CristhianRudolf7/excalibur-agent/internal/master"
)

var specBuildCommand = regexp.MustCompile(`\b(?:build|implement|programalo|programa|desarrollalo|desarrolla|construyelo|hazlo|proceed|code it)\b`)
var specBuildDenial = regexp.MustCompile(`\b(?:do not|don't|no|not yet|todavia no|aun no)\s+(?:build|implement|program|desarroll|constru|lo hagas)`)

// The approved-spec action bypasses the conversational model. Source text is
// still read by the worker through the existing attachment handoff.
func autonomousDeliveryCard(history []master.Normalized, msg master.Normalized) (json.RawMessage, bool) {
	text := foldAccents(strings.ToLower(msg.Text))
	if msg.RelayedBy != "" {
		return nil, false
	}
	if !specBuildCommand.MatchString(text) || specBuildDenial.MatchString(text) {
		return nil, false
	}
	rows := append(append([]master.Normalized(nil), history...), msg)
	ids := delegationDocumentIDs(rows, "")
	if len(ids) == 0 {
		return nil, false
	}
	var requests []string
	for i := len(rows) - 1; i >= 0 && len(requests) < delegationAttachmentTurns; i-- {
		if rows[i].Role == "assistant" || rows[i].Role == "system" {
			continue
		}
		if len(requests) == 0 || rows[i].Text != requests[len(requests)-1] {
			requests = append(requests, rows[i].Text)
		}
	}
	goal := "Implement the complete attached client specification as a working web application. Read every page and implement every numbered requirement. User request and context (newest first):\n" + strings.Join(requests, "\n")
	raw, _ := json.Marshal(map[string]any{"action": "spawn", "card": map[string]any{
		"name": "client-spec-delivery", "description": "complete approved client specification", "profile": "implementer", "goal": goal,
		"deliverable": "Complete application, realistic seeded database, automated tests, README, requirement evidence and reviewed desktop/mobile screenshots",
		"inputs":      ids, "suggested_skills": []string{appUIQualitySkill},
		"done_criteria":       []string{"Every numbered source requirement has an implemented feature and an executed API test or browser check", "Dashboard totals are recomputed from seeded database transactions in an executed test", "Realistic products, coherent categories, plausible names and valid local identifier formats are checked", "app-ui-quality reviewed with screenshots of every main screen at 1440x900 and 390x844", "README documents setup, demo access, assumptions, tests and screenshots", "delivery-evidence.json passes the daemon completion gate"},
		"pinned_instructions": []string{"Ask zero questions. Choose reasonable defaults and record assumptions. Complete the full spec before reporting. Internal budgets are not user deadlines.", deliveryEvidenceInstructions},
		"report_contract":     "One final summary after all features and checks pass; include assumptions, test results, screenshot paths and the verified public URL and expiry when requested. Never name the underlying model.",
	}})
	return raw, true
}

const deliveryEvidenceInstructions = professionalSourceInstructions + interactiveControlInstructions + cleanDataInstructions + dashboardQualityInstructions + `
Before finishing, write delivery-evidence.json in the project root with this JSON shape:
{"requirements":[{"id":1,"feature":"implemented behavior","files":["src/example.js"],"check":"exact successful exec command"}],"screens":[{"name":"login","desktop":"screenshots/login-desktop.png","mobile":"screenshots/login-mobile.png","reviewed":true}],"database_totals_check":"exact successful exec command","demo_data_check":"exact successful exec command","assumptions":["chosen defaults"]}.
Include every numbered source requirement, with its own implemented feature, existing source files and an executed API test or browser check. Run checks via exec and await exit code 0 (exec_io for background sessions); cite the exact command. The database totals test must independently recompute dashboard totals from seeded transactions. The demo data test must check category coherence, plausible names, RUC (11 digits) and DNI (8 digits) where applicable, stock movements and totals derived from transactions. No placeholder customer names. Use English screen names login, dashboard, inventory-list, inventory-form, sales-list, sales-form, purchases-list, purchases-form, suppliers-list, suppliers-form, customers-list, customers-form, movements-list and movements-form where those modules occur in the source. Include screenshots for login, dashboard, every module list and form at 1440x900 and 390x844. Inspect each screenshot with inspect_image before setting reviewed:true; the daemon requires successful inspection receipts for each image, repeated after image changes; fix icons, alignment, layout and mobile overflow. Use real SVG icons, right-aligned tabular money and a sidebar that extends to the page bottom. Do not mark a requirement complete merely because a navigation link exists. Include setup, demo login, tests, assumptions and screenshot locations in README.md. Finish only when this evidence is complete.`

var approvedSpecStatement = regexp.MustCompile(`\b(?:client approved|approved (?:spec|specification|tdr)|cliente aprobo|tdr aprobados|especificacion aprobada)\b`)

func approvedSpecAcknowledgement(history []master.Normalized, msg master.Normalized) bool {
	text := foldAccents(strings.ToLower(msg.Text))
	if !approvedSpecStatement.MatchString(text) || specBuildCommand.MatchString(text) {
		return false
	}
	return len(delegationDocumentIDs(append(append([]master.Normalized(nil), history...), msg), "")) > 0
}

const professionalSourceInstructions = robustIdentifierInstructions + `Write source as a professional maintainer would: descriptive identifiers, readable functions, conventional formatting and clear module boundaries. Never compress hand-written source into long lines or use single-letter state names. Keep each source file at most 600 lines; split backend routes, data access and seed setup into modules, and frontend screens into reusable components. Outside string literals and literal data tables, keep code lines at most 200 characters, average code line length at most 100, and at most two top-level statements per line.
Use the project's configured formatter. If absent, use an offline standard formatter such as prettier, gofmt or black; never download one during verification. Apply formatter output, then run a formatting check or successful write invocation after the final source edit. Add formatters:[{"check":"prettier --check .","files":["src/example.js"]}] to delivery-evidence.json, covering every hand-written source file, including tests and styles. Commands must directly invoke a supported formatter or an existing package.json formatter script (for example npm run format:check); use npx --no-install when needed. gofmt must use -w because -l alone does not fail on formatting differences. Only successful daemon-observed exits count. Rerun after edits. Build output, dependency/lock files, vendored files and generated files with standard generated headers are excluded. The gate returns file:line offenders; fix every offender before delivery.`

const interactiveControlInstructions = `
Every interactive control must work as labelled. Search and date/filter inputs must change the displayed result set, not merely show a notice or echo the input. Buttons, navigation, validation, submissions and detail views need executed browser assertions against their labelled effect. Check detail rendering against the actual response shape. Forms with multiple items need variable-length add/remove controls; exercise more rows than initially rendered, remove rows, submit and verify persistence after reload. Use descriptive control identifiers.
Record ui:{"check":"node check-browser.mjs","report":"browser-report.json"} in delivery-evidence.json. Run the browser suite through exec and await exit zero. The suite must generate the JSON report after the latest source edit. Use playwright, puppeteer or webdriver, and report runner accordingly. For every screen (including details, dialogs and conditional states), enumerate the union of discovered controls with a stable id, label, selector and kind; discovered must equal the inventory length. Discover native buttons, inputs, selects, textareas, links, forms, ARIA interactive roles and contenteditable elements from the DOM, including controls revealed after interaction. Check disabled states and how they become enabled; do not omit controls. Include all screens in screens and review their screenshots.
Browser report shape: {"runner":"playwright","screens":[{"name":"example","url":"http://127.0.0.1:5308/example","discovered":1,"controls":[{"id":"query","label":"Search","selector":"input[type=search]","kind":"search"}]}],"checks":[{"screen":"example","control":"query","action":"fill with a matching query","effect":"rows","target":"tbody tr","before":["alpha","beta"],"after":["alpha"],"passed":true}]}.
Record every discovered control, and at least one successful assertion per control. kind is search, filter, button, submit, form, detail, field, link, add-item or remove-item. Capture before/after state from the live browser, not invented values. Search/filter effects must be rows arrays that shrink under a selective input. Link effects are route strings. Detail effects are {visible:true,record:{...rendered values...}}, not an unrendered API array. Field/form effects use {values:{...}} or {errors:{...}}; successful submissions use records arrays/objects read back after saving. Button effects can be rows, items, records, form, detail, route or dialog (boolean open state). Message/notice/toast-only results and unchanged state fail. Declare each multiple-item form control repeatable:true, with add-item/remove-item controls referencing its id in form; their items arrays must grow/shrink in successful checks. Include the form's submit/persistence assertion too. Use DOM-derived row identities/values, persisted records and rendered detail fields as evidence. Run delivery_check and repair every missing or ineffective control; do not remove controls from inventory to silence a failure.
`

const robustIdentifierInstructions = `Allocate user-visible document numbers from a database sequence or MAX(existing_number) + 1 inside the same transaction that inserts the record. Never use count+1 or collection length+1: deletion leaves gaps and can cause duplicate numbers. Prefer database-generated primary keys for internal identifiers. For MAX+1, serialize allocation with the database's appropriate locking/isolation, or retry the entire transaction on a uniqueness conflict. Keep allocation and insertion atomic, handle rollback, and test creation after deletions and concurrent creation. Use a durable sequence when numbers must never be reused; gaps are acceptable unless the requirements explicitly forbid them.
`

const cleanDataInstructions = `
Run all automated tests, API checks, verification scripts and mutating browser checks against isolated storage: a temporary file, in-memory database or separate DB_FILE/DATABASE_URL. Never use the delivered application's database for verification. Start the delivered app from a clean seed. After every verification run and final source edit, reset/reseed the delivered data or prove the complete delivered dataset equals the seed before publication or completion. Capture final screenshots from that clean delivered app with read-only browsing.
Add data_delivery:{"check":"node verify-delivery-data.mjs","report":"data-delivery-report.json"} to delivery-evidence.json. Execute this check after all referenced requirement, database, demo and browser checks and await exit zero. Generate the report in that command: {"delivery_storage":"demo-store","seed_fingerprint":"64 lowercase SHA-256 hex digits","delivered_fingerprint":"same digest","verification":[{"check":"exact executed verification command","storage":"isolated-store"}]}. List every referenced verification command with its actual storage identity. Use canonical credential-free storage identities (resolve path/URL aliases and symlinks); never put credentials in evidence. Hash a canonical export of the complete persisted demo dataset against a fresh seed, excluding only documented nondeterministic metadata; do not hash the seed source file or merely assert success. Each verification storage must differ from delivery_storage. Rerunning verification invalidates the seed proof: rerun the data delivery check. Missing isolation or clean seed evidence blocks publication and completion.
`

const dashboardQualityInstructions = `
Use unique, specific KPI card and section labels that state the measure and time scope. When a count is zero, show a calm empty state instead of an attention/warning panel. Charts must show the seeded history across the intended date range, with multiple populated days, never a single-day spike caused by verification writes.
In data_delivery.report add dashboard:{"labels":[{"label":"Activity this month","selector":"#activity-kpi"},{"label":"Daily activity history","selector":"#history-heading"}],"panels":[{"selector":"#attention-panel","count":0,"alert_visible":false,"empty_visible":true}],"charts":[{"selector":"#history-chart","seeded":[{"date":"2026-01-01","value":12},{"date":"2026-01-02","value":18}],"rendered":[{"date":"2026-01-01","value":12},{"date":"2026-01-02","value":18}]}]}. Inventory every KPI/section label, count-based panel (use [] when none) and history chart from the delivered app DOM. As part of the clean seed proof command, use read-only browser assertions against the delivered app after reset/reseed, record the actual count and alert/empty visibility, and compare every rendered date bucket and value with an independent aggregation from the fresh seed. Arrays must cover the full displayed date range, in ascending ISO date order, with at least two populated days. Values are numeric; use the same exact units on both sides. Verify that persisted data still equals the seed after these read-only checks. Review final desktop/mobile screenshots for these conditions and document the chosen measure/time scope. Specific label meaning and DOM inventory completeness remain your responsibility; do not invent observations or omit a failing card/panel/chart.
`
