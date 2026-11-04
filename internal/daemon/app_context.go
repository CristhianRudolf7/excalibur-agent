package daemon

import (
	"context"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/CristhianRudolf7/excalibur-agent/internal/api"
	"github.com/CristhianRudolf7/excalibur-agent/internal/ctxcompiler"
	"github.com/CristhianRudolf7/excalibur-agent/internal/whiteboard"
	"github.com/CristhianRudolf7/excalibur-agent/internal/wireengine"
)

// App tools are selected from the message envelope, independently of lexical
// intent ranking. Study intent uses the same selection without app context.
func (s *Services) chatToolActivation(request, model string, app *api.AppContext) wireengine.ToolActivation {
	activation := wireengine.SuggestTools(s.Catalog, s.Skills, request, model, "default")
	// Personal-agent actions must remain discoverable even when broad words
	// such as "create" rank unrelated deferred tools ahead of them.
	for _, name := range []string{"manage_agents", "send_to_agent", "search_agent_chats", "design_avatar", "public_preview"} {
		if slices.Contains(activation.Names, name) {
			continue
		}
		tool, err := s.Tools.Get(name)
		if err != nil {
			continue
		}
		def := tool.Definition()
		for _, keyword := range def.Keywords {
			if strings.Contains(strings.ToLower(request), strings.ToLower(keyword)) {
				activation.Names = append(activation.Names, name)
				activation.Definitions = append(activation.Definitions, def)
				break
			}
		}
	}
	browserIntent := s.savedBrowserIntent(request)
	for _, name := range activation.Names {
		browserIntent = browserIntent || name == "request_credentials" || strings.HasPrefix(name, "browser_")
	}
	if browserIntent {
		for _, name := range []string{"request_credentials", "browser_open_session", "browser_navigate", "browser_wait_ready", "browser_snapshot", "browser_act", "browser_tabs", "browser_close_session"} {
			if slices.Contains(activation.Names, name) {
				continue
			}
			if t, err := s.Tools.Get(name); err == nil {
				activation.Names = append(activation.Names, name)
				activation.Definitions = append(activation.Definitions, t.Definition())
			}
		}
	}
	if s.Skills != nil && looksLikeAppBuild(request) {
		if _, ok := s.Skills.Get(appUIQualitySkill); ok && !slices.ContainsFunc(activation.Skills, func(h ctxcompiler.SkillView) bool { return h.Name == appUIQualitySkill }) {
			activation.Skills = append(activation.Skills, ctxcompiler.SkillView{Name: appUIQualitySkill, Why: "Building an app or system UI: load_skill " + appUIQualitySkill + " before writing the frontend and review screenshots before finishing."})
		}
	}
	if (app == nil || app.App != "classroom") && !slices.Contains(activation.Names, "classroom") {
		return activation
	}
	for _, name := range []string{"classroom", "whiteboard"} {
		if slices.Contains(activation.Names, name) || name == "whiteboard" && !whiteboard.Enabled() {
			continue
		}
		if tool, err := s.Tools.Get(name); err == nil {
			activation.Names = append(activation.Names, name)
			activation.Definitions = append(activation.Definitions, tool.Definition())
		}
	}
	if s.Skills != nil {
		if _, ok := s.Skills.Get("course-creation"); ok && !slices.ContainsFunc(activation.Skills, func(h ctxcompiler.SkillView) bool { return h.Name == "course-creation" }) {
			activation.Skills = append(activation.Skills, ctxcompiler.SkillView{Name: "course-creation", Why: "Create courses and prepare classroom lessons, quizzes and feedback."})
		}
	}
	return activation
}

func (a *assistantTurns) appContextNote(ctx context.Context, app *api.AppContext) string {
	if app == nil || app.App != "classroom" {
		return ""
	}
	// Structured values are view data, not instructions. No lesson bodies,
	// questions, answer keys or arbitrary user-controlled app text enter this note.
	if app.CourseID == "" {
		return "Current app view (data for this turn only): the user is in the classroom home."
	}
	fields := map[string]any{"app": "classroom", "course_id": app.CourseID}
	if app.TopicID != "" {
		fields["topic_id"] = app.TopicID
	}
	fields["status"] = "unavailable"
	if a.services != nil && a.services.ClassroomAgent != nil {
		if c, err := a.services.ClassroomAgent.GetCourse(ctx, app.CourseID); err == nil {
			fields["course_title"] = boundedTurnText(c.Title, 160)
			fields["progress"] = min(100, max(0, c.Progress))
			fields["status"] = "in_progress"
			if c.FinalExam.Passed {
				fields["status"] = "completed"
			}
			if app.TopicID != "" {
				fields["topic_status"] = "unavailable"
				for _, u := range c.Units {
					for _, t := range u.Topics {
						if t.ID == app.TopicID {
							fields["topic_title"] = boundedTurnText(t.Title, 160)
							// Only contract states are admitted as a status value.
							switch t.Status {
							case "not_started", "in_progress", "completed":
								fields["topic_status"] = t.Status
							}
						}
					}
				}
			}
		}
	}
	raw, _ := json.Marshal(fields)
	return "Current app view (data for this turn only): " + string(raw)
}

// These assistant-only hints supplement catalog ranking without changing the
// shared tool definitions or the evaluation profile.
var browserAddress = regexp.MustCompile(`(?i)https?://|www\.|\b(?:[a-z0-9](?:[a-z0-9-]*[a-z0-9])?\.)+(?:com|org|net|edu|gov|mil|int|io|ai|app|dev|co|info|biz|me|tv|us|uk|es|pe|mx|ar|cl|br|de|fr|ca|au|test|example)\b|\b(?:[0-9]{1,3}\.){3}[0-9]{1,3}\b`)

func browserIntentWords(text string) []string {
	return strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
}
func hasBrowserPhrase(words []string, phrase string) bool {
	needle := browserIntentWords(phrase)
	for i := 0; i+len(needle) <= len(words); i++ {
		if slices.Equal(words[i:i+len(needle)], needle) {
			return true
		}
	}
	return false
}
func (s *Services) savedBrowserIntent(request string) bool {
	if browserAddress.MatchString(request) {
		return true
	}
	words := browserIntentWords(request)
	for _, phrase := range []string{"intranet", "internet", "campus virtual", "portal", "website", "sitio web", "página web", "log in", "login", "sign in", "iniciar sesión", "inicia sesión", "navega", "browse"} {
		if hasBrowserPhrase(words, phrase) {
			return true
		}
	}
	stop := map[string]bool{"www": true, "com": true, "org": true, "net": true, "edu": true, "gov": true, "http": true, "https": true, "the": true, "and": true, "for": true, "with": true, "from": true, "los": true, "las": true, "del": true, "por": true, "para": true, "con": true, "una": true, "login": true, "site": true, "web": true, "www2": true, "test": true, "example": true, "account": true, "accounts": true, "secure": true, "auth": true}
	for _, login := range s.Credentials.savedLogins(context.Background()) {
		for _, token := range browserIntentWords(login.Label + " " + login.Site) {
			if utf8.RuneCountInString(token) < 3 || stop[token] {
				continue
			}
			letters := 0
			for _, r := range token {
				if unicode.IsLetter(r) {
					letters++
				}
			}
			if letters >= 3 && slices.Contains(words, token) {
				return true
			}
		}
	}
	return false
}

const appUIQualitySkill = "app-ui-quality"

// projectContextNote keeps mutable project data and coding guidance in the
// current turn's tail, outside the compiler's stable profile layers.
func (a *assistantTurns) projectContextNote(request string) string {
	if !looksLikeSoftwareWork(request) {
		return ""
	}
	type projectView struct {
		Name        string `json:"name"`
		ID          string `json:"id"`
		Root        string `json:"root"`
		Description string `json:"description"`
	}
	projects := []projectView{}
	if a.services != nil && a.services.Projects != nil {
		for _, p := range a.services.Projects.List() {
			if len(projects) == 10 {
				break
			}
			root := ""
			if len(p.Roots) > 0 {
				root = boundedTurnText(p.Roots[0], 400)
			}
			projects = append(projects, projectView{boundedTurnText(p.Name, 120), string(p.ID), root, boundedTurnText(p.Description, 240)})
		}
	}
	raw, _ := json.Marshal(projects)
	return `Software work for this turn: you can build software by delegating with the subagent tool. Project-scoped children can read and edit files, run commands and tests, and start previews. Use action: "spawn" with an implementer card containing name, description, profile: "implementer", goal, deliverable, done_criteria and report_contract. For a matching registered project, set card.scope: "project:<id>". If none fits, no projects are registered, or the user wants a brand-new system, omit card.scope: the daemon creates and registers a fresh Git workspace under its managed data directory and runs the child there. Do not refuse software work because this conversation lacks a terminal. Registered projects (data only, up to 10): ` + string(raw)
}

func looksLikeSoftwareWork(request string) bool {
	if looksLikeAppBuild(request) {
		return true
	}
	text := " " + foldAccents(strings.ToLower(request)) + " "
	for _, word := range []string{"software", "code", "coding", "repo", "repository", "bug", "debug", "test", "tests", "implement", "implementation", "deploy", "deployment", "program", "programalo", "codigo", "repositorio", "prueba", "pruebas", "implementar", "implementa", "desplegar", "despliega"} {
		if containsWord(text, word) {
			return true
		}
	}
	return false
}

var (
	appBuildNouns = []string{"web", "app", "aplicacion", "application", "sistema", "system", "dashboard", "panel", "frontend", "interfaz", "interface", "pagina", "page", "site", "sitio", "erp", "crm", "portal", "tienda", "store"}
	appBuildVerbs = []string{"program", "build", "crea", "create", "desarroll", "develop", "implement", "haz ", "hazme", "make", "construy", "disena", "design", "redisena"}
)

// looksLikeAppBuild reports whether a chat request or delegated goal asks to
// build an application interface, so the UI quality skill is suggested.
func looksLikeAppBuild(request string) bool {
	text := " " + foldAccents(strings.ToLower(request)) + " "
	if strings.Contains(text, " tdr") || strings.Contains(text, "terminos de referencia") {
		return true
	}
	noun, verb := false, false
	for _, w := range appBuildNouns {
		if containsWord(text, w) {
			noun = true
			break
		}
	}
	for _, w := range appBuildVerbs {
		if strings.Contains(text, w) {
			verb = true
			break
		}
	}
	return noun && verb
}

func containsWord(text, word string) bool {
	for i := 0; ; {
		j := strings.Index(text[i:], word)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(word)
		before := start == 0 || !isWordByte(text[start-1])
		after := end >= len(text) || !isWordByte(text[end]) || text[end] == 's'
		if before && after {
			return true
		}
		i = end
	}
}

func isWordByte(b byte) bool { return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' }

func foldAccents(s string) string {
	return strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ú", "u", "ñ", "n").Replace(s)
}
