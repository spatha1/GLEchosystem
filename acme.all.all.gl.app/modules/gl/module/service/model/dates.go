package model

import (
	"fmt"
	"strings"
	"time"
	_ "time/tzdata" // the IANA time-zone database is embedded: no dependency on the host OS image

	"gl/formula"
)

// US-009 — business dates, calendars and accounting periods.
//
// An event carries several dates that are never merged: the transaction date (when it
// happened at source), the effective date (when the cover or change takes effect), and the
// recording instant. A date policy decides which date books the event, into which accounting
// period, whether it is an advance (effective after that period) or current, and what
// happens when its natural period is already closed. The decision is a pure function of the
// event's dates, the policy and the processing date, and returns its reasoning step by step.

// Calendar defines business days in a time zone.
type Calendar struct {
	Code     string   `json:"code"`
	Timezone string   `json:"timezone"`          // IANA, e.g. Australia/Sydney
	Weekend  []string `json:"weekend,omitempty"` // MON … SUN
	Holidays []string `json:"holidays,omitempty"`
}

// DatePolicy is how events are dated and booked.
type DatePolicy struct {
	Code                  string   `json:"code"`
	EventTypes            []string `json:"eventTypes,omitempty"` // empty = the default policy
	Calendar              string   `json:"calendar"`
	BookDate              string   `json:"bookDate"`                  // TRANSACTION_DATE | EFFECTIVE_DATE | RECORDED_DATE
	BusinessDayRule       string   `json:"businessDayRule,omitempty"` // NONE (default) | NEXT_BUSINESS_DAY
	FiscalYearStartMonth  int      `json:"fiscalYearStartMonth,omitempty"`
	Advance               bool     `json:"advance,omitempty"`               // classify A when effective after the book period
	LookupDate            string   `json:"lookupDate,omitempty"`            // TRANSACTION_DATE | EFFECTIVE_DATE (default)
	CloseAfterBusinessDay int      `json:"closeAfterBusinessDay,omitempty"` // a period closes after its Nth business day following month end
	LatePolicy            string   `json:"latePolicy,omitempty"`            // NEXT_OPEN_PERIOD (default) | REJECT
}

var (
	weekdays    = map[string]time.Weekday{"SUN": time.Sunday, "MON": time.Monday, "TUE": time.Tuesday, "WED": time.Wednesday, "THU": time.Thursday, "FRI": time.Friday, "SAT": time.Saturday}
	bookDates   = []string{"TRANSACTION_DATE", "EFFECTIVE_DATE", "RECORDED_DATE"}
	lookupDates = []string{"TRANSACTION_DATE", "EFFECTIVE_DATE"}
)

func init() {
	validators = append(validators, validateDates)
	symbolProviders = append(symbolProviders, dateSymbols)
}

// dateSymbols: every event's dates are readable in formulas; BOOKING_CLASS and
// ACCOUNTING_PERIOD when a date policy applies.
func dateSymbols(d *Document, e EventType, s *formula.Symbols) {
	s.Fields["TRANSACTION_DATE"] = formula.KDate
	s.Fields["EFFECTIVE_DATE"] = formula.KDate
	if p, ok := policyFor(d, e.Code); ok {
		s.Fields["BOOK_DATE"] = formula.KDate
		s.Fields["ACCOUNTING_PERIOD"] = formula.KString
		if p.Advance {
			s.Fields["BOOKING_CLASS"] = formula.KString
		}
	}
}

// policyFor returns the date policy that applies to an event type: one naming it, else the
// default (no eventTypes).
func policyFor(d *Document, eventType string) (DatePolicy, bool) {
	var def *DatePolicy
	for i, p := range d.DatePolicies {
		if len(p.EventTypes) == 0 {
			def = &d.DatePolicies[i]
			continue
		}
		if oneOf(eventType, p.EventTypes) {
			return p, true
		}
	}
	if def != nil {
		return *def, true
	}
	return DatePolicy{}, false
}

func validateDates(d *Document) []Finding {
	var fs []Finding
	cals := map[string]bool{}
	for i, c := range d.Calendars {
		p := fmt.Sprintf("calendars/%d", i)
		if !isCode(c.Code) || cals[c.Code] {
			fs = append(fs, errf(p, "INVALID_CODE", "calendar code %q must be a unique upper-case code", c.Code))
		}
		cals[c.Code] = true
		if _, err := time.LoadLocation(c.Timezone); err != nil || c.Timezone == "" || strings.EqualFold(c.Timezone, "Local") {
			fs = append(fs, errf(p, "INVALID_TIMEZONE", "timezone %q must be an IANA zone such as Australia/Sydney", c.Timezone))
		}
		for _, w := range c.Weekend {
			if _, ok := weekdays[w]; !ok {
				fs = append(fs, errf(p, "INVALID_WEEKDAY", "weekend day %q must be one of MON TUE WED THU FRI SAT SUN", w))
			}
		}
		seen := map[string]bool{}
		for _, h := range c.Holidays {
			if _, ok := ParseDay(h); !ok || seen[h] {
				fs = append(fs, errf(p, "INVALID_HOLIDAY", "holiday %q must be a unique real date (YYYY-MM-DD)", h))
			}
			seen[h] = true
		}
	}
	events := map[string]bool{}
	for _, e := range d.Events {
		events[e.Code] = true
	}
	covered := map[string]string{}
	defaults := 0
	for i, pol := range d.DatePolicies {
		p := fmt.Sprintf("datePolicies/%d", i)
		if !isCode(pol.Code) {
			fs = append(fs, errf(p, "INVALID_CODE", "date policy code %q must be an upper-case code", pol.Code))
		}
		if !cals[pol.Calendar] {
			fs = append(fs, errf(p, "UNKNOWN_CALENDAR", "date policy %s uses calendar %q, which is not configured", pol.Code, pol.Calendar))
		}
		if !oneOf(pol.BookDate, bookDates) {
			fs = append(fs, errf(p, "INVALID_BOOK_DATE", "bookDate %q must be one of %v", pol.BookDate, bookDates))
		}
		if pol.BusinessDayRule != "" && pol.BusinessDayRule != "NONE" && pol.BusinessDayRule != "NEXT_BUSINESS_DAY" {
			fs = append(fs, errf(p, "INVALID_RULE", "businessDayRule %q must be NONE or NEXT_BUSINESS_DAY", pol.BusinessDayRule))
		}
		if pol.FiscalYearStartMonth < 0 || pol.FiscalYearStartMonth > 12 {
			fs = append(fs, errf(p, "INVALID_RULE", "fiscalYearStartMonth must be 1–12"))
		}
		if pol.LookupDate != "" && !oneOf(pol.LookupDate, lookupDates) {
			fs = append(fs, errf(p, "INVALID_RULE", "lookupDate %q must be one of %v", pol.LookupDate, lookupDates))
		}
		if pol.CloseAfterBusinessDay < 0 || pol.CloseAfterBusinessDay > 30 {
			fs = append(fs, errf(p, "INVALID_RULE", "closeAfterBusinessDay must be 0–30"))
		}
		if pol.LatePolicy != "" && pol.LatePolicy != "NEXT_OPEN_PERIOD" && pol.LatePolicy != "REJECT" {
			fs = append(fs, errf(p, "INVALID_RULE", "latePolicy %q must be NEXT_OPEN_PERIOD or REJECT", pol.LatePolicy))
		}
		if len(pol.EventTypes) == 0 {
			defaults++
			if defaults > 1 {
				fs = append(fs, errf(p, "AMBIGUOUS_DATE_POLICY", "more than one default date policy (without eventTypes)"))
			}
		}
		for _, ev := range pol.EventTypes {
			if !events[ev] {
				fs = append(fs, errf(p, "UNKNOWN_EVENT", "date policy %s names event %s, which is not in the catalog", pol.Code, ev))
			}
			if prev, dup := covered[ev]; dup {
				fs = append(fs, errf(p, "AMBIGUOUS_DATE_POLICY", "event %s is governed by both %s and %s", ev, prev, pol.Code))
			}
			covered[ev] = pol.Code
		}
	}
	if len(d.DatePolicies) > 0 {
		for i, e := range d.Events {
			if _, ok := policyFor(d, e.Code); !ok {
				fs = append(fs, errf(fmt.Sprintf("events/%d", i), "MISSING_DATE_POLICY", "no date policy governs event %s; add one or a default policy", e.Code))
			}
		}
	} else if len(d.Events) > 0 {
		fs = append(fs, warnf("datePolicies", "MISSING_DATE_POLICY", "no date policies yet: events cannot be booked into periods until one is configured"))
	}
	return fs
}

// EventDates are an event's dates as received.
type EventDates struct {
	TransactionDate string `json:"transactionDate"`
	EffectiveDate   string `json:"effectiveDate"`
	RecordedAt      string `json:"recordedAt,omitempty"` // RFC 3339 instant
}

// PeriodDecision is where and how an event is booked, with the reasoning.
type PeriodDecision struct {
	Policy           string   `json:"policy"`
	TransactionDate  string   `json:"transactionDate"`
	EffectiveDate    string   `json:"effectiveDate"`
	BookDate         string   `json:"bookDate"`
	AccountingPeriod string   `json:"accountingPeriod"` // YYYY-MM
	FiscalYear       int      `json:"fiscalYear"`
	FiscalPeriod     int      `json:"fiscalPeriod"`
	BookingClass     string   `json:"bookingClass,omitempty"` // A | C when the policy classifies advances
	LateFromPeriod   string   `json:"lateFromPeriod,omitempty"`
	Steps            []string `json:"steps"`
}

// PeriodError is a booking refusal (e.g. CLOSED_PERIOD under a REJECT late policy).
type PeriodError struct {
	Code    string
	Message string
}

func (e *PeriodError) Error() string { return e.Code + ": " + e.Message }

type calendar struct {
	loc      *time.Location
	weekend  map[time.Weekday]bool
	holidays map[string]bool
}

func loadCalendar(d *Document, code string) (calendar, error) {
	for _, c := range d.Calendars {
		if c.Code != code {
			continue
		}
		loc, err := time.LoadLocation(c.Timezone)
		if err != nil {
			return calendar{}, err
		}
		cal := calendar{loc: loc, weekend: map[time.Weekday]bool{}, holidays: map[string]bool{}}
		for _, w := range c.Weekend {
			cal.weekend[weekdays[w]] = true
		}
		for _, h := range c.Holidays {
			cal.holidays[h] = true
		}
		return cal, nil
	}
	return calendar{}, fmt.Errorf("calendar %s not configured", code)
}

func (c calendar) businessDay(t time.Time) bool {
	return !c.weekend[t.Weekday()] && !c.holidays[t.Format("2006-01-02")]
}

func (c calendar) nextBusinessDay(t time.Time) time.Time {
	for !c.businessDay(t) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

func monthEnd(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, time.UTC)
}

func monthStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
}

// closeDate is the last day bookings into the month of t are accepted: the Nth business day
// after month end (N = 0: month end itself).
func (c calendar) closeDate(t time.Time, n int) time.Time {
	d := monthEnd(t)
	for i := 0; i < n; {
		d = d.AddDate(0, 0, 1)
		if c.businessDay(d) {
			i++
		}
	}
	return d
}

func fiscal(t time.Time, startMonth int) (int, int) {
	if startMonth <= 1 {
		return t.Year(), int(t.Month())
	}
	p := (int(t.Month())-startMonth+12)%12 + 1
	fy := t.Year()
	if int(t.Month()) >= startMonth {
		fy++
	}
	return fy, p
}

// DecidePeriod books an event's dates under the policy that governs its event type, as of a
// processing date. It is deterministic: the same dates, policy and processing date always
// give the same decision, and Steps say why.
func DecidePeriod(d *Document, eventType string, dates EventDates, asOf time.Time) (PeriodDecision, error) {
	pol, ok := policyFor(d, eventType)
	if !ok {
		return PeriodDecision{}, &PeriodError{"MISSING_DATE_POLICY", "no date policy governs " + eventType}
	}
	cal, err := loadCalendar(d, pol.Calendar)
	if err != nil {
		return PeriodDecision{}, &PeriodError{"UNKNOWN_CALENDAR", err.Error()}
	}
	txn, ok1 := ParseDay(dates.TransactionDate)
	eff, ok2 := ParseDay(dates.EffectiveDate)
	if !ok1 || !ok2 {
		return PeriodDecision{}, &PeriodError{"INVALID_DATE", "transactionDate and effectiveDate must be real dates (YYYY-MM-DD)"}
	}
	dec := PeriodDecision{Policy: pol.Code, TransactionDate: dates.TransactionDate, EffectiveDate: dates.EffectiveDate}
	step := func(format string, a ...any) { dec.Steps = append(dec.Steps, fmt.Sprintf(format, a...)) }

	var book time.Time
	switch pol.BookDate {
	case "TRANSACTION_DATE":
		book = txn
		step("book date = transaction date %s (policy %s)", dates.TransactionDate, pol.Code)
	case "EFFECTIVE_DATE":
		book = eff
		step("book date = effective date %s (policy %s)", dates.EffectiveDate, pol.Code)
	case "RECORDED_DATE":
		at, err := time.Parse(time.RFC3339, dates.RecordedAt)
		if err != nil {
			return PeriodDecision{}, &PeriodError{"INVALID_DATE", "recordedAt must be an RFC 3339 instant when the policy books on the recorded date"}
		}
		local := at.In(cal.loc)
		book = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
		step("book date = recorded instant %s in %s = %s", dates.RecordedAt, cal.loc, book.Format("2006-01-02"))
	}
	if pol.BusinessDayRule == "NEXT_BUSINESS_DAY" && !cal.businessDay(book) {
		was := book
		book = cal.nextBusinessDay(book)
		step("%s is not a business day; rolled to %s", was.Format("2006-01-02"), book.Format("2006-01-02"))
	}

	natural := monthStart(book)
	close := cal.closeDate(natural, pol.CloseAfterBusinessDay)
	asOfDay := time.Date(asOf.Year(), asOf.Month(), asOf.Day(), 0, 0, 0, 0, time.UTC)
	if asOfDay.After(close) {
		step("period %s closed after %s (business day %d after month end); processing on %s", natural.Format("2006-01"), close.Format("2006-01-02"), pol.CloseAfterBusinessDay, asOfDay.Format("2006-01-02"))
		if pol.LatePolicy == "REJECT" {
			return PeriodDecision{}, &PeriodError{"CLOSED_PERIOD", fmt.Sprintf("period %s is closed and policy %s rejects late events", natural.Format("2006-01"), pol.Code)}
		}
		dec.LateFromPeriod = natural.Format("2006-01")
		p := natural
		for asOfDay.After(cal.closeDate(p, pol.CloseAfterBusinessDay)) {
			p = p.AddDate(0, 1, 0)
		}
		book = p
		if pol.BusinessDayRule == "NEXT_BUSINESS_DAY" {
			book = cal.nextBusinessDay(book)
		}
		step("late event: booked on %s in the first open period %s; the closed period is not changed", book.Format("2006-01-02"), p.Format("2006-01"))
	}

	dec.BookDate = book.Format("2006-01-02")
	dec.AccountingPeriod = book.Format("2006-01")
	dec.FiscalYear, dec.FiscalPeriod = fiscal(book, pol.FiscalYearStartMonth)
	if pol.Advance {
		if eff.After(monthEnd(book)) {
			dec.BookingClass = "A"
			step("effective %s is after the end of period %s: advance (A)", dates.EffectiveDate, dec.AccountingPeriod)
		} else {
			dec.BookingClass = "C"
			step("effective %s falls on or before the end of period %s: current (C)", dates.EffectiveDate, dec.AccountingPeriod)
		}
	}
	return dec, nil
}

// lookupDay is the date that selects effective-dated lookups and authority for an event.
func lookupDay(d *Document, eventType string, dates EventDates) (time.Time, bool) {
	which := "EFFECTIVE_DATE"
	if pol, ok := policyFor(d, eventType); ok && pol.LookupDate != "" {
		which = pol.LookupDate
	}
	s := dates.EffectiveDate
	if which == "TRANSACTION_DATE" {
		s = dates.TransactionDate
	}
	return ParseDay(s)
}
