package ai

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gl/model"
)

var spec = filepath.Join("..", "..", "..", "..", "..", "..", "acme.all.all.gl.spec")

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(spec, "_harness", "fixtures", "GL-TEST-001.config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if c, ok := m["content"]; ok {
		return c
	}
	return b
}

func sampleBRD(t *testing.T) BRD {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(spec, "_harness", "fixtures", "brd", "motor-renewals-brd.md"))
	if err != nil {
		t.Fatal(err)
	}
	return BRD{Name: "motor-renewals-brd.md", Text: string(b)}
}

// check is the engine's validation, as saving would run it (without the store's schema checks).
func check(content []byte) ([]model.Finding, []byte, error) {
	var m map[string]any
	if err := json.Unmarshal(content, &m); err != nil {
		return nil, nil, err
	}
	canonical, _ := json.Marshal(m)
	return model.ValidateCanonical(canonical), canonical, nil
}

// stub is a stand-in for the provider: it returns prepared answers in order and records what it was asked.
type stub struct {
	answers []string
	calls   [][]Message
}

func (s *stub) Status() Status {
	return Status{Available: true, Provider: "openai", Model: "test-double"}
}

func (s *stub) JSON(_ context.Context, msgs []Message) (string, Usage, error) {
	s.calls = append(s.calls, append([]Message(nil), msgs...))
	if len(s.answers) == 0 {
		return "", Usage{}, errors.New("the stub has no answer left")
	}
	a := s.answers[0]
	s.answers = s.answers[1:]
	return a, Usage{PromptTokens: 1000, CompletionTokens: 200}, nil
}

func item(t *testing.T, p *Proposal, id string) Item {
	t.Helper()
	for _, it := range p.Items {
		if it.ID == id {
			return it
		}
	}
	ids := []string{}
	for _, it := range p.Items {
		ids = append(ids, it.ID)
	}
	t.Fatalf("no item %s; items: %v", id, ids)
	return Item{}
}

func strs(v any) []string {
	out := []string{}
	if a, ok := v.([]any); ok {
		for _, x := range a {
			out = append(out, x.(string))
		}
	}
	return out
}

func run(t *testing.T, step string, content []byte, answers ...string) (*Proposal, *stub) {
	t.Helper()
	d := &stub{answers: answers}
	p, err := RunStep(context.Background(), d, Request{Step: step, BRD: sampleBRD(t), Content: content, EffectiveFrom: "2026-01-01",
		Ref: model.ConfigRef{ConfigID: "GL_TEST_001", Version: 9}}, check)
	if err != nil {
		t.Fatal(err)
	}
	return p, d
}

func apply(t *testing.T, content []byte, items []Item) []byte {
	t.Helper()
	out, err := Apply(content, items)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

const eventsAnswer = `{"events":[{"code":"RENEWAL","template":"RENEWAL","name":"Policy renewed","purpose":"A motor or home policy is renewed for a new term, possibly through a broker.",
 "sourceKind":"POLICY","economicKey":["POLICY_NO","TERM_NO"],"otherKeys":["INTERMEDIARY_ID"],"dimensions":["COMPANY","LOB","PRODUCT"],
 "evidence":["the policy system sends a renewal event identified by the policy number and the term number","Renewals are booked every Friday"],"confidence":"high"}],
 "questions":["Is stamp duty charged on home policies in every state?"]}`

const measuresAnswer = `{"events":[{"event":"RENEWAL","measures":[
 {"code":"PREMIUM","catalog":true,"evidence":["the premium for the new term"]},
 {"code":"TAX","catalog":true,"evidence":["the GST on that premium"]},
 {"code":"STAMP_DUTY","catalog":true,"evidence":["the stamp duty"]},
 {"code":"COMMISSION","catalog":true,"evidence":["the broker commission (zero when there is no broker)"]}]}],"questions":[]}`

const formulasWrong = `{"formulas":[{"code":"GROSS_DUE","formula":"PREMIUM + GST + STAMP_DUTY","resultType":"AMOUNT","events":["RENEWAL"],
 "description":"What the policyholder owes","evidence":["the premium plus GST plus stamp duty"]}],"questions":[]}`

const formulasRight = `{"formulas":[{"code":"GROSS_DUE","formula":"PREMIUM + TAX + STAMP_DUTY","resultType":"AMOUNT","events":["RENEWAL"],
 "description":"What the policyholder owes","evidence":["the premium plus GST plus stamp duty"]}],"questions":[]}`

func rulesAnswer(stampDutyLine bool) string {
	sd := ""
	if stampDutyLine {
		sd = `{"code":"SD0_CR","side":"C","class":"SD0","amount":"STAMP_DUTY","explanation":"Owed to the state revenue office"},`
	}
	return `{"rules":[{"event":"RENEWAL","code":"PR_RENEWAL","description":"Renewal: receivable against premium, GST and stamp duty; broker commission when there is one",
 "lines":[{"code":"AR0_DR","side":"D","class":"AR0","amount":"GROSS_DUE","explanation":"The policyholder owes premium, GST and stamp duty"},
  {"code":"PW0_CR","side":"C","class":"PW0","amount":"PREMIUM","explanation":"Written premium"},
  {"code":"TX0_CR","side":"C","class":"TX0","amount":"TAX","explanation":"GST owed to the tax office"},` + sd + `
  {"code":"CM0_DR","side":"D","class":"CM0","amount":"COMMISSION","when":"COMMISSION > 0","explanation":"Commission expense"},
  {"code":"BP0_CR","side":"C","class":"BP0","amount":"COMMISSION","when":"COMMISSION > 0","explanation":"Owed to the broker"}],
 "evidence":["Debits must equal credits for every journal, per company and currency."]}],
 "newClasses":[{"code":"SD0","name":"Stamp duty payable","nature":"LIABILITY","normalSide":"C","why":"Stamp duty is owed to the state revenue office","evidence":["Stamp duty collected is owed to the state revenue office."]},
  {"code":"CM0","name":"Commission expense","nature":"EXPENSE","why":"Broker commission is an expense","evidence":["Broker commission is an expense of the insurer"]},
  {"code":"BP0","name":"Broker commission payable","nature":"LIABILITY","normalSide":"C","why":"Owed to the broker","evidence":["is owed to the broker"]}],
 "accounts":[{"code":"9999999","class":"SD0"}],
 "testCases":[{"event":"RENEWAL","description":"Motor renewal through a broker","currency":"AUD","keys":{"POLICY_NO":"P1001","TERM_NO":"2","INTERMEDIARY_ID":"B77"},
   "dimensions":{"COMPANY":"C01","LOB":"MOTOR","PRODUCT":"MOTOR_PRIVATE"},"measures":{"PREMIUM":"1000.00","TAX":"100.00","STAMP_DUTY":"90.00","COMMISSION":"150.00"}},
  {"event":"RENEWAL","description":"Home renewal sold directly","currency":"AUD","keys":{"POLICY_NO":"P2002","TERM_NO":"1"},
   "dimensions":{"COMPANY":"C01","LOB":"HOME","PRODUCT":"HOME_BUILDING"},"measures":{"PREMIUM":"800.00","TAX":"80.00","STAMP_DUTY":"40.00","COMMISSION":"0.00"}}],
 "questions":[]}`
}

// The four steps, each approved and applied before the next, take the BRD to a configuration
// that validates once a person has entered the account numbers, with test cases that hold.
func TestStepsTakeABRDToABalancedConfiguration(t *testing.T) {
	content := fixture(t)

	// Step 1: events.
	p, d := run(t, StepEvents, content, eventsAnswer)
	if p.Attempts != 1 || len(p.Problems) != 0 {
		t.Fatalf("events: attempts %d, problems %v", p.Attempts, p.Problems)
	}
	ev := item(t, p, "events/RENEWAL")
	if got := strs(ev.Value["economicKey"]); strings.Join(got, ",") != "POLICY_NO,TERM_NO" {
		t.Fatalf("economic key %v", got)
	}
	if got := strs(ev.Value["keys"]); strings.Join(got, ",") != "POLICY_NO,TERM_NO,INTERMEDIARY_ID" {
		t.Fatalf("an event listing keys must list its economic key too; got %v", got)
	}
	if _, ok := ev.Value["measures"]; ok {
		t.Fatal("measures belong to step 2, not step 1")
	}
	if len(ev.Evidence) != 2 || !ev.Evidence[0].Found || ev.Evidence[1].Found {
		t.Fatalf("evidence must be checked against the BRD: %+v", ev.Evidence)
	}
	if a := item(t, p, "sourceAuthority/RENEWAL"); a.Value["sourceSystem"] != "POLICY" {
		t.Fatalf("source authority %v", a.Value)
	}
	item(t, p, "keyProfiles/INTERMEDIARY_ID")
	if len(p.Questions) != 1 {
		t.Fatalf("questions %v", p.Questions)
	}
	if sys := d.calls[0][0].Content; !strings.Contains(sys, "STEP 1 of 4") || !strings.Contains(d.calls[0][1].Content, "<<<BRD") {
		t.Fatal("the request must carry the step's task and the BRD")
	}
	content = apply(t, content, p.Items)

	// Step 2: measures.
	p, _ = run(t, StepMeasures, content, measuresAnswer)
	if len(p.Problems) != 0 {
		t.Fatalf("measures: %v", p.Problems)
	}
	if got := strs(item(t, p, "events/RENEWAL").Value["measures"]); strings.Join(got, ",") != "PREMIUM,TAX,STAMP_DUTY,COMMISSION" {
		t.Fatalf("measures %v", got)
	}
	content = apply(t, content, p.Items)

	// Step 3: formulas. The first answer names GST, which is not a measure: the engine finds it
	// and the model repairs it.
	p, d = run(t, StepFormulas, content, formulasWrong, formulasRight)
	if p.Attempts != 2 || len(p.Problems) != 0 {
		t.Fatalf("formulas: attempts %d, problems %v", p.Attempts, p.Problems)
	}
	repair := d.calls[1][len(d.calls[1])-1].Content
	if !strings.Contains(repair, "GST") || !strings.Contains(repair, "found these problems") {
		t.Fatalf("the repair request must name the engine's finding: %s", repair)
	}
	if f := item(t, p, "derivedMeasures/GROSS_DUE"); f.Value["formula"] != "PREMIUM + TAX + STAMP_DUTY" {
		t.Fatalf("formula %v", f.Value)
	}
	content = apply(t, content, p.Items)

	// Step 4: debit and credit lines, LHS = RHS, with test cases.
	p, _ = run(t, StepRules, content, rulesAnswer(true))
	if len(p.Problems) != 0 {
		t.Fatalf("rules: %v", p.Problems)
	}
	for _, it := range p.Items {
		if it.Section == "accounts" || it.Section == "accountAssignmentRules" {
			t.Fatalf("the AI may not add accounts or assignment rules: %s", it.ID)
		}
	}
	gaps := []string{}
	for _, g := range p.Gaps {
		gaps = append(gaps, g.Kind+":"+g.Code)
	}
	if strings.Join(gaps, ",") != "ACCOUNT_NUMBER:BP0,ACCOUNT_NUMBER:CM0,ACCOUNT_NUMBER:SD0" {
		t.Fatalf("a person enters the account numbers of new classes; gaps %v", gaps)
	}
	if len(p.Balance) != 1 || strings.Join(p.Balance[0].LHS, " + ") != "GROSS_DUE + COMMISSION" || strings.Join(p.Balance[0].RHS, " + ") != "PREMIUM + TAX + STAMP_DUTY + COMMISSION" {
		t.Fatalf("balance %+v", p.Balance)
	}
	if len(p.Cases) != 2 {
		t.Fatalf("cases %+v", p.Cases)
	}
	for _, c := range p.Cases {
		if !c.Balanced || c.Debit != c.Credit {
			t.Fatalf("case %s does not balance: %+v", c.Code, c)
		}
	}
	if p.Cases[0].Debit != "1340.00" || p.Cases[1].Debit != "920.00" || len(p.Cases[1].Lines) != 4 {
		t.Fatalf("broker case Dr %s, direct case Dr %s with %d lines", p.Cases[0].Debit, p.Cases[1].Debit, len(p.Cases[1].Lines))
	}
	item(t, p, "scenarios/RENEWAL_AI_1")
	item(t, p, "scenarios/RENEWAL_AI_2")
	content = apply(t, content, p.Items)

	// The person enters the account numbers the AI may not invent.
	var m map[string]any
	_ = json.Unmarshal(content, &m)
	for _, a := range [][3]string{{"2200300", "Stamp duty payable", "SD0"}, {"5100100", "Commission expense", "CM0"}, {"2300100", "Broker commission payable", "BP0"}} {
		m["accounts"] = append(m["accounts"].([]any), map[string]any{"code": a[0], "name": a[1], "class": a[2], "activeFrom": "2025-07-01"})
		m["accountAssignmentRules"] = append(m["accountAssignmentRules"].([]any), map[string]any{"code": "AA_" + a[2], "class": a[2], "account": a[0]})
	}
	content, _ = json.Marshal(m)
	fs, canonical, _ := check(content)
	for _, f := range fs {
		if f.Severity == "error" {
			t.Fatalf("after the person's account numbers the configuration must validate: %s %s %s", f.Code, f.Path, f.Message)
		}
	}
	doc, _ := model.Parse(canonical)
	for _, r := range model.RunScenarios(doc, model.ConfigRef{ConfigID: "GL_TEST_001", Version: 9}, []string{"RENEWAL_AI_1", "RENEWAL_AI_2"}) {
		if r.Status != "PASSED" {
			t.Fatalf("test case %s: %s %v", r.Code, r.Status, r.Mismatches)
		}
	}
}

// A rule whose lines do not balance is sent back with the unbalanced test case.
func TestUnbalancedRulesAreRepaired(t *testing.T) {
	content := fixture(t)
	for _, s := range []struct{ step, answer string }{{StepEvents, eventsAnswer}, {StepMeasures, measuresAnswer}, {StepFormulas, formulasRight}} {
		p, _ := run(t, s.step, content, s.answer)
		content = apply(t, content, p.Items)
	}
	p, d := run(t, StepRules, content, rulesAnswer(false), rulesAnswer(true))
	if p.Attempts != 2 || len(p.Problems) != 0 {
		t.Fatalf("attempts %d, problems %v", p.Attempts, p.Problems)
	}
	if repair := d.calls[1][len(d.calls[1])-1].Content; !strings.Contains(repair, "does not balance") {
		t.Fatalf("the repair request must say which test case does not balance: %s", repair)
	}
}

// After MaxAttempts the remaining problems are reported, never hidden.
func TestProblemsLeftAfterTheLastAttemptAreReported(t *testing.T) {
	content := fixture(t)
	p, _ := run(t, StepEvents, content, eventsAnswer)
	content = apply(t, content, p.Items)
	p, _ = run(t, StepMeasures, content, measuresAnswer)
	content = apply(t, content, p.Items)
	p, _ = run(t, StepFormulas, content, formulasWrong, formulasWrong, formulasWrong)
	if p.Attempts != MaxAttempts || len(p.Problems) == 0 {
		t.Fatalf("attempts %d, problems %v", p.Attempts, p.Problems)
	}
}

func TestMeasuresNotInTheCatalogBecomeCustom(t *testing.T) {
	content := fixture(t)
	p, _ := run(t, StepEvents, content, eventsAnswer)
	content = apply(t, content, p.Items)
	p, _ = run(t, StepMeasures, content, `{"events":[{"event":"RENEWAL","measures":[{"code":"PREMIUM","catalog":true},
		{"code":"NO_CLAIM_BONUS","catalog":false,"name":"No-claim bonus","definition":"Discount for claim-free years","dataType":"AMOUNT","sign":"NEGATIVE","why":"No catalog measure is a discount"}]},
		{"event":"NOT_CONFIGURED","measures":[{"code":"PREMIUM","catalog":true}]}]}`)
	b := item(t, p, "baseMeasures/NO_CLAIM_BONUS")
	if b.Value["custom"] != true || b.Value["sign"] != "NEGATIVE" || strings.Join(strs(b.Value["sourceScope"]), ",") != "POLICY" {
		t.Fatalf("custom measure %v", b.Value)
	}
	if len(p.Notes) != 1 || !strings.Contains(p.Notes[0], "NOT_CONFIGURED") {
		t.Fatalf("measures of an unknown event are dropped with a note: %v", p.Notes)
	}
}

func TestNothingIsAskedWhenAIIsNotSetUp(t *testing.T) {
	_, err := RunStep(context.Background(), Load(""), Request{Step: StepEvents, BRD: BRD{Name: "x.md", Text: "x"}, Content: fixture(t)}, check)
	var pe ErrProvider
	if !errors.As(err, &pe) || !strings.Contains(pe.Error(), "not set up") {
		t.Fatalf("err %v", err)
	}
}

func TestSettings(t *testing.T) {
	dir := t.TempDir()
	write := func(body string) string {
		p := filepath.Join(dir, "ai.env")
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := []struct {
		name, body, reason, model string
	}{
		{"no key", "AI_PROVIDER=openai\n", "no OpenAI key", ""},
		{"other provider", "AI_PROVIDER=other\nOPENAI_API_KEY=k\n", "OpenAI only", ""},
		{"switched off", "OPENAI_API_KEY=k\nAI_ENABLED=false\n", "switched off", ""},
		{"ready", "# comment\nOPENAI_API_KEY=\"k-123\"\n", "", DefaultModel},
		{"model", "OPENAI_API_KEY=k\nOPENAI_MODEL=gpt-test\n", "", "gpt-test"},
	}
	for _, c := range cases {
		s := Load(write(c.body))
		st := s.Status()
		if c.reason != "" && (st.Available || !strings.Contains(st.Reason, c.reason)) {
			t.Fatalf("%s: %+v", c.name, st)
		}
		if c.reason == "" && (!st.Available || st.Model != c.model) {
			t.Fatalf("%s: %+v", c.name, st)
		}
		if b, _ := json.Marshal(st); strings.Contains(string(b), "k-123") {
			t.Fatal("the status must never carry the key")
		}
	}
	if s := Load(filepath.Join(dir, "missing.env")); s.Status().Available {
		t.Fatal("a missing file is not set up")
	}
	if s := parse(map[string]string{"OPENAI_API_KEY": "k", "OPENAI_BASE_URL": "http://example.com/v1"}); s.base != "https://api.openai.com/v1" {
		t.Fatalf("only HTTPS or this machine may receive the key; base %s", s.base)
	}
}

func TestOpenAIRequest(t *testing.T) {
	var got struct {
		Auth string
		Body map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Auth = r.Header.Get("Authorization")
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &got.Body)
		if strings.Contains(string(b), "fail") {
			w.WriteHeader(401)
			_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided: k-secret"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"events\":[]}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":12,"completion_tokens":3}}`))
	}))
	defer srv.Close()
	s := parse(map[string]string{"OPENAI_API_KEY": "k-secret", "OPENAI_BASE_URL": srv.URL})
	out, u, err := s.JSON(context.Background(), []Message{{Role: "system", Content: "s"}, {Role: "user", Content: "u"}})
	if err != nil || out != `{"events":[]}` || u.PromptTokens != 12 {
		t.Fatalf("out %q usage %+v err %v", out, u, err)
	}
	if got.Auth != "Bearer k-secret" || got.Body["model"] != DefaultModel || got.Body["response_format"].(map[string]any)["type"] != "json_object" {
		t.Fatalf("request %+v", got)
	}
	_, _, err = s.JSON(context.Background(), []Message{{Role: "user", Content: "fail"}})
	if err == nil || strings.Contains(err.Error(), "k-secret") || !strings.Contains(err.Error(), "HTTP 401") {
		t.Fatalf("errors must be shown without the key: %v", err)
	}
}

func docx(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("word/document.xml")
	_, _ = w.Write([]byte(`<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body + `</w:body></w:document>`))
	_ = zw.Close()
	return buf.Bytes()
}

func TestExtractText(t *testing.T) {
	d := docx(t, `<w:p><w:r><w:t>3.1 Renewal.</w:t></w:r><w:r><w:t xml:space="preserve"> A renewal event</w:t></w:r></w:p>`+
		`<w:tbl><w:tr><w:tc><w:p><w:r><w:t>Premium</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>1,000.00</w:t></w:r></w:p></w:tc></w:tr></w:tbl>`)
	text, err := ExtractText("BRD.docx", d)
	if err != nil || !strings.Contains(text, "3.1 Renewal. A renewal event") || !strings.Contains(text, "Premium | 1,000.00") {
		t.Fatalf("docx: %q %v", text, err)
	}
	if text, err := ExtractText("brd.md", []byte("\xef\xbb\xbf# BRD\r\nline")); err != nil || text != "# BRD\nline" {
		t.Fatalf("md: %q %v", text, err)
	}
	for name, data := range map[string][]byte{"brd.pdf": []byte("%PDF"), "brd.xlsx": []byte("x"), "brd.txt": {}, "brd.docx": []byte("not a zip")} {
		if _, err := ExtractText(name, data); err == nil {
			t.Fatalf("%s must be refused", name)
		}
	}
	if _, err := ExtractText("big.txt", bytes.Repeat([]byte("a"), MaxBRDChars+1)); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("an oversized BRD is refused, never cut short: %v", err)
	}
}
