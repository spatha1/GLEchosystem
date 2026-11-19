package model

import (
	"reflect"
	"strings"
	"testing"
)

// dateCatalog: P1 with an ENDORSEMENT event, a Sydney calendar and a date policy.
func dateCatalog(policy map[string]any) map[string]any {
	c := p1Catalog("POLICY")
	c["events"] = append(c["events"].([]any), map[string]any{"code": "ENDORSEMENT", "economicKey": []any{"POLICY_NO", "ENDORSEMENT_NO"}, "measures": []any{"PREMIUM"}})
	c["sourceAuthority"] = append(c["sourceAuthority"].([]any), map[string]any{"eventType": "ENDORSEMENT", "measure": "*", "sourceSystem": "POLICY", "intakeMode": "RAW_EVENT"})
	c["calendars"] = []any{map[string]any{"code": "AU_SYD", "timezone": "Australia/Sydney", "weekend": []any{"SAT", "SUN"},
		"holidays": []any{"2026-01-26", "2026-12-25", "2026-12-28", "2027-01-01"}}}
	p := map[string]any{"code": "DEFAULT", "calendar": "AU_SYD", "bookDate": "TRANSACTION_DATE", "advance": true,
		"fiscalYearStartMonth": 7, "closeAfterBusinessDay": 3, "latePolicy": "NEXT_OPEN_PERIOD"}
	for k, v := range policy {
		p[k] = v
	}
	c["datePolicies"] = []any{p}
	return c
}

func decide(t *testing.T, doc *Document, txn, eff, asOf string) PeriodDecision {
	t.Helper()
	d, err := DecidePeriod(doc, "ENDORSEMENT", EventDates{TransactionDate: txn, EffectiveDate: eff}, day(asOf))
	if err != nil {
		t.Fatalf("%s/%s as of %s: %v", txn, eff, asOf, err)
	}
	return d
}

// AC: a 20-Jan endorsement effective 01-Feb is classified advance under the demo period rule.
func TestAdvanceClassificationForTheDemo(t *testing.T) {
	c := withDerived(dateCatalog(nil), map[string]any{"code": "ADVANCE_PREMIUM", "formula": "IF(BOOKING_CLASS == 'A', PREMIUM, 0)", "resultType": "AMOUNT", "eventTypes": []any{"ENDORSEMENT"}})
	doc, fs := parse(t, c)
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("%+v", e)
	}
	d := decide(t, doc, "2026-01-20", "2026-02-01", "2026-01-20")
	if d.BookingClass != "A" || d.BookDate != "2026-01-20" || d.AccountingPeriod != "2026-01" {
		t.Fatalf("E5: %+v", d)
	}
	if !strings.Contains(strings.Join(d.Steps, "|"), "advance (A)") {
		t.Errorf("the decision must say why: %v", d.Steps)
	}
	if cur := decide(t, doc, "2026-01-01", "2026-01-01", "2026-01-01"); cur.BookingClass != "C" {
		t.Errorf("E1 is current: %+v", cur)
	}
	if cur := decide(t, doc, "2026-01-20", "2026-01-31", "2026-01-20"); cur.BookingClass != "C" {
		t.Errorf("effective on the last day of the period is current: %+v", cur)
	}
	got, err := EvaluateEvent(doc, ref, EventInput{EventType: "ENDORSEMENT", Currency: "AUD", TransactionDate: "2026-01-20", EffectiveDate: "2026-02-01", Measures: map[string]string{"PREMIUM": "200.00"}})
	if err != nil {
		t.Fatal(err)
	}
	if v := byCode(got.Derived)["ADVANCE_PREMIUM"].Value; v != "200 AUD" {
		t.Errorf("rules can read BOOKING_CLASS: %s", v)
	}
	var bc ResolvedDimension
	for _, r := range got.Dimensions {
		if r.Dimension == "BOOKING_CLASS" {
			bc = r
		}
	}
	if bc.Value != "A" || bc.Via != "DATE_POLICY" || bc.Lookup != "DEFAULT" || got.Period == nil {
		t.Errorf("booking class lineage: %+v, period %+v", bc, got.Period)
	}
}

// AC: posting (book) and effective dates are kept separately — never merged.
func TestPostingAndEffectiveDatesStaySeparate(t *testing.T) {
	c := withDerived(dateCatalog(nil),
		map[string]any{"code": "LEAD_DAYS_POSITIVE", "formula": "EFFECTIVE_DATE > BOOK_DATE", "resultType": "BOOL", "eventTypes": []any{"ENDORSEMENT"}},
		map[string]any{"code": "PERIOD", "formula": "ACCOUNTING_PERIOD", "resultType": "STRING", "eventTypes": []any{"ENDORSEMENT"}})
	doc, fs := parse(t, c)
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("%+v", e)
	}
	got, err := EvaluateEvent(doc, ref, EventInput{EventType: "ENDORSEMENT", Currency: "AUD", TransactionDate: "2026-01-20", EffectiveDate: "2026-02-01", Measures: map[string]string{"PREMIUM": "200.00"}})
	if err != nil {
		t.Fatal(err)
	}
	p := got.Period
	if p.BookDate != "2026-01-20" || p.EffectiveDate != "2026-02-01" || p.TransactionDate != "2026-01-20" || p.AccountingPeriod != "2026-01" {
		t.Errorf("dates merged or lost: %+v", p)
	}
	m := byCode(got.Derived)
	if m["LEAD_DAYS_POSITIVE"].Value != "true" || m["PERIOD"].Value != "'2026-01'" {
		t.Errorf("formulas see both dates: %+v", m)
	}
}

// AC: closed-period decisions are deterministic and auditable — the same inputs always give
// the same decision, and the steps explain it; a late event never changes the closed period.
func TestClosedPeriodDecisionsAreDeterministicAndExplained(t *testing.T) {
	doc, _ := parse(t, dateCatalog(nil))
	// January closes after the 3rd business day after 31 Jan: Mon 2, Tue 3, Wed 4 February.
	open := decide(t, doc, "2026-01-20", "2026-01-20", "2026-02-04")
	if open.AccountingPeriod != "2026-01" || open.LateFromPeriod != "" {
		t.Errorf("January is still open on 4 Feb: %+v", open)
	}
	late := decide(t, doc, "2026-01-20", "2026-01-20", "2026-02-05")
	if late.AccountingPeriod != "2026-02" || late.BookDate != "2026-02-01" || late.LateFromPeriod != "2026-01" {
		t.Fatalf("a January event processed on 5 Feb books into February: %+v", late)
	}
	steps := strings.Join(late.Steps, "|")
	if !strings.Contains(steps, "closed after 2026-02-04") || !strings.Contains(steps, "the closed period is not changed") {
		t.Errorf("the decision must explain itself: %v", late.Steps)
	}
	for i := 0; i < 20; i++ {
		if again := decide(t, doc, "2026-01-20", "2026-01-20", "2026-02-05"); !reflect.DeepEqual(again, late) {
			t.Fatalf("run %d differs: %+v", i, again)
		}
	}
	// Discovered on 1 May: April is still open (it closes after the 3rd business day after
	// 30 April — Fri 1, Mon 4, Tue 5 May), so the event books into April; on 6 May, into May.
	if far := decide(t, doc, "2026-01-20", "2026-01-20", "2026-05-01"); far.AccountingPeriod != "2026-04" {
		t.Errorf("first open period on 1 May: %+v", far)
	}
	if far := decide(t, doc, "2026-01-20", "2026-01-20", "2026-05-06"); far.AccountingPeriod != "2026-05" {
		t.Errorf("first open period on 6 May: %+v", far)
	}
	// Under a REJECT policy the same late event is an explicit CLOSED_PERIOD error.
	strict, _ := parse(t, dateCatalog(map[string]any{"latePolicy": "REJECT"}))
	if _, err := DecidePeriod(strict, "ENDORSEMENT", EventDates{TransactionDate: "2026-01-20", EffectiveDate: "2026-01-20"}, day("2026-02-05")); err == nil || !strings.Contains(err.Error(), "CLOSED_PERIOD") {
		t.Errorf("REJECT: %v", err)
	}
}

// Tests: month, year and fiscal-year boundaries.
func TestMonthYearAndFiscalBoundaries(t *testing.T) {
	doc, _ := parse(t, dateCatalog(nil))
	nye := decide(t, doc, "2026-12-31", "2027-01-01", "2026-12-31")
	if nye.AccountingPeriod != "2026-12" || nye.BookingClass != "A" || nye.FiscalYear != 2027 || nye.FiscalPeriod != 6 {
		t.Errorf("31 Dec effective 1 Jan: %+v", nye)
	}
	for _, k := range []struct {
		date       string
		fy, period int
	}{{"2026-06-30", 2026, 12}, {"2026-07-01", 2027, 1}, {"2027-01-15", 2027, 7}} {
		d := decide(t, doc, k.date, k.date, k.date)
		if d.FiscalYear != k.fy || d.FiscalPeriod != k.period {
			t.Errorf("%s: FY%d P%02d, want FY%d P%02d", k.date, d.FiscalYear, d.FiscalPeriod, k.fy, k.period)
		}
	}
	cal, _ := parse(t, dateCatalog(map[string]any{"fiscalYearStartMonth": 0}))
	if d := decide(t, cal, "2026-12-31", "2026-12-31", "2026-12-31"); d.FiscalYear != 2026 || d.FiscalPeriod != 12 {
		t.Errorf("calendar fiscal year: %+v", d)
	}
}

// Tests: holidays and weekends roll the book date to the next business day when configured.
func TestHolidaysAndWeekends(t *testing.T) {
	doc, _ := parse(t, dateCatalog(map[string]any{"businessDayRule": "NEXT_BUSINESS_DAY"}))
	for txn, want := range map[string]string{
		"2026-01-26": "2026-01-27", // Australia Day (Monday holiday)
		"2026-01-31": "2026-02-02", // Saturday rolls into the next month
		"2026-12-25": "2026-12-29", // Christmas Fri, weekend, Boxing Day substitute Mon
		"2026-01-20": "2026-01-20", // ordinary Tuesday
	} {
		if d := decide(t, doc, txn, txn, txn); d.BookDate != want {
			t.Errorf("%s booked %s, want %s", txn, d.BookDate, want)
		}
	}
	if d := decide(t, doc, "2026-01-31", "2026-01-31", "2026-01-31"); d.AccountingPeriod != "2026-02" || d.BookingClass != "C" {
		t.Errorf("a Saturday 31 Jan rolled to 2 Feb is a February booking: %+v", d)
	}
}

// Tests: time zones — a recorded instant becomes the business date in the calendar's zone.
func TestRecordedInstantsUseTheCalendarTimezone(t *testing.T) {
	doc, fs := parse(t, dateCatalog(map[string]any{"bookDate": "RECORDED_DATE"}))
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("%+v", e)
	}
	for at, want := range map[string]string{
		"2026-01-31T14:30:00Z": "2026-02-01", // 01:30 on 1 Feb in Sydney (UTC+11, daylight saving)
		"2026-01-31T12:00:00Z": "2026-01-31", // 23:00 on 31 Jan in Sydney
		"2026-07-31T14:30:00Z": "2026-08-01", // 00:30 on 1 Aug in Sydney (UTC+10, standard time)
		"2026-07-31T13:30:00Z": "2026-07-31", // 23:30 on 31 Jul in Sydney
	} {
		d, err := DecidePeriod(doc, "ENDORSEMENT", EventDates{TransactionDate: "2026-01-01", EffectiveDate: "2026-01-01", RecordedAt: at}, day("2026-01-01"))
		if err != nil || d.BookDate != want {
			t.Errorf("%s → %s (%v), want %s", at, d.BookDate, err, want)
		}
	}
	if _, err := DecidePeriod(doc, "ENDORSEMENT", EventDates{TransactionDate: "2026-01-01", EffectiveDate: "2026-01-01"}, day("2026-01-01")); err == nil {
		t.Error("a recorded-date policy without recordedAt must be an error")
	}
}

// The policy decides which date selects effective-dated lookups.
func TestLookupDateFollowsThePolicy(t *testing.T) {
	for which, want := range map[string]string{"EFFECTIVE_DATE": "C09", "TRANSACTION_DATE": "C01"} {
		c := dimCatalog("C09")
		c["calendars"] = []any{map[string]any{"code": "AU_SYD", "timezone": "Australia/Sydney"}}
		c["datePolicies"] = []any{map[string]any{"code": "DEFAULT", "calendar": "AU_SYD", "bookDate": "TRANSACTION_DATE", "lookupDate": which}}
		doc, fs := parse(t, c)
		if e := errorsOf(fs); len(e) > 0 {
			t.Fatalf("%+v", e)
		}
		in := payment("BANK-OLD", "C01", "")
		in.TransactionDate, in.EffectiveDate = "2026-02-28", "2026-03-01" // BANK-OLD moves to C09 on 1 March
		got, err := EvaluateEvent(doc, ref, in)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range got.Dimensions {
			if r.Dimension == "BANKING_COMPANY" && r.Value != want {
				t.Errorf("lookupDate %s: %s, want %s", which, r.Value, want)
			}
		}
	}
}

func TestDatePolicyValidation(t *testing.T) {
	cases := map[string]struct {
		mutate func(c map[string]any)
		code   string
	}{
		"unknown calendar": {func(c map[string]any) { c["datePolicies"].([]any)[0].(map[string]any)["calendar"] = "NOPE" }, "UNKNOWN_CALENDAR"},
		"bad timezone":     {func(c map[string]any) { c["calendars"].([]any)[0].(map[string]any)["timezone"] = "Mars/Olympus" }, "INVALID_TIMEZONE"},
		"local timezone":   {func(c map[string]any) { c["calendars"].([]any)[0].(map[string]any)["timezone"] = "Local" }, "INVALID_TIMEZONE"},
		"bad weekday":      {func(c map[string]any) { c["calendars"].([]any)[0].(map[string]any)["weekend"] = []any{"SATURDAY"} }, "INVALID_WEEKDAY"},
		"bad holiday":      {func(c map[string]any) { c["calendars"].([]any)[0].(map[string]any)["holidays"] = []any{"2026-02-30"} }, "INVALID_HOLIDAY"},
		"bad book date":    {func(c map[string]any) { c["datePolicies"].([]any)[0].(map[string]any)["bookDate"] = "POSTING_DATE" }, "INVALID_BOOK_DATE"},
		"bad late policy":  {func(c map[string]any) { c["datePolicies"].([]any)[0].(map[string]any)["latePolicy"] = "REOPEN" }, "INVALID_RULE"},
		"two defaults": {func(c map[string]any) {
			c["datePolicies"] = append(c["datePolicies"].([]any), map[string]any{"code": "OTHER", "calendar": "AU_SYD", "bookDate": "EFFECTIVE_DATE"})
		}, "AMBIGUOUS_DATE_POLICY"},
		"event in two policies": {func(c map[string]any) {
			c["datePolicies"] = []any{
				map[string]any{"code": "P1", "calendar": "AU_SYD", "bookDate": "TRANSACTION_DATE", "eventTypes": []any{"ENDORSEMENT"}},
				map[string]any{"code": "P2", "calendar": "AU_SYD", "bookDate": "EFFECTIVE_DATE", "eventTypes": []any{"ENDORSEMENT"}},
			}
		}, "AMBIGUOUS_DATE_POLICY"},
		"event without a policy": {func(c map[string]any) {
			c["datePolicies"] = []any{map[string]any{"code": "P1", "calendar": "AU_SYD", "bookDate": "TRANSACTION_DATE", "eventTypes": []any{"ENDORSEMENT"}}}
		}, "MISSING_DATE_POLICY"},
		"unknown event": {func(c map[string]any) { c["datePolicies"].([]any)[0].(map[string]any)["eventTypes"] = []any{"NOPE"} }, "UNKNOWN_EVENT"},
	}
	for name, k := range cases {
		c := dateCatalog(nil)
		k.mutate(c)
		if _, fs := parse(t, c); !hasCode(errorsOf(fs), k.code) {
			t.Errorf("%s: want %s, got %+v", name, k.code, errorsOf(fs))
		}
	}
	if _, fs := parse(t, dateCatalog(nil)); len(errorsOf(fs)) != 0 {
		t.Errorf("the demo policy must validate: %+v", errorsOf(fs))
	}
}
