package ai

// The assistant: a contextual helper on every screen of administration, client onboarding and
// Stage 1 (chart of accounts). A person asks a question, optionally with documents; the model
// answers and may propose suggestions. It never changes anything: every suggestion is shown
// with its evidence and confidence, and applied only when the person accepts it, through the
// same API and checks as a manual change, by someone allowed to make it.
//
// What leaves the server is minimised: the server builds the context from what the caller may
// see in the bound tenant only; every e-mail address (users, or any in a document) is replaced
// by an alias before the request and restored in the answer; documents are capped in size.

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gl/model"
)

// Areas of the application the assistant serves.
const (
	AreaAdmin      = "admin"
	AreaOnboarding = "onboarding"
	AreaStage1     = "stage1"
)

// Stage 1 steps.
var Stage1Steps = []string{"import", "classes", "assignments", "review", "submit"}

// Limits of one request (cost controls; the daily budget is the server's).
const (
	MaxDocChars      = 120_000
	MaxQuestionChars = 4_000
	MaxSuggestions   = 80
)

// Doc is a document the person attached, as text (a sheet as CSV, a BRD as text).
type Doc struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

// AssistRequest is one question in one place of the application.
type AssistRequest struct {
	Area      string
	Step      string
	Question  string
	Documents []Doc
	Context   map[string]any // built by the server from what the caller may see
}

// Suggestion is one proposed change or finding, never applied by the assistant.
type Suggestion struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Title      string         `json:"title"`
	Why        string         `json:"why"`
	Confidence string         `json:"confidence"` // high | medium | low
	Evidence   []Evidence     `json:"evidence"`
	Data       map[string]any `json:"data"`
	Notes      []string       `json:"notes,omitempty"` // what the guardrails found
}

// AssistResult is the assistant's answer.
type AssistResult struct {
	Answer      string       `json:"answer"`
	Suggestions []Suggestion `json:"suggestions"`
	Questions   []string     `json:"questions"`
	Provider    string       `json:"provider"`
	Model       string       `json:"model"`
	Usage       Usage        `json:"usage"`
	Aliased     int          `json:"aliased"` // e-mail addresses replaced by aliases before sending
	Truncated   bool         `json:"truncated"`
}

// kinds a suggestion may have, by area and step.
var kinds = map[string][]string{
	AreaAdmin:                   {"roles", "conflict", "note"},
	AreaOnboarding:              {"client", "roles", "missing", "note"},
	AreaStage1 + "/import":      {"columns", "missing", "note"},
	AreaStage1 + "/classes":     {"mapping", "newClass", "note"},
	AreaStage1 + "/assignments": {"rule", "conditions", "note"},
	AreaStage1 + "/review":      {"issue", "note"},
	AreaStage1 + "/submit":      {"issue", "note"},
	AreaStage1 + "/":            {"note"},
	AreaOnboarding + "/":        {"client", "roles", "missing", "note"},
	AreaAdmin + "/":             {"roles", "conflict", "note"},
}

func kindsFor(area, step string) []string {
	if k, ok := kinds[area+"/"+step]; ok {
		return k
	}
	if area == AreaStage1 {
		return kinds[AreaStage1+"/"]
	}
	return kinds[area]
}

// ValidArea says whether the area and step are known.
func ValidArea(area, step string) bool {
	switch area {
	case AreaAdmin, AreaOnboarding:
		return step == ""
	case AreaStage1:
		for _, s := range Stage1Steps {
			if s == step {
				return true
			}
		}
	}
	return false
}

// ── aliases for e-mail addresses ─────────────────────────────────────────────────────────────

var emailRe = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// Aliases replaces e-mail addresses with stable aliases (person-1, …) and restores them.
type Aliases struct {
	toAlias map[string]string
	toEmail map[string]string
}

func NewAliases() *Aliases {
	return &Aliases{toAlias: map[string]string{}, toEmail: map[string]string{}}
}

// Hide replaces every e-mail address in s.
func (a *Aliases) Hide(s string) string {
	return emailRe.ReplaceAllStringFunc(s, func(e string) string {
		k := strings.ToLower(e)
		if al, ok := a.toAlias[k]; ok {
			return al
		}
		al := fmt.Sprintf("person-%d", len(a.toAlias)+1)
		a.toAlias[k], a.toEmail[al] = al, k
		return al
	})
}

var aliasRe = regexp.MustCompile(`person-\d+`)

// Show restores the addresses an answer refers to by alias.
func (a *Aliases) Show(s string) string {
	return aliasRe.ReplaceAllStringFunc(s, func(al string) string {
		if e, ok := a.toEmail[al]; ok {
			return e
		}
		return al
	})
}

// Count is how many addresses were hidden.
func (a *Aliases) Count() int { return len(a.toAlias) }

func (a *Aliases) showAny(v any) any {
	switch x := v.(type) {
	case string:
		return a.Show(x)
	case []any:
		for i := range x {
			x[i] = a.showAny(x[i])
		}
		return x
	case map[string]any:
		for k, y := range x {
			x[k] = a.showAny(y)
		}
		return x
	}
	return v
}

// ── the request ──────────────────────────────────────────────────────────────────────────────

const assistRules = `You are the configuration assistant of the GL Ecosystem, a general-ledger configuration tool for insurers.
You help a person; you never change anything yourself. Everything you propose is shown to the person, who accepts, edits or rejects it.
Rules you must follow:
- Use only facts in the CONTEXT and the DOCUMENTS. Never invent account numbers, class codes, condition values, users or roles.
- Every suggestion cites evidence: short quotes copied exactly from the DOCUMENTS or the CONTEXT (a row, a cell, a sentence).
- Say how sure you are (confidence high, medium or low) and what you are unsure about. Ask when information is missing.
- Source codes (for example a policy system's accounting class codes) are kept exactly as received; never rename them.
- Standard classes come only from the library in the CONTEXT; propose a new class only when no library class has the same meaning.
- You cannot grant roles, approve configurations or bypass permissions; you may only propose changes for a person to confirm.
- People are referred to by alias (person-1, …); use the aliases as given.
Answer with one JSON object: {"answer": "plain business language, a few short paragraphs or bullet lines",
 "suggestions": [{"kind": "...", "title": "short", "why": "the reason in business words", "confidence": "high|medium|low",
   "evidence": ["exact quote", "..."], "data": { ... }}],
 "questions": ["what you need from the person"]}`

var kindHelp = map[string]string{
	"roles":      `"roles": data {"user": "person-1 or an address the person gave", "roles": ["GL_ADMIN"|"CONFIG_EDITOR"|"CONFIG_APPROVER"|"OPERATOR"|"VIEWER"]} — the full set of roles proposed for that user in this client.`,
	"conflict":   `"conflict": data {"user": "person-1", "roles": [...], "issue": "why the combination is risky (for example editing and approving the same configuration)"}.`,
	"client":     `"client": data {"name", "configId" (upper-case code), "effectiveFrom" (YYYY-MM-DD), "description", "sourceSystems": [codes], "parameters": {name: value}} — only what the documents or context state.`,
	"missing":    `"missing": data {"item": "what is still needed", "who": "who should provide it"}.`,
	"columns":    `"columns": data {"file": "document name", "sheet": "sheet name if any", "kind": "COA"|"CLASSES"|"ASSIGNMENTS"|"OTHER", "columns": {role: "exact column header"}, "conditions": ["exact headers that are assignment conditions"]}. Roles for COA: account, name, type, normalBalance, class, activeFrom, activeTo, currency, entity. For CLASSES: code, meaning, role, system. For ASSIGNMENTS: class, account, priority, effectiveFrom, effectiveTo, accountAU-style columns are conditions only if the document says so.`,
	"mapping":    `"mapping": data {"system": "source system code", "code": "source class code exactly as received", "mappings": [{"standard": "LIBRARY_CODE", "when": {"CONDITION": "value"}}]} — one mapping without "when", or one per meaning with the condition that tells them apart.`,
	"newClass":   `"newClass": data {"code": "UPPER_CODE", "name", "definition", "nature": "ASSET"|"LIABILITY"|"EQUITY"|"INCOME"|"EXPENSE", "normalSide": "D"|"C"} — only when no library class has the meaning.`,
	"rule":       `"rule": data {"sourceRow": the row number in the document, "sourceClass": "code as received", "class": "client class code", "match": {"CONDITION": "value or *"}, "account": "account number exactly as in the documents or COA", "priority": number or omitted}.`,
	"conditions": `"conditions": data {"file": "document name", "columns": ["exact headers that are conditions"], "dimensions": {"exact header": "UPPER_CODE dimension name"}}.`,
	"issue":      `"issue": data {"path": "where (section/item)", "severity": "error"|"warning", "message": "what is wrong and how to fix it"}.`,
	"note":       `"note": data {} — an observation that needs no change.`,
}

func (r AssistRequest) messages(al *Aliases) ([]Message, bool) {
	ks := kindsFor(r.Area, r.Step)
	var help []string
	for _, k := range ks {
		help = append(help, "- "+kindHelp[k])
	}
	sys := assistRules + "\nWhere you are: " + r.Area + stepSuffix(r.Step) + ".\nSuggestion kinds allowed here:\n" + strings.Join(help, "\n")
	ctxJSON, _ := json.Marshal(r.Context)
	var b strings.Builder
	b.WriteString("CONTEXT (JSON):\n")
	b.WriteString(al.Hide(string(ctxJSON)))
	truncated := false
	left := MaxDocChars
	for _, d := range r.Documents {
		text := d.Text
		if n := len([]rune(text)); n > left {
			text = string([]rune(text)[:max(left, 0)])
			truncated = true
		}
		left -= len([]rune(text))
		fmt.Fprintf(&b, "\n\nDOCUMENT %q:\n%s", d.Name, al.Hide(text))
	}
	if truncated {
		b.WriteString("\n\n(Some document text was cut to the size limit; say so if it matters.)")
	}
	b.WriteString("\n\nQUESTION:\n" + al.Hide(r.Question))
	return []Message{{Role: "system", Content: sys}, {Role: "user", Content: b.String()}}, truncated
}

func stepSuffix(s string) string {
	if s == "" {
		return ""
	}
	return " → step " + s
}

// Assist asks the model once and applies the guardrails. sources is the text evidence may
// quote (documents and context); known holds the codes a suggestion may refer to.
func Assist(ctx context.Context, d Drafter, r AssistRequest, known Known) (*AssistResult, error) {
	st := d.Status()
	if !st.Available {
		return nil, ErrProvider(st.Reason)
	}
	if !ValidArea(r.Area, r.Step) {
		return nil, fmt.Errorf("unknown area %q step %q", r.Area, r.Step)
	}
	al := NewAliases()
	msgs, truncated := r.messages(al)
	raw, u, err := d.JSON(ctx, msgs)
	if err != nil {
		return nil, err
	}
	var out struct {
		Answer      string `json:"answer"`
		Suggestions []struct {
			Kind       string         `json:"kind"`
			Title      string         `json:"title"`
			Why        string         `json:"why"`
			Confidence string         `json:"confidence"`
			Evidence   []string       `json:"evidence"`
			Data       map[string]any `json:"data"`
		} `json:"suggestions"`
		Questions []string `json:"questions"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, ErrProvider("The AI's answer was not in the expected form; ask again, perhaps more simply.")
	}
	res := &AssistResult{Answer: al.Show(out.Answer), Suggestions: []Suggestion{}, Questions: []string{}, Provider: st.Provider, Model: st.Model,
		Usage: u, Aliased: al.Count(), Truncated: truncated}
	for _, q := range out.Questions {
		if strings.TrimSpace(q) != "" {
			res.Questions = append(res.Questions, al.Show(q))
		}
	}
	src := newSources(r, al)
	allowed := kindsFor(r.Area, r.Step)
	for i, s := range out.Suggestions {
		if len(res.Suggestions) >= MaxSuggestions {
			break
		}
		if !contains(allowed, s.Kind) {
			continue // a kind that does not belong here is dropped
		}
		sg := Suggestion{ID: fmt.Sprintf("S%d", i+1), Kind: s.Kind, Title: al.Show(s.Title), Why: al.Show(s.Why), Confidence: confidence(s.Confidence),
			Evidence: []Evidence{}, Data: map[string]any{}}
		if s.Data != nil {
			sg.Data = al.showAny(s.Data).(map[string]any)
		}
		for _, q := range s.Evidence {
			if strings.TrimSpace(q) == "" {
				continue
			}
			sg.Evidence = append(sg.Evidence, Evidence{Quote: al.Show(q), Found: src.has(q)})
		}
		known.guard(&sg)
		if len(sg.Evidence) == 0 && sg.Kind != "note" {
			sg.Notes = append(sg.Notes, "No evidence was given: check it against the documents before accepting.")
			sg.Confidence = "low"
		} else if !anyFound(sg.Evidence) && sg.Kind != "note" {
			sg.Notes = append(sg.Notes, "None of the quoted evidence was found in the documents or the configuration.")
			sg.Confidence = "low"
		}
		res.Suggestions = append(res.Suggestions, sg)
	}
	return res, nil
}

func confidence(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high", "medium":
		return strings.ToLower(strings.TrimSpace(s))
	}
	return "low"
}

func anyFound(es []Evidence) bool {
	for _, e := range es {
		if e.Found {
			return true
		}
	}
	return false
}

// sources is the text evidence may quote, word-normalised.
type sources struct{ text string }

func newSources(r AssistRequest, al *Aliases) sources {
	var b strings.Builder
	ctxJSON, _ := json.Marshal(r.Context)
	b.WriteString(normText(al.Hide(string(ctxJSON))))
	for _, d := range r.Documents {
		b.WriteString(" ")
		b.WriteString(normText(al.Hide(d.Text)))
	}
	return sources{text: " " + b.String() + " "}
}

func (s sources) has(q string) bool {
	for _, part := range elision.Split(q, -1) {
		n := normText(part)
		if n != "" && !strings.Contains(s.text, " "+n+" ") {
			return false
		}
	}
	return normText(q) != ""
}

// Known is what suggestions may refer to; anything else is flagged, never silently used.
type Known struct {
	Accounts        map[string]bool // the client's accounts (codes)
	DocumentText    string          // to accept account numbers that appear in the documents
	Classes         map[string]bool // the client's classes
	StandardClasses map[string]bool // the library
	Roles           map[string]bool
}

func (k Known) guard(s *Suggestion) {
	str := func(key string) string { v, _ := s.Data[key].(string); return strings.TrimSpace(v) }
	flag := func(note string) {
		s.Notes = append(s.Notes, note)
		s.Confidence = "low"
	}
	switch s.Kind {
	case "roles", "conflict":
		roles, _ := s.Data["roles"].([]any)
		var keep []any
		for _, r := range roles {
			rs, _ := r.(string)
			if k.Roles[rs] {
				keep = append(keep, rs)
			} else {
				flag(fmt.Sprintf("%q is not a role of the GL Ecosystem and was removed.", rs))
			}
		}
		s.Data["roles"] = keep
	case "rule":
		acct := str("account")
		if acct != "" && !k.Accounts[acct] && !strings.Contains(k.DocumentText, acct) {
			flag(fmt.Sprintf("Account %s is neither in the client's chart of accounts nor in the documents.", acct))
		} else if acct != "" && !k.Accounts[acct] {
			s.Notes = append(s.Notes, fmt.Sprintf("Account %s is in the documents but not yet in the chart of accounts: import it first.", acct))
		}
		if c := str("class"); c != "" && !k.Classes[c] {
			flag(fmt.Sprintf("Class %s is not a class of this configuration yet: map the source classes first.", c))
		}
	case "mapping":
		ms, _ := s.Data["mappings"].([]any)
		for _, m := range ms {
			mm, _ := m.(map[string]any)
			std, _ := mm["standard"].(string)
			if !k.StandardClasses[std] {
				flag(fmt.Sprintf("%q is not in the standard class library.", std))
			}
		}
	case "newClass":
		if c := str("code"); k.StandardClasses[c] {
			flag(fmt.Sprintf("%s is already a library class: map to it instead of creating one.", c))
		}
		if n := str("nature"); n != "" && !contains([]string{"ASSET", "LIABILITY", "EQUITY", "INCOME", "EXPENSE"}, n) {
			flag(fmt.Sprintf("Nature %q is not one of ASSET, LIABILITY, EQUITY, INCOME, EXPENSE.", n))
		}
	}
}

// KnownFrom builds Known from a document and the library.
func KnownFrom(d *model.Document, lib model.ClassLibrary, roles []string, docs []Doc) Known {
	k := Known{Accounts: map[string]bool{}, Classes: map[string]bool{}, StandardClasses: map[string]bool{}, Roles: map[string]bool{}}
	if d != nil {
		for _, a := range d.Accounts {
			k.Accounts[a.Code] = true
		}
		for _, c := range d.AccountingClasses {
			k.Classes[c.Code] = true
		}
	}
	for _, c := range lib.Classes {
		k.StandardClasses[c.Code] = true
	}
	for _, r := range roles {
		k.Roles[r] = true
	}
	var b strings.Builder
	for _, x := range docs {
		b.WriteString(x.Text)
		b.WriteString("\n")
	}
	k.DocumentText = b.String()
	return k
}

// SortedKeys is a helper for building contexts deterministically.
func SortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
