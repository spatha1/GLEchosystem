package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gl/model"
)

// The steps, in order. Each works on the draft as earlier approved steps left it.
const (
	StepEvents   = "events"
	StepMeasures = "measures"
	StepFormulas = "formulas"
	StepRules    = "rules"
)

// Steps lists them in order.
var Steps = []string{StepEvents, StepMeasures, StepFormulas, StepRules}

// MaxAttempts bounds the draft → check → repair loop.
const MaxAttempts = 3

// Checker validates candidate content exactly as saving it would, returning its findings
// and its canonical form.
type Checker func(content []byte) ([]model.Finding, []byte, error)

// Request is one step for one draft version.
type Request struct {
	Step          string
	BRD           BRD
	Notes         string // the person's guidance for this step, optional
	Content       []byte // the draft's content, canonical
	EffectiveFrom string // the version's effective date; test cases are dated on it
	Ref           model.ConfigRef
}

// Evidence is a quote from the BRD that a proposal relies on.
type Evidence struct {
	Quote string `json:"quote"`
	Found bool   `json:"found"` // the quote is in the BRD (case and spacing aside); false flags a possible invention
}

// Part is one element inside an item: a measure of an event, a line of a rule.
type Part struct {
	Code     string     `json:"code"`
	Label    string     `json:"label,omitempty"`
	Note     string     `json:"note,omitempty"`
	Evidence []Evidence `json:"evidence,omitempty"`
}

// Item is one configuration item the step proposes, as it will be saved: Value is upserted
// into Section by Key.
type Item struct {
	ID         string         `json:"id"`
	Section    string         `json:"section"`
	Key        string         `json:"key"`
	Group      string         `json:"group"`  // the business event it belongs to; "" for shared reference data
	Change     string         `json:"change"` // add | update
	Label      string         `json:"label"`
	Why        string         `json:"why,omitempty"`
	Confidence string         `json:"confidence,omitempty"`
	Evidence   []Evidence     `json:"evidence"`
	Parts      []Part         `json:"parts,omitempty"`
	Value      map[string]any `json:"value"`
}

// Gap is something only a person may supply: an account number, a source system, the values
// of a closed dimension.
type Gap struct {
	Kind    string `json:"kind"` // ACCOUNT_NUMBER | SOURCE_SYSTEM | DIMENSION_VALUES
	Code    string `json:"code"`
	Name    string `json:"name,omitempty"`
	Group   string `json:"group,omitempty"`
	Message string `json:"message"`
}

// CaseLine is one journal line of a test case.
type CaseLine struct {
	Line    string `json:"line"`
	Class   string `json:"class"`
	Side    string `json:"side"`
	Amount  string `json:"amount"`
	Entity  string `json:"entity"`
	Account string `json:"account,omitempty"`
	Problem string `json:"problem,omitempty"`
}

// Case is a synthetic test case run through the proposed rule.
type Case struct {
	Code        string            `json:"code"`
	Event       string            `json:"event"`
	Description string            `json:"description"`
	Currency    string            `json:"currency"`
	Measures    map[string]string `json:"measures"`
	Lines       []CaseLine        `json:"lines"`
	Skipped     []string          `json:"skipped"`
	Debit       string            `json:"debit"`
	Credit      string            `json:"credit"`
	Balanced    bool              `json:"balanced"`
	Problem     string            `json:"problem,omitempty"`
}

// Balance states a rule's two sides: debit amounts (LHS) against credit amounts (RHS).
type Balance struct {
	Rule        string   `json:"rule"`
	Event       string   `json:"event"`
	LHS         []string `json:"lhs"`
	RHS         []string `json:"rhs"`
	Conditional []string `json:"conditional"`
}

// Proposal is what one step proposes, already checked by the engine.
type Proposal struct {
	Step      string          `json:"step"`
	Provider  string          `json:"provider"`
	Model     string          `json:"model"`
	BRD       string          `json:"brd"`
	Items     []Item          `json:"items"`
	Gaps      []Gap           `json:"gaps"`
	Questions []string        `json:"questions"`
	Notes     []string        `json:"notes"`    // what the guardrails dropped or changed, and why
	Findings  []model.Finding `json:"findings"` // findings the proposal adds to the draft
	Problems  []string        `json:"problems"` // what the engine still finds wrong after the last attempt
	Cases     []Case          `json:"cases"`
	Balance   []Balance       `json:"balance"`
	Attempts  int             `json:"attempts"`
	Usage     Usage           `json:"usage"`
}

// ValidStep says whether s names a step.
func ValidStep(s string) bool {
	for _, x := range Steps {
		if x == s {
			return true
		}
	}
	return false
}

// personCodes are findings a person resolves (an account number, a closed dimension's
// values), never the AI.
var personCodes = map[string]bool{"MISSING_ACCOUNT": true, "MISSING_VALUES": true, "MISSING_ASSIGNMENT": true}

// RunStep asks the model for one step, applies the guardrails, checks the result with the
// engine and lets the model repair what the engine finds, up to MaxAttempts.
func RunStep(ctx context.Context, d Drafter, req Request, check Checker) (*Proposal, error) {
	st := d.Status()
	if !st.Available {
		return nil, ErrProvider(st.Reason)
	}
	if !ValidStep(req.Step) {
		return nil, fmt.Errorf("unknown step %q", req.Step)
	}
	doc, _ := model.Parse(req.Content)
	baseline, _, err := check(req.Content)
	if err != nil {
		return nil, err
	}
	s := newStep(req, doc)
	msgs := []Message{{Role: "system", Content: systemPrompt + "\n\n" + s.task()}, {Role: "user", Content: s.context() + brdBlock(req)}}
	p := &Proposal{Step: req.Step, Provider: st.Provider, Model: st.Model, BRD: req.BRD.Name}
	for attempt := 1; ; attempt++ {
		raw, u, err := d.JSON(ctx, msgs)
		p.Usage.add(u)
		p.Attempts = attempt
		if err != nil {
			return nil, err
		}
		r := s.build(raw)
		if r.parseErr == "" {
			cand, cerr := Apply(req.Content, r.items)
			if cerr != nil {
				r.problems = append(r.problems, cerr.Error())
			} else {
				fs, canonical, cerr := check(cand)
				if cerr != nil {
					r.problems = append(r.problems, cerr.Error())
				} else {
					if req.Step == StepRules {
						s.runCases(&r, canonical)
						if cand2, e := Apply(req.Content, r.items); e == nil {
							if fs2, _, e := check(cand2); e == nil {
								fs = fs2
							}
						}
					}
					r.findings = added(baseline, fs)
					for _, f := range r.findings {
						if f.Severity == "error" && !personCodes[f.Code] {
							r.problems = append(r.problems, fmt.Sprintf("%s at %s: %s", f.Code, f.Path, f.Message))
						}
					}
				}
			}
		} else {
			r.problems = append(r.problems, r.parseErr)
		}
		if len(r.problems) == 0 || attempt >= MaxAttempts {
			p.Items, p.Gaps, p.Questions, p.Notes, p.Findings, p.Problems, p.Cases, p.Balance = r.items, r.gaps, r.questions, r.notes, r.findings, r.problems, r.cases, r.balance
			p.normalize()
			return p, nil
		}
		msgs = append(msgs, Message{Role: "assistant", Content: raw},
			Message{Role: "user", Content: "The GL engine checked your proposal and found these problems. Fix them and return the complete corrected JSON object, in the same shape:\n- " + strings.Join(r.problems, "\n- ")})
	}
}

func (p *Proposal) normalize() {
	if p.Items == nil {
		p.Items = []Item{}
	}
	for i := range p.Items {
		if p.Items[i].Evidence == nil {
			p.Items[i].Evidence = []Evidence{}
		}
	}
	if p.Gaps == nil {
		p.Gaps = []Gap{}
	}
	if p.Questions == nil {
		p.Questions = []string{}
	}
	if p.Notes == nil {
		p.Notes = []string{}
	}
	if p.Findings == nil {
		p.Findings = []model.Finding{}
	}
	if p.Problems == nil {
		p.Problems = []string{}
	}
	if p.Cases == nil {
		p.Cases = []Case{}
	}
	if p.Balance == nil {
		p.Balance = []Balance{}
	}
}

// added returns the findings in after that were not in before.
func added(before, after []model.Finding) []model.Finding {
	seen := map[string]bool{}
	for _, f := range before {
		seen[f.Code+"|"+f.Path+"|"+f.Message] = true
	}
	out := []model.Finding{}
	for _, f := range after {
		if !seen[f.Code+"|"+f.Path+"|"+f.Message] {
			out = append(out, f)
		}
	}
	return out
}

const systemPrompt = `You help a business analyst configure an insurance General Ledger (GL) from a business requirements document (BRD).
You propose; a person reviews and approves every step, and the GL engine checks everything you propose.
Rules:
- Answer with one JSON object only, exactly in the shape asked for.
- Use what the BRD says, the catalogs and the configuration you are given. Do not invent requirements. When the BRD does not say something you need, add a question to "questions" instead of guessing.
- For every proposal, put in "evidence" the BRD sentences you relied on, copied word for word (short exact quotes, not paraphrases).
- Codes are UPPER_SNAKE_CASE: capital letters, digits and underscores, starting with a letter, at most 64 characters.
- Prefer the standard insurance catalogs. Propose something custom only when nothing in a catalog fits, and say why.
- Never include personal data. Business keys and amounts in test cases are made up.`

func brdBlock(req Request) string {
	b := "\n\nBRD (" + req.BRD.Name + "):\n<<<BRD\n" + req.BRD.Text + "\nBRD>>>"
	if strings.TrimSpace(req.Notes) != "" {
		b += "\n\nThe analyst adds: " + strings.TrimSpace(req.Notes)
	}
	return b
}

// ── content: items are upserted by key ──

// KeyOf is an item's identity within its section.
func KeyOf(section string, v map[string]any) string {
	str := func(k string) string { s, _ := v[k].(string); return s }
	if section == "sourceAuthority" {
		return str("eventType") + "|" + str("measure") + "|" + str("effectiveFrom")
	}
	return str("code")
}

// Apply upserts items into content.
func Apply(content []byte, items []Item) ([]byte, error) {
	m := map[string]any{}
	if len(content) > 0 {
		dec := json.NewDecoder(strings.NewReader(string(content)))
		dec.UseNumber()
		if err := dec.Decode(&m); err != nil {
			return nil, fmt.Errorf("the draft's content could not be read: %v", err)
		}
	}
	for _, it := range items {
		arr, _ := m[it.Section].([]any)
		replaced := false
		for i, x := range arr {
			if xm, ok := x.(map[string]any); ok && KeyOf(it.Section, xm) == it.Key {
				arr[i] = it.Value
				replaced = true
				break
			}
		}
		if !replaced {
			arr = append(arr, it.Value)
		}
		m[it.Section] = arr
	}
	return json.Marshal(m)
}

func toMap(v any) map[string]any {
	b, _ := json.Marshal(v)
	m := map[string]any{}
	_ = json.Unmarshal(b, &m)
	return m
}

// ── evidence ──

var spaces = regexp.MustCompile(`\s+`)

func normText(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("’", "'", "‘", "'", "“", `"`, "”", `"`, "–", "-", "—", "-").Replace(s)
	return strings.TrimSpace(spaces.ReplaceAllString(s, " "))
}

func (s *step) evidence(quotes []string) []Evidence {
	out := []Evidence{}
	for _, q := range quotes {
		q = strings.TrimSpace(q)
		if q == "" {
			continue
		}
		n := strings.Trim(normText(q), ` "'.,;:…`)
		out = append(out, Evidence{Quote: q, Found: len(n) >= 6 && strings.Contains(s.brd, n)})
	}
	return out
}

// ── codes ──

var codeRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

func normCode(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.NewReplacer(" ", "_", "-", "_", ".", "_").Replace(s)
	return s
}

func normCodes(xs []string) []string {
	out := []string{}
	for _, x := range xs {
		if c := normCode(x); codeRe.MatchString(c) {
			out = append(out, c)
		}
	}
	return out
}

func union(lists ...[]string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, l := range lists {
		for _, x := range l {
			if x != "" && !seen[x] {
				seen[x] = true
				out = append(out, x)
			}
		}
	}
	return out
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}

// ── the step ──

type step struct {
	req      Request
	doc      *model.Document
	brd      string
	catalog  model.EventCatalog
	measures map[string]model.Measure
	stdDims  map[string]model.DimensionDef
}

type result struct {
	items     []Item
	gaps      []Gap
	questions []string
	notes     []string
	problems  []string
	findings  []model.Finding
	cases     []Case
	balance   []Balance
	parseErr  string
	testCases []testCase
}

func newStep(req Request, doc *model.Document) *step {
	s := &step{req: req, doc: doc, brd: normText(req.BRD.Text), catalog: model.OOTBEventCatalog(), measures: map[string]model.Measure{}, stdDims: map[string]model.DimensionDef{}}
	for _, m := range model.OOTBMeasures() {
		s.measures[m.Code] = m
	}
	for _, d := range model.OOTBDimensions() {
		s.stdDims[d.Code] = d
	}
	return s
}

func (s *step) task() string {
	switch s.req.Step {
	case StepEvents:
		return taskEvents
	case StepMeasures:
		return taskMeasures
	case StepFormulas:
		return taskFormulas
	default:
		return taskRules
	}
}

func (s *step) context() string {
	var c any
	switch s.req.Step {
	case StepEvents:
		c = s.eventsContext()
	case StepMeasures:
		c = s.measuresContext()
	case StepFormulas:
		c = s.formulasContext()
	default:
		c = s.rulesContext()
	}
	b, _ := json.MarshalIndent(c, "", " ")
	return "Catalogs and the current configuration (JSON):\n" + string(b)
}

func (s *step) build(raw string) result {
	switch s.req.Step {
	case StepEvents:
		return s.buildEvents(raw)
	case StepMeasures:
		return s.buildMeasures(raw)
	case StepFormulas:
		return s.buildFormulas(raw)
	default:
		return s.buildRules(raw)
	}
}

func decode(raw string, v any) string {
	if err := json.Unmarshal([]byte(raw), v); err != nil {
		return "The answer was not the JSON object asked for (" + err.Error() + ")."
	}
	return ""
}

func (s *step) event(code string) *model.EventType {
	for i := range s.doc.Events {
		if s.doc.Events[i].Code == code {
			return &s.doc.Events[i]
		}
	}
	return nil
}

// sourceKindOf is the kind of the source authoritative for all of an event's measures.
func (s *step) sourceKindOf(event string) string {
	for _, a := range s.doc.SourceAuthority {
		if a.EventType == event && a.Measure == model.AllMeasures {
			for _, ss := range s.doc.SourceSystems {
				if ss.Code == a.SourceSystem {
					return ss.Kind
				}
			}
		}
	}
	return ""
}

func (s *step) dimensionConfigured(code string) *model.Dimension {
	for i := range s.doc.Dimensions {
		if s.doc.Dimensions[i].Code == code {
			return &s.doc.Dimensions[i]
		}
	}
	return nil
}

func (s *step) keyConfigured(code string) *model.KeyProfile {
	for i := range s.doc.KeyProfiles {
		if s.doc.KeyProfiles[i].Code == code {
			return &s.doc.KeyProfiles[i]
		}
	}
	return nil
}

func (s *step) classOf(code string) *model.AccountingClass {
	for i := range s.doc.AccountingClasses {
		if s.doc.AccountingClasses[i].Code == code {
			return &s.doc.AccountingClasses[i]
		}
	}
	return nil
}

func (s *step) accountsOf(class string) []string {
	out := []string{}
	for _, a := range s.doc.Accounts {
		if a.Class == class {
			out = append(out, a.Code)
		}
	}
	return out
}

// ── step 1: events ──

const taskEvents = `STEP 1 of 4: identify the business events the BRD describes: each kind of financial event a source system sends that must produce GL postings (for example a policy renewed, an invoice raised, a payment received, a claim paid). Do not propose measures, formulas or accounting yet: later steps do that.
Use a catalog event template whenever one matches; the event code is then the template's code unless the BRD clearly needs another.
Return:
{"events":[{"code":"…","template":"catalog event code, or empty","name":"…","purpose":"one sentence in business words","sourceKind":"POLICY|BILLING|CLAIMS|EARNINGS|REINSURANCE|SOURCE_JOURNAL|CUSTOM","economicKey":["the business keys that identify one such event"],"otherKeys":["other business keys the event carries, for example the broker"],"dimensions":["dimension codes the BRD needs, for example COMPANY, LOB, PRODUCT"],"evidence":["…"],"confidence":"high|medium|low"}],"questions":["…"]}`

func (s *step) eventsContext() any {
	type tmpl struct {
		Code        string   `json:"code"`
		Name        string   `json:"name"`
		Area        string   `json:"area"`
		Purpose     string   `json:"purpose"`
		SourceKind  string   `json:"sourceKind"`
		EconomicKey []string `json:"economicKey"`
		Keys        []string `json:"keys,omitempty"`
		Dimensions  []string `json:"dimensions"`
	}
	ts := []tmpl{}
	for _, t := range s.catalog.Events {
		ts = append(ts, tmpl{t.Code, t.Name, t.Area, t.Purpose, t.SourceKind, t.EconomicKey, t.Keys, t.Dimensions})
	}
	keys := []map[string]string{}
	for _, k := range s.catalog.Keys {
		keys = append(keys, map[string]string{"code": k.Code, "description": k.Description})
	}
	dims := []map[string]string{}
	for _, d := range model.OOTBDimensions() {
		dims = append(dims, map[string]string{"code": d.Code, "name": d.Name})
	}
	evs := []string{}
	for _, e := range s.doc.Events {
		evs = append(evs, e.Code)
	}
	return map[string]any{"catalogEvents": ts, "catalogKeys": keys, "standardDimensions": dims,
		"configuration": map[string]any{"sourceSystems": s.doc.SourceSystems, "eventsAlreadyConfigured": evs}}
}

func (s *step) buildEvents(raw string) result {
	var out struct {
		Events []struct {
			Code        string   `json:"code"`
			Template    string   `json:"template"`
			Name        string   `json:"name"`
			Purpose     string   `json:"purpose"`
			SourceKind  string   `json:"sourceKind"`
			EconomicKey []string `json:"economicKey"`
			OtherKeys   []string `json:"otherKeys"`
			Dimensions  []string `json:"dimensions"`
			Evidence    []string `json:"evidence"`
			Confidence  string   `json:"confidence"`
		} `json:"events"`
		Questions []string `json:"questions"`
	}
	r := result{questions: []string{}}
	if r.parseErr = decode(raw, &out); r.parseErr != "" {
		return r
	}
	r.questions = out.Questions
	templates := map[string]model.EventTemplate{}
	for _, t := range s.catalog.Events {
		templates[t.Code] = t
	}
	sharedKeys := map[string]bool{}
	sharedDims := map[string]bool{}
	for _, e := range out.Events {
		code := normCode(e.Code)
		if !codeRe.MatchString(code) {
			r.notes = append(r.notes, fmt.Sprintf("Dropped event %q: not a valid code.", e.Code))
			continue
		}
		if s.event(code) != nil {
			r.notes = append(r.notes, fmt.Sprintf("%s is already configured in this draft; left as it is.", code))
			continue
		}
		var ev model.EventType
		kind := strings.ToUpper(strings.TrimSpace(e.SourceKind))
		t, fromTemplate := templates[normCode(e.Template)]
		if fromTemplate {
			ev = t.Event()
			ev.Code = code
			if kind == "" {
				kind = t.SourceKind
			}
		} else {
			ev = model.EventType{Code: code}
		}
		ev.Measures = nil // step 2
		for i := range ev.Lists {
			ev.Lists[i].Measures = nil
		}
		if p := strings.TrimSpace(e.Purpose); p != "" {
			ev.Description = p
		}
		other := normCodes(e.OtherKeys)
		if aiKey := normCodes(e.EconomicKey); fromTemplate {
			// The catalog's economic key stands; any further key the AI names is carried as another key.
			for _, k := range aiKey {
				if !contains(ev.EconomicKey, k) {
					other = append(other, k)
				}
			}
		} else {
			ev.EconomicKey = aiKey
		}
		if len(ev.EconomicKey) == 0 {
			r.problems = append(r.problems, fmt.Sprintf("event %s has no economic key: give the business keys that identify one such event", code))
			continue
		}
		// An event that lists its keys must list its economic key too.
		if len(other) > 0 || len(ev.Keys) > 0 {
			ev.Keys = union(ev.EconomicKey, ev.Keys, other)
		}
		dims := []string{}
		for _, d := range union(ev.Dimensions, normCodes(e.Dimensions)) {
			if s.dimensionConfigured(d) != nil || s.stdDims[d].Code != "" {
				dims = append(dims, d)
			} else {
				r.notes = append(r.notes, fmt.Sprintf("%s: dimension %s is neither configured nor standard; left out (add it as a custom dimension in step 6 if needed).", code, d))
			}
		}
		ev.Dimensions = dims
		label := code
		if e.Name != "" {
			label += " · " + e.Name
		}
		why := ""
		if fromTemplate {
			why = "From the insurance catalog template " + t.Code + "."
		} else {
			why = "Custom event: no catalog template matched."
		}
		r.items = append(r.items, Item{ID: "events/" + code, Section: "events", Key: code, Group: code, Change: "add", Label: label, Why: why,
			Confidence: e.Confidence, Evidence: s.evidence(e.Evidence), Value: toMap(ev)})

		if kind != "" && !contains(model.SourceKinds, kind) {
			r.notes = append(r.notes, fmt.Sprintf("%s: source kind %q is not one of %s.", code, kind, strings.Join(model.SourceKinds, ", ")))
			kind = ""
		}
		var src string
		for _, ss := range s.doc.SourceSystems {
			if ss.Kind == kind {
				src = ss.Code
				break
			}
		}
		if src != "" {
			a := model.SourceAuthority{EventType: code, Measure: model.AllMeasures, SourceSystem: src, IntakeMode: "RAW_EVENT"}
			r.items = append(r.items, Item{ID: "sourceAuthority/" + code, Section: "sourceAuthority", Key: code + "|*|", Group: code, Change: "add",
				Label: code + " comes from " + src, Why: "The " + strings.ToLower(kind) + " system is authoritative for all of its measures.", Value: toMap(a)})
		} else {
			r.gaps = append(r.gaps, Gap{Kind: "SOURCE_SYSTEM", Code: kind, Group: code,
				Message: fmt.Sprintf("No %s source system is configured for %s: add one in step 4 (Event details), then set it as the event's source.", strings.ToLower(orDefault(kind, "matching")), code)})
		}

		// Business keys and standard dimensions the event uses that the draft lacks.
		for _, k := range s.catalog.KeysFor(model.EventTemplate{EconomicKey: ev.EconomicKey, Keys: ev.Keys, Lists: ev.Lists}) {
			if s.keyConfigured(k.Code) == nil && !sharedKeys[k.Code] {
				sharedKeys[k.Code] = true
				r.items = append(r.items, Item{ID: "keyProfiles/" + k.Code, Section: "keyProfiles", Key: k.Code, Change: "add", Label: "Business key " + k.Code,
					Why: "Standard business key used by " + code + ".", Value: toMap(k)})
			}
		}
		for _, k := range union(ev.EconomicKey, ev.Keys) {
			if s.keyConfigured(k) == nil && !sharedKeys[k] && !inCatalogKeys(s.catalog, k) {
				sharedKeys[k] = true
				r.items = append(r.items, Item{ID: "keyProfiles/" + k, Section: "keyProfiles", Key: k, Change: "add", Label: "Business key " + k,
					Why: "Custom business key named in the BRD for " + code + ".", Value: map[string]any{"code": k, "description": "From the BRD"}})
			}
		}
		for _, d := range ev.Dimensions {
			if s.dimensionConfigured(d) != nil || sharedDims[d] {
				continue
			}
			sharedDims[d] = true
			r.items = append(r.items, Item{ID: "dimensions/" + d, Section: "dimensions", Key: d, Change: "add", Label: "Dimension " + d,
				Why: "Standard dimension used by " + code + ".", Value: map[string]any{"code": d}})
			if !s.stdDims[d].Open {
				r.gaps = append(r.gaps, Gap{Kind: "DIMENSION_VALUES", Code: d, Name: s.stdDims[d].Name,
					Message: fmt.Sprintf("%s is a closed dimension: enter the client's values for it in step 6 (Dimensions).", d)})
			}
		}
	}
	if len(out.Events) == 0 {
		r.problems = append(r.problems, "no business event was proposed")
	}
	return r
}

func inCatalogKeys(c model.EventCatalog, code string) bool {
	for _, k := range c.Keys {
		if k.Code == code {
			return true
		}
	}
	return false
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// ── step 2: measures ──

const taskMeasures = `STEP 2 of 4: for each business event listed under "events", identify the amounts (measures) its source sends, as the BRD describes. Use catalog measures (by code) wherever the meaning matches; propose a custom measure only when none fits.
Do not list amounts the GL calculates from others (a total, a gross or net amount): those are formulas, in step 3.
Return:
{"events":[{"event":"EVENT_CODE","measures":[{"code":"…","catalog":true,"evidence":["…"]},{"code":"…","catalog":false,"name":"…","definition":"…","dataType":"AMOUNT|QUANTITY|RATE|COUNT","sign":"POSITIVE|NEGATIVE|SIGNED","why":"why no catalog measure fits","evidence":["…"]}]}],"questions":["…"]}`

func (s *step) measuresContext() any {
	evs := []map[string]any{}
	for _, e := range s.doc.Events {
		evs = append(evs, map[string]any{"code": e.Code, "purpose": e.Description, "sourceKind": s.sourceKindOf(e.Code), "measuresSoFar": e.Measures})
	}
	ms := []map[string]any{}
	for _, m := range model.OOTBMeasures() {
		ms = append(ms, map[string]any{"code": m.Code, "name": m.Name, "category": m.Category, "definition": m.Definition, "sourceScope": m.SourceScope})
	}
	custom := []model.BaseMeasure{}
	for _, b := range s.doc.BaseMeasures {
		if b.Custom {
			custom = append(custom, b)
		}
	}
	return map[string]any{"events": evs, "catalogMeasures": ms, "customMeasures": custom}
}

func (s *step) buildMeasures(raw string) result {
	var out struct {
		Events []struct {
			Event    string `json:"event"`
			Measures []struct {
				Code       string   `json:"code"`
				Catalog    bool     `json:"catalog"`
				Name       string   `json:"name"`
				Definition string   `json:"definition"`
				DataType   string   `json:"dataType"`
				Sign       string   `json:"sign"`
				Why        string   `json:"why"`
				Evidence   []string `json:"evidence"`
			} `json:"measures"`
		} `json:"events"`
		Questions []string `json:"questions"`
	}
	r := result{questions: []string{}}
	if r.parseErr = decode(raw, &out); r.parseErr != "" {
		return r
	}
	r.questions = out.Questions
	customAdded := map[string]bool{}
	for _, e := range out.Events {
		code := normCode(e.Event)
		ev := s.event(code)
		if ev == nil {
			r.notes = append(r.notes, fmt.Sprintf("Measures for %s were dropped: the event is not in the draft (approve it in step 1 first).", e.Event))
			continue
		}
		upd := *ev
		upd.Measures = append([]string{}, ev.Measures...)
		var parts []Part
		var all []Evidence
		for _, m := range e.Measures {
			mc := normCode(m.Code)
			if !codeRe.MatchString(mc) {
				r.notes = append(r.notes, fmt.Sprintf("%s: dropped measure %q, not a valid code.", code, m.Code))
				continue
			}
			evd := s.evidence(m.Evidence)
			all = append(all, evd...)
			if c, ok := s.measures[mc]; ok {
				parts = append(parts, Part{Code: mc, Label: c.Name + " (catalog)", Evidence: evd})
			} else {
				isCustom := false
				for _, b := range s.doc.BaseMeasures {
					if b.Code == mc && b.Custom {
						isCustom = true
					}
				}
				if !isCustom && !customAdded[mc] {
					customAdded[mc] = true
					dt := orDefault(strings.ToUpper(m.DataType), "AMOUNT")
					cur := "EVENT"
					if dt != "AMOUNT" {
						cur = "NONE"
					}
					scope := []string{}
					if k := s.sourceKindOf(code); k != "" {
						scope = []string{k}
					}
					bm := model.BaseMeasure{Code: mc, Custom: true, Name: orDefault(strings.TrimSpace(m.Name), mc), Definition: orDefault(strings.TrimSpace(m.Definition), "From the BRD"),
						DataType: dt, Sign: orDefault(strings.ToUpper(m.Sign), "POSITIVE"), Currency: cur, Grain: "EVENT", SourceScope: scope}
					r.items = append(r.items, Item{ID: "baseMeasures/" + mc, Section: "baseMeasures", Key: mc, Group: code, Change: "add",
						Label: "Custom measure " + mc + " · " + bm.Name, Why: orDefault(strings.TrimSpace(m.Why), "No catalog measure fits."), Evidence: evd, Value: toMap(bm)})
				}
				note := "custom"
				if m.Catalog {
					note = "not in the catalog: proposed as custom"
				}
				parts = append(parts, Part{Code: mc, Label: orDefault(m.Name, mc), Note: note, Evidence: evd})
			}
			upd.Measures = union(upd.Measures, []string{mc})
		}
		if len(parts) == 0 {
			continue
		}
		r.items = append(r.items, Item{ID: "events/" + code, Section: "events", Key: code, Group: code, Change: "update",
			Label: code + " carries " + strings.Join(upd.Measures, ", "), Evidence: all, Parts: parts, Value: toMap(upd)})
	}
	if len(r.items) == 0 {
		r.problems = append(r.problems, "no measure was proposed for the events listed")
	}
	return r
}

// ── step 3: formulas ──

const taskFormulas = `STEP 3 of 4: identify the amounts the GL must calculate from the measures (formulas), as the BRD describes: for example the gross amount a policyholder owes (premium plus taxes and duties). Only amounts the accounting needs; never restate a measure.
Formula language: measure, formula and dimension names; numbers; strings in single quotes; + - * / and parentheses; comparisons == != < <= > >=; AND OR NOT; IF(condition, a, b); COALESCE(a, b); ISNULL(x); ROUND(x, places); ABS(x); x IN ('A', 'B'). An AMOUNT formula combines AMOUNT measures.
Return:
{"formulas":[{"code":"…","formula":"…","resultType":"AMOUNT|NUMBER|BOOL|STRING","events":["EVENT_CODE"],"description":"…","evidence":["…"]}],"questions":["…"]}
If the BRD needs no calculated amount, return an empty "formulas" list.`

func (s *step) formulasContext() any {
	evs := []map[string]any{}
	for _, e := range s.doc.Events {
		ms := []map[string]string{}
		for _, m := range e.Measures {
			name := m
			if c, ok := s.measures[m]; ok {
				name = c.Name + ": " + c.Definition
			}
			for _, b := range s.doc.BaseMeasures {
				if b.Code == m && b.Custom {
					name = b.Name + ": " + b.Definition
				}
			}
			ms = append(ms, map[string]string{"code": m, "meaning": name})
		}
		evs = append(evs, map[string]any{"code": e.Code, "purpose": e.Description, "measures": ms, "dimensions": e.Dimensions})
	}
	return map[string]any{"events": evs, "formulasAlready": s.doc.DerivedMeasures}
}

func (s *step) buildFormulas(raw string) result {
	var out struct {
		Formulas []struct {
			Code        string   `json:"code"`
			Formula     string   `json:"formula"`
			ResultType  string   `json:"resultType"`
			Events      []string `json:"events"`
			Description string   `json:"description"`
			Evidence    []string `json:"evidence"`
		} `json:"formulas"`
		Questions []string `json:"questions"`
	}
	r := result{questions: []string{}}
	if r.parseErr = decode(raw, &out); r.parseErr != "" {
		return r
	}
	r.questions = out.Questions
	for _, f := range out.Formulas {
		code := normCode(f.Code)
		if !codeRe.MatchString(code) || strings.TrimSpace(f.Formula) == "" {
			r.problems = append(r.problems, fmt.Sprintf("formula %q needs a valid code and a formula", f.Code))
			continue
		}
		if _, ok := s.measures[code]; ok {
			r.problems = append(r.problems, fmt.Sprintf("formula %s has the code of a catalog measure: choose another code", code))
			continue
		}
		exists := false
		for _, d := range s.doc.DerivedMeasures {
			if d.Code == code {
				exists = true
			}
		}
		if exists {
			r.notes = append(r.notes, code+" is already a formula in this draft; left as it is.")
			continue
		}
		evs := []string{}
		for _, e := range normCodes(f.Events) {
			if s.event(e) != nil {
				evs = append(evs, e)
			}
		}
		dm := model.DerivedMeasure{Code: code, Formula: strings.TrimSpace(f.Formula), ResultType: orDefault(strings.ToUpper(f.ResultType), "AMOUNT"),
			EventTypes: evs, Description: strings.TrimSpace(f.Description)}
		group := ""
		if len(evs) > 0 {
			group = evs[0]
		}
		r.items = append(r.items, Item{ID: "derivedMeasures/" + code, Section: "derivedMeasures", Key: code, Group: group, Change: "add",
			Label: code + " = " + dm.Formula, Why: dm.Description, Evidence: s.evidence(f.Evidence), Value: toMap(dm)})
	}
	return r
}

// ── step 4: debit and credit lines (LHS = RHS) and test cases ──

const taskRules = `STEP 4 of 4: write the accounting. For each business event, one posting rule whose debit and credit lines balance for any amounts: LHS = RHS, the sum of the debit amounts equals the sum of the credit amounts.
Use the accounting classes listed. When the BRD needs a kind of line no listed class fits, propose a new class in "newClasses" (code, name, nature). Never propose account numbers: a person adds them.
A line: {"code":"e.g. AR0_DR","side":"D or C","class":"class code","amount":"a measure or formula name, or an AMOUNT expression","when":"optional BOOL condition; the line is produced only when true","explanation":"in business words"}. Lines that post together (a commission expense and the amount owed to the broker) must carry the same condition, so both sides stay equal.
Also give 2 or 3 synthetic test cases per event with made-up, realistic amounts, covering each condition both ways. Use only the dimension values listed, business key values matching the patterns listed, and the currency given.
Return:
{"rules":[{"event":"…","code":"PR_<EVENT>","description":"…","lines":[…],"evidence":["…"]}],"newClasses":[{"code":"…","name":"…","nature":"ASSET|LIABILITY|EQUITY|INCOME|EXPENSE","normalSide":"D|C","why":"…","evidence":["…"]}],"testCases":[{"event":"…","description":"…","currency":"…","keys":{},"dimensions":{},"measures":{"CODE":"1000.00"}}],"questions":["…"]}`

type testCase struct {
	Event       string            `json:"event"`
	Description string            `json:"description"`
	Currency    string            `json:"currency"`
	Keys        map[string]string `json:"keys"`
	Dimensions  map[string]string `json:"dimensions"`
	Measures    map[string]string `json:"measures"`
}

func (s *step) currency() string {
	count := map[string]int{}
	for _, sc := range s.doc.Scenarios {
		if sc.Input.Currency != "" {
			count[sc.Input.Currency]++
		}
	}
	best, n := "", 0
	for c, k := range count {
		if k > n || (k == n && c < best) {
			best, n = c, k
		}
	}
	return best
}

func (s *step) rulesContext() any {
	hasRule := map[string]bool{}
	for _, r := range s.doc.PostingRules {
		if r.IsActive() {
			hasRule[r.EventType] = true
		}
	}
	evs := []map[string]any{}
	for _, e := range s.doc.Events {
		if hasRule[e.Code] {
			continue
		}
		fs := []map[string]string{}
		for _, d := range s.doc.DerivedMeasures {
			if len(d.EventTypes) == 0 || contains(d.EventTypes, e.Code) {
				fs = append(fs, map[string]string{"code": d.Code, "formula": d.Formula})
			}
		}
		dims := []map[string]any{}
		for _, dc := range e.Dimensions {
			d := map[string]any{"code": dc}
			if cd := s.dimensionConfigured(dc); cd != nil && len(cd.Values) > 0 {
				d["values"] = cd.Values
			}
			dims = append(dims, d)
		}
		keys := []map[string]string{}
		for _, k := range union(e.EconomicKey, e.Keys) {
			kp := map[string]string{"code": k}
			if p := s.keyConfigured(k); p != nil && p.Pattern != "" {
				kp["pattern"] = p.Pattern
			}
			keys = append(keys, kp)
		}
		evs = append(evs, map[string]any{"code": e.Code, "purpose": e.Description, "measures": e.Measures, "formulas": fs, "dimensions": dims, "keys": keys})
	}
	classes := []map[string]any{}
	for _, c := range s.doc.AccountingClasses {
		classes = append(classes, map[string]any{"code": c.Code, "name": c.Name, "nature": c.Nature, "accounts": s.accountsOf(c.Code)})
	}
	cur := s.currency()
	ctx := map[string]any{"events": evs, "accountingClasses": classes}
	if cur != "" {
		ctx["currency"] = cur
	}
	return ctx
}

func (s *step) buildRules(raw string) result {
	var out struct {
		Rules []struct {
			Event       string               `json:"event"`
			Code        string               `json:"code"`
			Description string               `json:"description"`
			Lines       []model.LineTemplate `json:"lines"`
			Evidence    []string             `json:"evidence"`
		} `json:"rules"`
		NewClasses []struct {
			Code       string   `json:"code"`
			Name       string   `json:"name"`
			Nature     string   `json:"nature"`
			NormalSide string   `json:"normalSide"`
			Why        string   `json:"why"`
			Evidence   []string `json:"evidence"`
		} `json:"newClasses"`
		TestCases []testCase `json:"testCases"`
		Questions []string   `json:"questions"`
	}
	r := result{questions: []string{}}
	if r.parseErr = decode(raw, &out); r.parseErr != "" {
		return r
	}
	r.questions = out.Questions
	r.testCases = out.TestCases

	classes := map[string]bool{}
	for _, c := range s.doc.AccountingClasses {
		classes[c.Code] = true
	}
	newClass := map[string]string{}
	for _, c := range out.NewClasses {
		code := normCode(c.Code)
		nature := strings.ToUpper(strings.TrimSpace(c.Nature))
		if !codeRe.MatchString(code) || !contains([]string{"ASSET", "LIABILITY", "EQUITY", "INCOME", "EXPENSE"}, nature) {
			r.problems = append(r.problems, fmt.Sprintf("new class %q needs a valid code and a nature (ASSET, LIABILITY, EQUITY, INCOME or EXPENSE)", c.Code))
			continue
		}
		if classes[code] {
			continue // it exists: lines simply use it
		}
		ac := model.AccountingClass{Code: code, Name: orDefault(strings.TrimSpace(c.Name), code), Nature: nature, Description: strings.TrimSpace(c.Why)}
		if side := strings.ToUpper(strings.TrimSpace(c.NormalSide)); side == "D" || side == "C" {
			ac.NormalSide = side
		}
		classes[code] = true
		newClass[code] = ac.Name
		r.items = append(r.items, Item{ID: "accountingClasses/" + code, Section: "accountingClasses", Key: code, Change: "add",
			Label: "New class " + code + " · " + ac.Name, Why: ac.Description, Evidence: s.evidence(c.Evidence), Value: toMap(ac)})
	}

	ruleCodes := map[string]bool{}
	for _, x := range s.doc.PostingRules {
		ruleCodes[x.Code] = true
	}
	usedClasses := map[string]string{} // class → event
	for _, rl := range out.Rules {
		ev := normCode(rl.Event)
		if s.event(ev) == nil {
			r.notes = append(r.notes, fmt.Sprintf("The rule for %s was dropped: the event is not in the draft.", rl.Event))
			continue
		}
		code := normCode(rl.Code)
		if !codeRe.MatchString(code) {
			code = "PR_" + ev
		}
		if ruleCodes[code] {
			r.problems = append(r.problems, fmt.Sprintf("rule code %s is already used in this draft: choose another", code))
			continue
		}
		ruleCodes[code] = true
		var lines []model.LineTemplate
		var parts []Part
		bal := Balance{Rule: code, Event: ev, LHS: []string{}, RHS: []string{}, Conditional: []string{}}
		bad := false
		for i, l := range rl.Lines {
			l.Code = normCode(l.Code)
			l.Class = normCode(l.Class)
			l.Side = strings.ToUpper(strings.TrimSpace(l.Side))
			l.Amount = strings.TrimSpace(l.Amount)
			l.When = strings.TrimSpace(l.When)
			l.Entity = strings.TrimSpace(l.Entity)
			if !codeRe.MatchString(l.Code) {
				l.Code = fmt.Sprintf("%s_%s%d", l.Class, l.Side, i+1)
			}
			switch {
			case l.Side != "D" && l.Side != "C":
				r.problems = append(r.problems, fmt.Sprintf("rule %s line %s: side must be D or C", code, l.Code))
				bad = true
			case !classes[l.Class]:
				r.problems = append(r.problems, fmt.Sprintf("rule %s line %s uses class %s, which is neither listed nor proposed in newClasses", code, l.Code, l.Class))
				bad = true
			case l.Amount == "":
				r.problems = append(r.problems, fmt.Sprintf("rule %s line %s has no amount", code, l.Code))
				bad = true
			}
			lines = append(lines, l)
			word := "Debit"
			if l.Side == "D" {
				bal.LHS = append(bal.LHS, l.Amount)
			} else {
				word = "Credit"
				bal.RHS = append(bal.RHS, l.Amount)
			}
			note := ""
			if l.When != "" {
				note = "only when " + l.When
				bal.Conditional = append(bal.Conditional, l.Code+" only when "+l.When)
			}
			parts = append(parts, Part{Code: l.Code, Label: fmt.Sprintf("%s %s = %s", word, l.Class, l.Amount), Note: strings.TrimSpace(note + " " + l.Explanation)})
		}
		if len(lines) < 2 {
			r.problems = append(r.problems, fmt.Sprintf("rule %s needs at least one debit and one credit line", code))
			continue
		}
		if bad {
			continue
		}
		for _, l := range lines {
			usedClasses[l.Class] = ev
		}
		pr := model.PostingRule{Code: code, EventType: ev, Rank: 10, Description: strings.TrimSpace(rl.Description), Lines: lines}
		r.items = append(r.items, Item{ID: "postingRules/" + code, Section: "postingRules", Key: code, Group: ev, Change: "add",
			Label: code + " for " + ev, Why: pr.Description, Evidence: s.evidence(rl.Evidence), Parts: parts, Value: toMap(pr)})
		r.balance = append(r.balance, bal)
	}

	// Accounts are a person's to give: every class the lines use that has none.
	cls := make([]string, 0, len(usedClasses))
	for c := range usedClasses {
		cls = append(cls, c)
	}
	sort.Strings(cls)
	for _, c := range cls {
		if len(s.accountsOf(c)) > 0 {
			continue
		}
		name := newClass[c]
		if name == "" {
			if x := s.classOf(c); x != nil {
				name = x.Name
			}
		}
		r.gaps = append(r.gaps, Gap{Kind: "ACCOUNT_NUMBER", Code: c, Name: name, Group: usedClasses[c],
			Message: fmt.Sprintf("Class %s (%s) has no account yet: enter the client's ledger account number for it.", c, name)})
	}
	return r
}

// pendingAccount marks the placeholder a trial run uses for a class whose account a person has
// not entered yet. It exists only in memory, for the run; it is never saved or shown as an account.
const pendingAccount = "PENDING_ACCOUNT_"

// trial is the candidate with a placeholder account for every class that has none, so the
// debits and credits can be computed and compared before a person enters the account numbers.
func trial(doc *model.Document) *model.Document {
	t := *doc
	t.Accounts = append([]model.Account{}, doc.Accounts...)
	t.AssignmentRules = append([]model.AssignmentRule{}, doc.AssignmentRules...)
	for _, c := range doc.AccountingClasses {
		has := false
		for _, a := range doc.Accounts {
			if a.Class == c.Code {
				has = true
			}
		}
		if !has {
			t.Accounts = append(t.Accounts, model.Account{Code: pendingAccount + c.Code, Name: "to be entered", Class: c.Code, ActiveFrom: "1900-01-01"})
			t.AssignmentRules = append(t.AssignmentRules, model.AssignmentRule{Code: "AA_" + pendingAccount + c.Code, Class: c.Code, Account: pendingAccount + c.Code})
		}
	}
	return &t
}

// runCases runs the test cases through the candidate (rules and new classes included),
// checks that each balances, and adds the passing ones as scenarios with their expected
// journal: class, side, amount and company, never an account (a person enters those).
func (s *step) runCases(r *result, canonical []byte) {
	parsed, _ := model.Parse(canonical)
	doc := trial(parsed)
	src := map[string]string{}
	for _, a := range doc.SourceAuthority {
		if a.Measure == model.AllMeasures {
			src[a.EventType] = a.SourceSystem
		}
	}
	codes := map[string]bool{}
	for _, sc := range doc.Scenarios {
		codes[sc.Code] = true
	}
	n := map[string]int{}
	defCur := s.currency()
	for _, tc := range r.testCases {
		ev := normCode(tc.Event)
		hasRule := false
		for _, it := range r.items {
			if it.Section == "postingRules" && it.Group == ev {
				hasRule = true
			}
		}
		if !hasRule {
			continue
		}
		n[ev]++
		code := fmt.Sprintf("%s_AI_%d", ev, n[ev])
		for codes[code] {
			n[ev]++
			code = fmt.Sprintf("%s_AI_%d", ev, n[ev])
		}
		codes[code] = true
		cur := strings.ToUpper(strings.TrimSpace(orDefault(tc.Currency, defCur)))
		in := model.EventInput{EventType: ev, SourceSystem: src[ev], SourceEventID: code, SourceEventVersion: 1, Currency: cur,
			TransactionDate: s.req.EffectiveFrom, EffectiveDate: s.req.EffectiveFrom, Keys: tc.Keys, Dimensions: tc.Dimensions, Measures: tc.Measures}
		c := Case{Code: code, Event: ev, Description: strings.TrimSpace(tc.Description), Currency: cur, Measures: tc.Measures, Lines: []CaseLine{}, Skipped: []string{}}
		plan, err := model.PlanPosting(doc, s.req.Ref, in)
		if err != nil {
			c.Problem = err.Error()
			r.cases = append(r.cases, c)
			r.problems = append(r.problems, fmt.Sprintf("test case %s (%s) cannot run: %s", code, c.Description, err.Error()))
			continue
		}
		var legs []model.ExpectedLeg
		for _, l := range plan.Lines {
			cl := CaseLine{Line: l.Template, Class: l.Class, Side: l.Side, Amount: l.Amount, Entity: l.Entity, Account: l.Account, Problem: l.Problem}
			if strings.HasPrefix(cl.Account, pendingAccount) {
				cl.Account, cl.Problem = "", "account to be entered by a person"
			}
			c.Lines = append(c.Lines, cl)
			legs = append(legs, model.ExpectedLeg{Class: l.Class, Side: l.Side, Amount: l.Amount, Entity: l.Entity})
		}
		c.Skipped = append(c.Skipped, plan.Skipped...)
		c.Balanced = plan.Balanced
		if len(plan.Totals) > 0 {
			c.Debit, c.Credit = plan.Totals[0].Debit, plan.Totals[0].Credit
		}
		if len(plan.Totals) > 1 {
			c.Debit, c.Credit = "", ""
			for _, t := range plan.Totals {
				c.Debit += fmt.Sprintf("%s %s %s; ", t.Entity, t.Currency, t.Debit)
				c.Credit += fmt.Sprintf("%s %s %s; ", t.Entity, t.Currency, t.Credit)
			}
		}
		r.cases = append(r.cases, c)
		if !plan.Balanced {
			r.problems = append(r.problems, fmt.Sprintf("test case %s does not balance: debits %s, credits %s", code, c.Debit, c.Credit))
			continue
		}
		sc := model.Scenario{Code: code, EventType: ev, Description: "Synthetic test from the BRD: " + c.Description, Input: in,
			Expect: &model.ScenarioExpect{Rule: plan.Rule, Legs: legs}}
		v := toMap(sc)
		if input, ok := v["input"].(map[string]any); ok {
			delete(input, "eventType") // the scenario names its event once
		}
		r.items = append(r.items, Item{ID: "scenarios/" + code, Section: "scenarios", Key: code, Group: ev, Change: "add",
			Label: fmt.Sprintf("Test case %s: Dr %s = Cr %s", code, c.Debit, c.Credit), Why: c.Description, Value: v})
	}
}
