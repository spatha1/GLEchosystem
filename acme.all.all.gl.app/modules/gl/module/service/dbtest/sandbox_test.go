package dbtest

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type sbLine struct {
	LineID, Account, AccountingClass, Side, Amount string
}
type sbJournal struct {
	JournalID, LegalEntity, Currency, BookingDate, AccountingPeriod, Status string
	ConfigVersion                                                           int
	Lines                                                                   []sbLine
}
type sbBalance struct{ Entity, Currency, Account, Class, Key, Value, Debit, Credit, Balance string }
type sbPosted struct {
	Book struct {
		BookID   string
		Sandbox  bool
		Postings int
	}
	Steps []struct {
		Step     int
		Code     string
		Status   string
		Journals []sbJournal
	}
	Posted   int
	Balances []sbBalance
}

func approvedFixture(t *testing.T, content map[string]any) string {
	t.Helper()
	id := uniq("SBCFG")
	mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "content": content, "reason": "GL-TEST-001"}), 201)
	approve(t, tenantA, id, "1")
	return id
}

func openBook(t *testing.T, config string) string {
	t.Helper()
	book := uniq("BOOK")
	r := do(t, "POST", "/ledger/sandbox/books", tenantA, operator, map[string]any{"bookId": book, "configId": config, "version": 1, "reason": "sandbox run of GL-TEST-001"})
	mustStatus(t, r, 201)
	if !strings.Contains(r.Raw, `"sandbox":true`) {
		t.Fatalf("a book is a sandbox: %s", r.Raw)
	}
	return book
}

func scenarios(codes ...string) map[string]any {
	steps := []map[string]any{}
	for _, c := range codes {
		steps = append(steps, map[string]any{"scenario": c})
	}
	return map[string]any{"steps": steps}
}

// legsOf is a plan's or a journal's lines as comparable text, in a stable order.
func sbLegs(lines []sbLine) []string {
	out := []string{}
	for _, l := range lines {
		out = append(out, l.AccountingClass+" "+l.Side+" "+l.Amount+" "+l.Account)
	}
	sort.Strings(out)
	return out
}

// ALTB-05: posting to a sandbox book goes through the same engine as the lifecycle preview: the
// journals hold exactly the preview's lines and the balances are the preview's balances.
func TestSandboxPostingEqualsTheLifecyclePreview(t *testing.T) {
	needDB(t)
	cfg := approvedFixture(t, fixtureConfig(t))
	book := openBook(t, cfg)

	preview := decode[struct {
		Lifecycle struct {
			Complete bool
			Steps    []struct {
				Plan struct {
					Lines []struct{ Class, Side, Amount, Account string }
				}
				Balances []sbBalance
			}
		}
	}](t, do(t, "POST", "/configs/"+cfg+"/versions/1/lifecycle", tenantA, viewer, scenarios("E1", "E2", "E3")), 200).Lifecycle
	if !preview.Complete {
		t.Fatal("preview incomplete")
	}
	posted := decode[sbPosted](t, do(t, "POST", "/ledger/sandbox/books/"+book+"/post", tenantA, operator, scenarios("E1", "E2", "E3")), 200)
	if posted.Posted != 3 || posted.Book.Postings != 3 || !posted.Book.Sandbox {
		t.Fatalf("posted: %+v", posted)
	}
	for i, s := range posted.Steps {
		var want []sbLine
		for _, l := range preview.Steps[i].Plan.Lines {
			want = append(want, sbLine{AccountingClass: l.Class, Side: l.Side, Amount: l.Amount, Account: l.Account})
		}
		var got []sbLine
		for _, j := range s.Journals {
			if j.Status != "posted" || j.ConfigVersion != 1 || j.AccountingPeriod != "2026-01" {
				t.Errorf("%s: journal header %+v", s.Code, j)
			}
			got = append(got, j.Lines...)
		}
		if s.Status != "POSTED" || !reflect.DeepEqual(sbLegs(got), sbLegs(want)) {
			t.Errorf("%s: posted %v, preview %v", s.Code, sbLegs(got), sbLegs(want))
		}
	}
	if len(posted.Steps[2].Journals) != 2 {
		t.Errorf("E3 posts one entry per legal entity: %d", len(posted.Steps[2].Journals))
	}
	if !reflect.DeepEqual(posted.Balances, preview.Steps[2].Balances) {
		t.Errorf("balances: posted %+v\npreview %+v", posted.Balances, preview.Steps[2].Balances)
	}
	stored := decode[struct {
		Postings []struct {
			Seq      int
			Code     string
			Journals []sbJournal
		}
		Balances []sbBalance
	}](t, do(t, "GET", "/ledger/sandbox/books/"+book, tenantA, viewer, nil), 200)
	if len(stored.Postings) != 3 || stored.Postings[2].Code != "E3" || !reflect.DeepEqual(stored.Balances, posted.Balances) {
		t.Errorf("read back: %d postings, balances equal %v", len(stored.Postings), reflect.DeepEqual(stored.Balances, posted.Balances))
	}
}

// ALTB-05: a resend changes nothing; different content or another version of a posted event is
// refused; an event that cannot post stops the request and nothing of it is stored.
func TestSandboxPostingIsIdempotentAndAllOrNothing(t *testing.T) {
	needDB(t)
	content := fixtureConfig(t)
	cfg := approvedFixture(t, content)
	book := openBook(t, cfg)
	path := "/ledger/sandbox/books/" + book + "/post"
	mustStatus(t, do(t, "POST", path, tenantA, operator, scenarios("E1", "E2")), 200)

	again := decode[sbPosted](t, do(t, "POST", path, tenantA, operator, scenarios("E1")), 200)
	if again.Posted != 0 || again.Steps[0].Status != "UNCHANGED" || again.Book.Postings != 2 {
		t.Errorf("a resend: %+v", again)
	}
	var e1 map[string]any
	for _, s := range content["scenarios"].([]any) {
		if s.(map[string]any)["code"] == "E1" {
			e1 = s.(map[string]any)
		}
	}
	changed := clone(t, e1["input"]).(map[string]any)
	changed["eventType"] = e1["eventType"]
	changed["measures"].(map[string]any)["PREMIUM"] = "2000.00"
	if r := do(t, "POST", path, tenantA, operator, map[string]any{"steps": []any{map[string]any{"input": changed}}}); r.Status != 409 || errCodeOf(r) != "DUPLICATE_EVENT" {
		t.Errorf("same identity, different content: %d %s", r.Status, r.Raw)
	}
	changed["sourceEventVersion"] = 2
	if r := do(t, "POST", path, tenantA, operator, map[string]any{"steps": []any{map[string]any{"input": changed}}}); r.Status != 409 || errCodeOf(r) != "CORRECTION_NOT_SUPPORTED" {
		t.Errorf("a later version (a correction): %d %s", r.Status, r.Raw)
	}
	// E1_HOME could post, but E3_SAME_COMPANY cannot: nothing of the request is stored.
	r := do(t, "POST", path, tenantA, operator, scenarios("E1_HOME", "E3_SAME_COMPANY"))
	if r.Status != 422 || errCodeOf(r) != "POSTING_REFUSED" || !strings.Contains(r.Raw, "ENTITY_NOT_ALLOWED") {
		t.Errorf("a refused event: %d %s", r.Status, r.Raw)
	}
	n := decode[struct{ Postings []any }](t, do(t, "GET", "/ledger/sandbox/books/"+book, tenantA, viewer, nil), 200)
	if len(n.Postings) != 2 {
		t.Errorf("after the refused request the book still has 2 postings: %d", len(n.Postings))
	}
	noID := clone(t, e1["input"]).(map[string]any)
	noID["eventType"] = e1["eventType"]
	delete(noID, "sourceEventId")
	if r := do(t, "POST", path, tenantA, operator, map[string]any{"steps": []any{map[string]any{"input": noID}}}); r.Status != 400 {
		t.Errorf("an event without its identity: %d %s", r.Status, r.Raw)
	}
}

// ALTB-05: later postings read what earlier requests stored: a write-off posted on its own reads
// the 0.50 left on INV2 from the book's lines. (The write-off exists only in this test.)
func TestSandboxPostingReadsTheStoredLedger(t *testing.T) {
	needDB(t)
	c := fixtureConfig(t)
	c["events"] = append(c["events"].([]any), map[string]any{
		"code": "INVOICE_WRITTEN_OFF", "description": "Invoice balance written off", "dimensions": []any{"COMPANY"}, "economicKey": []any{"INVOICE_NO"}})
	c["sourceAuthority"] = append(c["sourceAuthority"].([]any), map[string]any{"eventType": "INVOICE_WRITTEN_OFF", "intakeMode": "RAW_EVENT", "measure": "*", "sourceSystem": "BILLING"})
	c["derivedMeasures"] = append(c["derivedMeasures"].([]any), map[string]any{"code": "WO_AMT", "eventTypes": []any{"INVOICE_WRITTEN_OFF"}, "formula": "OPEN_BALANCE('AR1', 'INVOICE_NO')", "resultType": "AMOUNT"})
	c["postingRules"] = append(c["postingRules"].([]any), map[string]any{"code": "PR_WRITE_OFF", "eventType": "INVOICE_WRITTEN_OFF", "rank": 10, "lines": []any{
		map[string]any{"code": "WO_DR", "class": "WO", "side": "D", "amount": "WO_AMT"},
		map[string]any{"code": "AR1_CR", "class": "AR1", "side": "C", "amount": "WO_AMT"}}})
	cfg := approvedFixture(t, c)
	book := openBook(t, cfg)
	path := "/ledger/sandbox/books/" + book + "/post"
	mustStatus(t, do(t, "POST", path, tenantA, operator, scenarios("E1", "E2", "E3")), 200)
	wo := map[string]any{"eventType": "INVOICE_WRITTEN_OFF", "sourceSystem": "BILLING", "sourceEventId": "WO1", "sourceEventVersion": 1, "currency": "AUD",
		"transactionDate": "2026-01-15", "effectiveDate": "2026-01-15", "keys": map[string]any{"INVOICE_NO": "INV2"}, "dimensions": map[string]any{"COMPANY": "C01"}}
	got := decode[sbPosted](t, do(t, "POST", path, tenantA, operator, map[string]any{"steps": []any{map[string]any{"code": "WO_INV2", "input": wo}}}), 200)
	if got.Posted != 1 || len(got.Steps[0].Journals) != 1 {
		t.Fatalf("write-off: %+v", got)
	}
	if legs := sbLegs(got.Steps[0].Journals[0].Lines); len(legs) != 2 || !strings.Contains(legs[0], " 0.50 ") || !strings.Contains(legs[1], " 0.50 ") {
		t.Errorf("the write-off reads 0.50 left on INV2 from the stored lines: %v", legs)
	}
	for _, b := range got.Balances {
		if b.Account == "9948600" && b.Value == "INV2" && b.Balance != "0.00" {
			t.Errorf("INV2 after the write-off: %+v", b)
		}
	}
}

// ALTB-05: who may do what, tenant isolation, and the database's own invariants.
func TestSandboxBooksAreGuarded(t *testing.T) {
	needDB(t)
	cfg := approvedFixture(t, fixtureConfig(t))
	for _, actor := range []string{viewer, ba, rev, admin} {
		if r := do(t, "POST", "/ledger/sandbox/books", tenantA, actor, map[string]any{"bookId": uniq("BOOK"), "configId": cfg, "version": 1, "reason": "x"}); r.Status != 403 {
			t.Errorf("%s opens a book: %d", actor, r.Status)
		}
	}
	draft := uniq("SBDRAFT")
	mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": draft, "effectiveFrom": "2026-01-01", "content": fixtureConfig(t), "reason": "draft"}), 201)
	if r := do(t, "POST", "/ledger/sandbox/books", tenantA, operator, map[string]any{"bookId": uniq("BOOK"), "configId": draft, "version": 1, "reason": "x"}); r.Status != 409 || errCodeOf(r) != "CONFIG_NOT_APPROVED" {
		t.Errorf("a book on a draft: %d %s", r.Status, r.Raw)
	}
	book := openBook(t, cfg)
	if r := do(t, "POST", "/ledger/sandbox/books", tenantA, operator, map[string]any{"bookId": book, "configId": cfg, "version": 1, "reason": "again"}); r.Status != 409 {
		t.Errorf("the same book id twice: %d", r.Status)
	}
	if r := do(t, "POST", "/ledger/sandbox/books/"+book+"/post", tenantA, ba, scenarios("E1")); r.Status != 403 {
		t.Errorf("an editor posts: %d", r.Status)
	}
	mustStatus(t, do(t, "POST", "/ledger/sandbox/books/"+book+"/post", tenantA, operator, scenarios("E1")), 200)
	if r := do(t, "GET", "/ledger/sandbox/books/"+book, tenantB, viewer, nil); r.Status != 404 {
		t.Errorf("another tenant reads the book: %d", r.Status)
	}
	if r := do(t, "GET", "/ledger/sandbox/books", tenantB, viewer, nil); strings.Contains(r.Raw, book) {
		t.Errorf("another tenant lists the book")
	}
	for _, stmt := range []string{
		"update sandbox_line set amount = 0 where book_id = $1",
		"delete from sandbox_posting where book_id = $1",
		"update sandbox_book set reason = 'changed' where book_id = $1",
	} {
		err := asApp(t, tenantA, func(tx *sql.Tx) error { _, err := tx.Exec(stmt, book); return err })
		if err == nil || !strings.Contains(err.Error(), "GL_SANDBOX_APPEND_ONLY") {
			t.Errorf("%s: %v", stmt, err)
		}
	}
	err := asApp(t, tenantA, func(tx *sql.Tx) error {
		_, err := tx.Exec(`insert into sandbox_book (tenant_id, book_id, sandbox, config_id, config_version, content_digest, reason, created_by)
			select tenant_id, $1, false, config_id, config_version, content_digest, 'real', 'x' from sandbox_book where book_id = $2`, uniq("REAL"), book)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "sandbox_book_sandbox_check") {
		t.Errorf("a book that is not a sandbox: %v", err)
	}
}

func errCodeOf(r result) string { return r.code() }

func clone(t *testing.T, v any) any {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
