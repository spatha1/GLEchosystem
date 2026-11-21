package logic

// ALTB-05 — the Ledger, sandbox first. A sandbox book pins one approved configuration version
// and posts synthetic events through the same engine call as a preview (model.RunLifecycle on
// a ledger loaded with the book's posted lines), keeping the C6 journal entries they produced.
// Every book is a sandbox (a database CHECK); nothing here is real financial data. Books,
// postings and lines are append-only: to start again, open a new book.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gl/model"

	modulesdk "github.com/mycelpf/mycel_module_sdk"
)

// BookView is a sandbox book as the API returns it.
type BookView struct {
	BookID        string `json:"bookId"`
	Sandbox       bool   `json:"sandbox"`
	ConfigID      string `json:"configId"`
	ConfigVersion int    `json:"configVersion"`
	ContentDigest string `json:"contentDigest"`
	Reason        string `json:"reason"`
	CreatedBy     string `json:"createdBy"`
	CreatedAt     string `json:"createdAt"`
	Postings      int    `json:"postings"`
}

// PostingView is one source event posted to a book, with its journal entries.
type PostingView struct {
	Seq                int                  `json:"seq"`
	Code               string               `json:"code,omitempty"`
	SourceSystem       string               `json:"sourceSystem"`
	SourceEventID      string               `json:"sourceEventId"`
	SourceEventVersion int                  `json:"sourceEventVersion"`
	EventType          string               `json:"eventType"`
	EconomicEventKey   string               `json:"economicEventKey"`
	BookDate           string               `json:"bookDate"`
	Journals           []model.JournalEntry `json:"journals"`
	PostedBy           string               `json:"postedBy"`
	PostedAt           string               `json:"postedAt"`
}

const bookCols = `b.book_id, b.sandbox::text, b.config_id, b.config_version::text, b.content_digest, b.reason, b.created_by, ` + "%s" +
	`, (select count(*) from sandbox_posting p where p.tenant_id = b.tenant_id and p.book_id = b.book_id)::text`

func scanBook(r modulesdk.Row) (BookView, error) {
	var b BookView
	var sandbox, version, postings string
	if err := scanCells(r, &b.BookID, &sandbox, &b.ConfigID, &version, &b.ContentDigest, &b.Reason, &b.CreatedBy, &b.CreatedAt, &postings); err != nil {
		return b, err
	}
	b.Sandbox = sandbox == "true"
	b.ConfigVersion, _ = strconv.Atoi(version)
	b.Postings, _ = strconv.Atoi(postings)
	return b, nil
}

func loadBook(c *call, bookID string) (BookView, *apiError) {
	rows, err := c.host.Query("select "+fmt.Sprintf(bookCols, ts("b.created_at"))+" from sandbox_book b where b.tenant_id = $1 and b.book_id = $2",
		[]string{c.id.Tenant, bookID})
	if err != nil {
		return BookView{}, errInternal("storage error")
	}
	if len(rows) == 0 {
		return BookView{}, errNotFound("sandbox book " + bookID + " not found")
	}
	b, err := scanBook(rows[0])
	if err != nil {
		return BookView{}, errInternal("storage error")
	}
	return b, nil
}

// listBooks — GET /ledger/sandbox/books.
func listBooks(c *call) modulesdk.Response {
	rows, err := c.host.Query("select "+fmt.Sprintf(bookCols, ts("b.created_at"))+" from sandbox_book b where b.tenant_id = $1 order by b.created_at, b.book_id",
		[]string{c.id.Tenant})
	if err != nil {
		return errInternal("storage error").response()
	}
	books := []BookView{}
	for _, r := range rows {
		b, err := scanBook(r)
		if err != nil {
			return errInternal("storage error").response()
		}
		books = append(books, b)
	}
	return jsonResponse(200, map[string]any{"books": books})
}

// createBook — POST /ledger/sandbox/books {bookId, configId, version, reason}: a new, empty
// sandbox book that posts with one approved configuration version.
func createBook(c *call) modulesdk.Response {
	var b struct {
		BookID   string `json:"bookId"`
		ConfigID string `json:"configId"`
		Version  int    `json:"version"`
		Reason   string `json:"reason"`
	}
	if e := decodeBody(c.req, &b); e != nil {
		return e.response()
	}
	if e := pathCode("bookId", b.BookID); e != nil {
		return e.response()
	}
	if e := pathCode("configId", b.ConfigID); e != nil {
		return e.response()
	}
	if b.Version < 1 {
		return errInvalid("version", "version must be a positive integer").response()
	}
	if e := requireReason(b.Reason); e != nil {
		return e.response()
	}
	v, e := loadVersion(c.host, c.id, b.ConfigID, b.Version, false)
	if e != nil {
		return e.response()
	}
	if v.Status != "approved" {
		return errConflict("CONFIG_NOT_APPROVED", fmt.Sprintf("%s v%d is %s: a sandbox book posts only with an approved version", b.ConfigID, b.Version, v.Status), false).response()
	}
	err := guarded(c.host, func() error {
		_, err := c.host.Exec(`insert into sandbox_book (tenant_id, book_id, config_id, config_version, content_digest, reason, created_by)
			values ($1, $2, $3, $4::integer, $5, $6, $7)`,
			[]string{c.id.Tenant, b.BookID, b.ConfigID, strconv.Itoa(b.Version), v.ContentDigest, b.Reason, c.id.Actor})
		return err
	})
	if err != nil {
		return classifySandbox(err).response()
	}
	book, ae := loadBook(c, b.BookID)
	if ae != nil {
		return ae.response()
	}
	return jsonResponse(201, book)
}

func loadPostings(c *call, bookID string) ([]PostingView, *apiError) {
	rows, err := c.host.Query(`select seq::text, step_code, source_system, source_event_id, source_event_version::text, event_type, economic_event_key,
			book_date::text, journals::text, posted_by, `+ts("posted_at")+`
		  from sandbox_posting where tenant_id = $1 and book_id = $2 order by seq`, []string{c.id.Tenant, bookID})
	if err != nil {
		return nil, errInternal("storage error")
	}
	out := []PostingView{}
	for _, r := range rows {
		var p PostingView
		var seq, version, journals string
		if err := scanCells(r, &seq, &p.Code, &p.SourceSystem, &p.SourceEventID, &version, &p.EventType, &p.EconomicEventKey, &p.BookDate, &journals, &p.PostedBy, &p.PostedAt); err != nil {
			return nil, errInternal("storage error")
		}
		p.Seq, _ = strconv.Atoi(seq)
		p.SourceEventVersion, _ = strconv.Atoi(version)
		if err := json.Unmarshal([]byte(journals), &p.Journals); err != nil {
			return nil, errInternal("storage error")
		}
		out = append(out, p)
	}
	return out, nil
}

func loadLines(c *call, bookID string) ([]model.LedgerLine, *apiError) {
	rows, err := c.host.Query(`select line_id, journal_id, class, account, side, entity, currency, book_date::text, amount::text, keys::text
		  from sandbox_line where tenant_id = $1 and book_id = $2 order by seq, line_id`, []string{c.id.Tenant, bookID})
	if err != nil {
		return nil, errInternal("storage error")
	}
	out := make([]model.LedgerLine, 0, len(rows))
	for _, r := range rows {
		var l model.LedgerLine
		var keys string
		if err := scanCells(r, &l.LineID, &l.JournalID, &l.Class, &l.Account, &l.Side, &l.Entity, &l.Currency, &l.BookDate, &l.Amount, &keys); err != nil {
			return nil, errInternal("storage error")
		}
		if err := json.Unmarshal([]byte(keys), &l.Keys); err != nil {
			return nil, errInternal("storage error")
		}
		out = append(out, l)
	}
	return out, nil
}

// bookDocument loads the configuration version a book pins; it must still be approved and
// unchanged, and valid.
func bookDocument(c *call, b BookView) (*model.Document, model.ConfigRef, *apiError) {
	doc, v, e := loadValidDocument(c, b.ConfigID, b.ConfigVersion)
	if e != nil {
		return nil, model.ConfigRef{}, e
	}
	if v.Status != "approved" {
		return nil, model.ConfigRef{}, errConflict("CONFIG_NOT_APPROVED", fmt.Sprintf("%s v%d is %s: this book can no longer post", b.ConfigID, b.ConfigVersion, v.Status), false)
	}
	if v.ContentDigest != b.ContentDigest {
		return nil, model.ConfigRef{}, errInternal("the pinned configuration changed")
	}
	return doc, model.ConfigRef{ConfigID: b.ConfigID, Version: b.ConfigVersion, ContentDigest: b.ContentDigest}, nil
}

// getBook — GET /ledger/sandbox/books/{bookId}: the book, its postings with their journal
// entries, and its balances.
func getBook(c *call) modulesdk.Response {
	bookID := c.params["bookId"]
	if e := pathCode("bookId", bookID); e != nil {
		return e.response()
	}
	b, e := loadBook(c, bookID)
	if e != nil {
		return e.response()
	}
	postings, e := loadPostings(c, bookID)
	if e != nil {
		return e.response()
	}
	lines, e := loadLines(c, bookID)
	if e != nil {
		return e.response()
	}
	doc, _, e := loadValidDocument(c, b.ConfigID, b.ConfigVersion)
	if e != nil {
		return e.response()
	}
	led := model.NewTempLedger(doc)
	if err := led.Load(lines); err != nil {
		return errInternal("a posted line could not be read").response()
	}
	return jsonResponse(200, map[string]any{"book": b, "postings": postings, "balances": led.Balances()})
}

type postStep struct {
	Scenario string            `json:"scenario,omitempty"`
	Code     string            `json:"code,omitempty"`
	Input    *model.EventInput `json:"input,omitempty"`
}

// postToBook — POST /ledger/sandbox/books/{bookId}/post {steps}: posts synthetic events in
// order. All or nothing: if any event cannot post, nothing is stored and the response says
// which and why. An event already posted with the same content is left unchanged (a resend);
// one posted with different content, or another version of it, is refused.
func postToBook(c *call) modulesdk.Response {
	bookID := c.params["bookId"]
	if e := pathCode("bookId", bookID); e != nil {
		return e.response()
	}
	var body struct {
		Profile string     `json:"profile,omitempty"` // ALTB-07: a lifecycle profile of the pinned version, instead of steps
		Steps   []postStep `json:"steps"`
	}
	if e := decodeBody(c.req, &body); e != nil {
		return e.response()
	}
	if (body.Profile == "") == (len(body.Steps) == 0) {
		return errInvalid("steps", "give steps (scenario codes or synthetic events) or a profile, not both").response()
	}
	if len(body.Steps) > maxLifecycleSteps {
		return errInvalid("steps", fmt.Sprintf("one posting request takes at most %d events", maxLifecycleSteps)).response()
	}
	b, e := loadBook(c, bookID)
	if e != nil {
		return e.response()
	}
	doc, ref, e := bookDocument(c, b)
	if e != nil {
		return e.response()
	}
	events := make([]model.LifecycleEvent, 0, len(body.Steps))
	if body.Profile != "" {
		p, ok := model.FindProfile(doc, body.Profile)
		if !ok {
			return (&apiError{Status: 404, Code: "NOT_FOUND", Message: "profile " + body.Profile + " is not in the configuration this book posts with", Field: "profile"}).response()
		}
		for _, code := range p.Steps {
			body.Steps = append(body.Steps, postStep{Scenario: code})
		}
	}
	for i, s := range body.Steps {
		ev, e := stepEvent(doc, s, i)
		if e != nil {
			return e.response()
		}
		events = append(events, ev)
	}

	// What is already in the book, by source event.
	rows, err := c.host.Query(`select source_system, source_event_id, source_event_version::text, input_digest, coalesce(max(seq) over (), 0)::text
		  from sandbox_posting where tenant_id = $1 and book_id = $2`, []string{c.id.Tenant, bookID})
	if err != nil {
		return errInternal("storage error").response()
	}
	type posted struct {
		version int
		digest  string
	}
	have := map[string]posted{}
	last := 0
	for _, r := range rows {
		var sys, id, version, digest, max string
		if err := scanCells(r, &sys, &id, &version, &digest, &max); err != nil {
			return errInternal("storage error").response()
		}
		n, _ := strconv.Atoi(version)
		have[sys+"/"+id] = posted{n, digest}
		last, _ = strconv.Atoi(max)
	}
	type outcome struct {
		Step   int                  `json:"step"`
		Code   string               `json:"code,omitempty"`
		Status string               `json:"status"` // POSTED | UNCHANGED
		Seq    int                  `json:"seq,omitempty"`
		Detail string               `json:"detail,omitempty"`
		Plan   *model.PostingPlan   `json:"plan,omitempty"`
		Journs []model.JournalEntry `json:"journals,omitempty"`
	}
	outcomes := make([]outcome, len(events))
	var toPost []model.LifecycleEvent
	var at []int
	for i, ev := range events {
		in := ev.Input
		key := in.SourceSystem + "/" + in.SourceEventID
		dig := model.InputDigest(in)
		outcomes[i] = outcome{Step: i + 1, Code: ev.Code}
		if p, ok := have[key]; ok {
			switch {
			case p.version == in.SourceEventVersion && p.digest == dig:
				outcomes[i].Status, outcomes[i].Detail = "UNCHANGED", fmt.Sprintf("%s version %d is already posted in this book with the same content", key, p.version)
				continue
			case p.version == in.SourceEventVersion:
				return (&apiError{Status: 409, Code: "DUPLICATE_EVENT", Message: fmt.Sprintf("step %d: %s version %d is already posted in this book with different content", i+1, key, p.version)}).response()
			default:
				return (&apiError{Status: 409, Code: "CORRECTION_NOT_SUPPORTED", Message: fmt.Sprintf("step %d: %s version %d is posted in this book; posting version %d is a correction (reverse and replace), which is not built yet",
					i+1, key, p.version, in.SourceEventVersion)}).response()
			}
		}
		have[key] = posted{in.SourceEventVersion, dig}
		toPost = append(toPost, ev)
		at = append(at, i)
	}

	lines, e := loadLines(c, bookID)
	if e != nil {
		return e.response()
	}
	led := model.NewTempLedger(doc)
	if err := led.Load(lines); err != nil {
		return errInternal("a posted line could not be read").response()
	}
	res := model.RunLifecycle(doc, ref, toPost, led)
	if !res.Complete && len(toPost) > 0 {
		return (&apiError{Status: 422, Code: "POSTING_REFUSED", Message: "nothing was posted: " + res.Stopped, Detail: res}).response()
	}
	type insert struct {
		seq      int
		ev       model.LifecycleEvent
		plan     *model.PostingPlan
		journals []model.JournalEntry
		lines    []model.LedgerLine
	}
	var ins []insert
	for j, ev := range toPost {
		plan := res.Steps[j].Plan
		js, ls, err := model.Journals(doc, c.id.Tenant, ev.Input, plan)
		if err != nil {
			code := model.ErrorCode(err)
			return (&apiError{Status: 422, Code: "POSTING_REFUSED", Message: fmt.Sprintf("nothing was posted: step %d (%s): %s", at[j]+1, ev.Code, err.Error()), Field: code}).response()
		}
		ins = append(ins, insert{seq: last + j + 1, ev: ev, plan: plan, journals: js, lines: ls})
	}
	err = guarded(c.host, func() error {
		for _, x := range ins {
			jb, err := json.Marshal(x.journals)
			if err != nil {
				return err
			}
			in := x.ev.Input
			if _, err := c.host.Exec(`insert into sandbox_posting (tenant_id, book_id, seq, step_code, source_system, source_event_id, source_event_version,
					input_digest, event_type, economic_event_key, book_date, journals, posted_by)
				values ($1, $2, $3::integer, $4, $5, $6, $7::integer, $8, $9, $10, $11::date, $12::jsonb, $13)`,
				[]string{c.id.Tenant, bookID, strconv.Itoa(x.seq), x.ev.Code, in.SourceSystem, in.SourceEventID, strconv.Itoa(in.SourceEventVersion),
					model.InputDigest(in), in.EventType, model.EconomicEventKey(doc, in), x.plan.BookDate, string(jb), c.id.Actor}); err != nil {
				return err
			}
			for _, l := range x.lines {
				kb, _ := json.Marshal(l.Keys)
				if _, err := c.host.Exec(`insert into sandbox_line (tenant_id, book_id, seq, line_id, journal_id, class, account, side, entity, currency, book_date, amount, keys)
					values ($1, $2, $3::integer, $4, $5, $6, $7, $8, $9, $10, $11::date, $12::numeric, $13::jsonb)`,
					[]string{c.id.Tenant, bookID, strconv.Itoa(x.seq), l.LineID, l.JournalID, l.Class, l.Account, l.Side, l.Entity, l.Currency, l.BookDate, l.Amount, string(kb)}); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return classifySandbox(err).response()
	}
	for j, x := range ins {
		o := &outcomes[at[j]]
		o.Status, o.Seq, o.Plan, o.Journs = "POSTED", x.seq, x.plan, x.journals
	}
	after := model.NewTempLedger(doc)
	all, e := loadLines(c, bookID)
	if e != nil {
		return e.response()
	}
	if err := after.Load(all); err != nil {
		return errInternal("a posted line could not be read").response()
	}
	b, _ = loadBook(c, bookID)
	return jsonResponse(200, map[string]any{"book": b, "steps": outcomes, "posted": len(ins), "balances": after.Balances()})
}

// stepEvent is one step's event: a scenario of the pinned version, or a synthetic event. A
// posting needs the event's dedup identity: sourceSystem, sourceEventId and sourceEventVersion.
func stepEvent(doc *model.Document, s postStep, i int) (model.LifecycleEvent, *apiError) {
	field := fmt.Sprintf("steps/%d", i)
	if (s.Scenario == "") == (s.Input == nil) {
		return model.LifecycleEvent{}, errInvalid(field, "each step names a scenario or gives an input, not both")
	}
	ev := model.LifecycleEvent{Code: s.Code}
	if s.Scenario != "" {
		sc, ok := model.FindScenario(doc, s.Scenario)
		if !ok {
			return ev, &apiError{Status: 404, Code: "NOT_FOUND", Message: "scenario " + s.Scenario + " is not in the configuration this book posts with", Field: field + "/scenario"}
		}
		ev.Input = sc.Input
		ev.Input.EventType = sc.EventType
		ev.Code = firstNonEmpty(s.Code, sc.Code)
	} else {
		ev.Input = *s.Input
	}
	in := ev.Input
	if strings.TrimSpace(in.SourceSystem) == "" || strings.TrimSpace(in.SourceEventID) == "" || in.SourceEventVersion < 1 {
		return ev, errInvalid(field, "a posting needs the event's identity: sourceSystem, sourceEventId and sourceEventVersion ≥ 1")
	}
	return ev, nil
}

// scanCells copies a row's cells into dst, in order.
func scanCells(r modulesdk.Row, dst ...*string) error {
	if len(r.Cells) != len(dst) {
		return fmt.Errorf("row has %d cells, want %d", len(r.Cells), len(dst))
	}
	for i, d := range dst {
		*d = r.Cells[i]
	}
	return nil
}

func classifySandbox(err error) *apiError {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "sandbox_book_pkey"):
		return errConflict("ALREADY_EXISTS", "a sandbox book with this id already exists", false)
	case strings.Contains(msg, "sandbox_posting_once"), strings.Contains(msg, "sandbox_posting_pkey"):
		return errConflict("CONCURRENT_CHANGE", "another posting to this book happened at the same time; retry", true)
	case strings.Contains(msg, "sandbox_line_pkey"):
		return errConflict("DUPLICATE_EVENT", "this economic event's journal is already posted in this book (by another source event)", false)
	case strings.Contains(msg, "GL_SANDBOX_APPEND_ONLY"):
		return errConflict("SANDBOX_APPEND_ONLY", "sandbox postings are never changed; open a new book", false)
	}
	return classify(err)
}
