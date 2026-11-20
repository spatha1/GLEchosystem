package model

import (
	"fmt"
	"time"
)

// US-005 — source systems, the financial event catalog and source authority.

// SourceSystem is a system GL can receive data from.
type SourceSystem struct {
	Code        string `json:"code"`
	Kind        string `json:"kind"`
	Description string `json:"description,omitempty"`
}

// SourceKinds are the kinds of source a configuration may declare.
var SourceKinds = []string{"POLICY", "BILLING", "CLAIMS", "EARNINGS", "REINSURANCE", "SOURCE_JOURNAL", "CUSTOM"}

// EventType is one configurable financial event (premium posted, invoice generated, payment
// received, …). Nothing about an event type is fixed in code: its code, its identity, the
// measures it carries and its 1:N lists all come from configuration.
type EventType struct {
	Code        string `json:"code"`
	Description string `json:"description,omitempty"`
	// EconomicKey lists the fields whose values identify the economic event independently of
	// the source and intake mode; economicEventKey = <code>:<value>:<value>…
	EconomicKey []string    `json:"economicKey"`
	Keys        []string    `json:"keys,omitempty"`       // business keys carried at event level (default: the economic key)
	Measures    []string    `json:"measures,omitempty"`   // measures carried once per event
	Dimensions  []string    `json:"dimensions,omitempty"` // dimensions carried by the event (US-008)
	Lists       []EventList `json:"lists,omitempty"`      // 1:N children, e.g. ALLOCATION_LIST
}

// EventList is a 1:N child collection of an event; its measures are carried once per item,
// never multiplied into the parent.
type EventList struct {
	Code     string   `json:"code"`
	ItemKey  []string `json:"itemKey"`            // fields identifying an item within the event
	Keys     []string `json:"keys,omitempty"`     // linkage keys per item (e.g. INVOICE_NO)
	Measures []string `json:"measures,omitempty"` // measures carried once per item
}

// SourceAuthority says which source system may POST a measure of an event type, in which
// intake mode, over an effective window [EffectiveFrom, EffectiveTo). Measure "*" covers
// every measure of the event; an entry for a specific measure takes precedence over "*".
type SourceAuthority struct {
	EventType     string `json:"eventType"`
	Measure       string `json:"measure"`
	SourceSystem  string `json:"sourceSystem"`
	IntakeMode    string `json:"intakeMode"`
	EffectiveFrom string `json:"effectiveFrom,omitempty"`
	EffectiveTo   string `json:"effectiveTo,omitempty"`
}

// IntakeModes: a raw financial event, or an already-balanced source journal.
var IntakeModes = []string{"RAW_EVENT", "SOURCE_JOURNAL"}

// AllMeasures is the source-authority wildcard.
const AllMeasures = "*"

func oneOf(s string, set []string) bool {
	for _, x := range set {
		if x == s {
			return true
		}
	}
	return false
}

func init() { validators = append(validators, validateSources) }

func validateSources(d *Document) []Finding {
	var fs []Finding
	systems := map[string]bool{}
	for i, s := range d.SourceSystems {
		p := fmt.Sprintf("sourceSystems/%d", i)
		if !isCode(s.Code) {
			fs = append(fs, errf(p, "INVALID_CODE", "source system code %q must be an upper-case code", s.Code))
		}
		if systems[s.Code] {
			fs = append(fs, errf(p, "DUPLICATE_CODE", "source system %s is declared twice", s.Code))
		}
		systems[s.Code] = true
		if !oneOf(s.Kind, SourceKinds) {
			fs = append(fs, errf(p, "INVALID_KIND", "source system kind %q must be one of %v", s.Kind, SourceKinds))
		}
	}

	events := map[string]EventType{}
	for i, e := range d.Events {
		p := fmt.Sprintf("events/%d", i)
		if !isCode(e.Code) {
			fs = append(fs, errf(p, "INVALID_CODE", "event code %q must be an upper-case code", e.Code))
		}
		if _, dup := events[e.Code]; dup {
			fs = append(fs, errf(p, "DUPLICATE_CODE", "event %s is declared twice", e.Code))
		}
		events[e.Code] = e
		if len(e.EconomicKey) == 0 {
			fs = append(fs, errf(p, "MISSING_ECONOMIC_KEY", "event %s needs an economicKey (the fields that identify the economic event)", e.Code))
		}
		fs = append(fs, codeList(p+"/economicKey", e.EconomicKey)...)
		seen := map[string]string{}
		for _, m := range e.Measures {
			if !isCode(m) {
				fs = append(fs, errf(p+"/measures", "INVALID_CODE", "measure %q must be an upper-case code", m))
			}
			if seen[m] != "" {
				fs = append(fs, errf(p+"/measures", "DUPLICATE_MEASURE", "event %s lists measure %s twice", e.Code, m))
			}
			seen[m] = "event"
		}
		lists := map[string]bool{}
		for j, l := range e.Lists {
			lp := fmt.Sprintf("%s/lists/%d", p, j)
			if !isCode(l.Code) {
				fs = append(fs, errf(lp, "INVALID_CODE", "list code %q must be an upper-case code", l.Code))
			}
			if lists[l.Code] {
				fs = append(fs, errf(lp, "DUPLICATE_CODE", "event %s declares list %s twice", e.Code, l.Code))
			}
			lists[l.Code] = true
			if len(l.ItemKey) == 0 {
				fs = append(fs, errf(lp, "MISSING_ITEM_KEY", "list %s needs an itemKey so each item keeps its identity", l.Code))
			}
			fs = append(fs, codeList(lp+"/itemKey", l.ItemKey)...)
			fs = append(fs, codeList(lp+"/keys", l.Keys)...)
			for _, m := range l.Measures {
				if !isCode(m) {
					fs = append(fs, errf(lp+"/measures", "INVALID_CODE", "measure %q must be an upper-case code", m))
				}
				if where := seen[m]; where != "" {
					fs = append(fs, errf(lp+"/measures", "AMBIGUOUS_GRAIN", "measure %s is carried both by %s and by list %s of event %s; a measure has one grain", m, where, l.Code, e.Code))
				}
				seen[m] = "list " + l.Code
			}
		}
	}

	// Source authority: known references, valid windows, no ambiguity, full coverage.
	type key struct{ event, measure string }
	byKey := map[key][]int{}
	for i, a := range d.SourceAuthority {
		p := fmt.Sprintf("sourceAuthority/%d", i)
		ev, known := events[a.EventType]
		if !known {
			fs = append(fs, errf(p, "UNKNOWN_EVENT", "source authority names event %q, which is not in the event catalog", a.EventType))
		}
		if !systems[a.SourceSystem] {
			fs = append(fs, errf(p, "UNKNOWN_SOURCE", "source authority names source system %q, which is not declared", a.SourceSystem))
		}
		if !oneOf(a.IntakeMode, IntakeModes) {
			fs = append(fs, errf(p, "INVALID_INTAKE_MODE", "intakeMode %q must be one of %v", a.IntakeMode, IntakeModes))
		}
		if a.Measure != AllMeasures && known && !carries(ev, a.Measure) {
			fs = append(fs, errf(p, "UNKNOWN_MEASURE", "event %s does not carry measure %q", a.EventType, a.Measure))
		}
		if _, ok := window(a); !ok {
			fs = append(fs, errf(p, "INVALID_WINDOW", "effectiveFrom/effectiveTo must be real dates with effectiveTo after effectiveFrom"))
		}
		byKey[key{a.EventType, a.Measure}] = append(byKey[key{a.EventType, a.Measure}], i)
	}
	for k, idx := range byKey {
		for x := 0; x < len(idx); x++ {
			for y := x + 1; y < len(idx); y++ {
				a, b := d.SourceAuthority[idx[x]], d.SourceAuthority[idx[y]]
				if overlaps(a, b) {
					fs = append(fs, errf(fmt.Sprintf("sourceAuthority/%d", idx[y]), "AMBIGUOUS_SOURCE_AUTHORITY",
						"event %s measure %s has two authorities in overlapping windows (%s and %s); give them non-overlapping effective windows",
						k.event, k.measure, describe(a), describe(b)))
				}
			}
		}
	}
	for i, e := range d.Events {
		for _, m := range carried(e) {
			if len(byKey[key{e.Code, m}]) == 0 && len(byKey[key{e.Code, AllMeasures}]) == 0 {
				fs = append(fs, errf(fmt.Sprintf("events/%d", i), "MISSING_SOURCE_AUTHORITY",
					"no source is authoritative for measure %s of event %s; add a sourceAuthority entry (or one for measure \"*\")", m, e.Code))
			}
		}
	}
	return fs
}

func codeList(path string, xs []string) []Finding {
	var fs []Finding
	for _, x := range xs {
		if !isCode(x) {
			fs = append(fs, errf(path, "INVALID_CODE", "%q must be an upper-case code", x))
		}
	}
	return fs
}

// carried lists every measure an event carries, at event and at list grain.
func carried(e EventType) []string {
	out := append([]string{}, e.Measures...)
	for _, l := range e.Lists {
		out = append(out, l.Measures...)
	}
	return out
}

func carries(e EventType, m string) bool { return oneOf(m, carried(e)) }

// window parses an entry's [from, to); a missing bound is open.
func window(a SourceAuthority) ([2]*time.Time, bool) {
	var w [2]*time.Time
	if a.EffectiveFrom != "" {
		t, ok := ParseDay(a.EffectiveFrom)
		if !ok {
			return w, false
		}
		w[0] = &t
	}
	if a.EffectiveTo != "" {
		t, ok := ParseDay(a.EffectiveTo)
		if !ok {
			return w, false
		}
		w[1] = &t
	}
	if w[0] != nil && w[1] != nil && !w[1].After(*w[0]) {
		return w, false
	}
	return w, true
}

func overlaps(a, b SourceAuthority) bool {
	wa, oka := window(a)
	wb, okb := window(b)
	if !oka || !okb {
		return false // reported as INVALID_WINDOW
	}
	// [a0, a1) and [b0, b1) overlap unless one ends at or before the other starts.
	if wa[1] != nil && wb[0] != nil && !wa[1].After(*wb[0]) {
		return false
	}
	if wb[1] != nil && wa[0] != nil && !wb[1].After(*wa[0]) {
		return false
	}
	return true
}

func covers(a SourceAuthority, day time.Time) bool {
	w, ok := window(a)
	if !ok {
		return false
	}
	return (w[0] == nil || !day.Before(*w[0])) && (w[1] == nil || day.Before(*w[1]))
}

func describe(a SourceAuthority) string {
	f, t := a.EffectiveFrom, a.EffectiveTo
	if f == "" {
		f = "…"
	}
	if t == "" {
		t = "…"
	}
	return fmt.Sprintf("%s/%s [%s, %s)", a.SourceSystem, a.IntakeMode, f, t)
}

// ErrNoAuthority and ErrAmbiguousAuthority are the runtime outcomes a validated
// configuration should never produce for a covered date; they become exception cases.
type AuthorityError struct {
	Code    string // MISSING_SOURCE_AUTHORITY | AMBIGUOUS_SOURCE_AUTHORITY
	Message string
}

func (e *AuthorityError) Error() string { return e.Code + ": " + e.Message }

// ResolveAuthority returns the authority that governs posting of measure m of an event type
// on a date. A specific entry for the measure wins over a "*" entry; anything else that
// is not exactly one entry is an error — never a guess.
func ResolveAuthority(d *Document, eventType, m string, day time.Time) (SourceAuthority, error) {
	pick := func(measure string) ([]SourceAuthority, bool) {
		var hit []SourceAuthority
		for _, a := range d.SourceAuthority {
			if a.EventType == eventType && a.Measure == measure && covers(a, day) {
				hit = append(hit, a)
			}
		}
		return hit, len(hit) > 0
	}
	for _, measure := range []string{m, AllMeasures} {
		if hit, ok := pick(measure); ok {
			if len(hit) > 1 {
				return SourceAuthority{}, &AuthorityError{"AMBIGUOUS_SOURCE_AUTHORITY", fmt.Sprintf("%d authorities for %s/%s on %s", len(hit), eventType, m, day.Format("2006-01-02"))}
			}
			return hit[0], nil
		}
	}
	return SourceAuthority{}, &AuthorityError{"MISSING_SOURCE_AUTHORITY", fmt.Sprintf("no authority for %s/%s on %s", eventType, m, day.Format("2006-01-02"))}
}

// MayPost reports whether a measure of an event received from sourceSystem in intakeMode
// may be posted on a date. A non-authoritative copy is kept for reconciliation only.
func MayPost(d *Document, eventType, m, sourceSystem, intakeMode string, day time.Time) (bool, error) {
	a, err := ResolveAuthority(d, eventType, m, day)
	if err != nil {
		return false, err
	}
	return a.SourceSystem == sourceSystem && a.IntakeMode == intakeMode, nil
}
