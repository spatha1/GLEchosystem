// Package model is the typed GL configuration (spec contract C1): what a configuration
// version's content means, and the cross-section validation that decides whether it can be
// approved. It is pure — standard library only, no I/O — so the store (package logic), the
// manifest compiler and the posting engine all read the same model.
//
// Each section is introduced by its story; a section's types, validation and runtime
// helpers live in their own file (sources.go for US-005, …).
package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"time"
)

// Finding is one validation result. Severity "error" blocks approval; "warning" does not.
type Finding struct {
	Path     string `json:"path"`
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

func errf(path, code, format string, a ...any) Finding {
	return Finding{Path: path, Code: code, Severity: "error", Message: fmt.Sprintf(format, a...)}
}

func warnf(path, code, format string, a ...any) Finding {
	return Finding{Path: path, Code: code, Severity: "warning", Message: fmt.Sprintf(format, a...)}
}

// Document is a configuration's content, typed. Sections no story has given meaning to yet
// are not decoded here; the store keeps them as they are.
type Document struct {
	SourceSystems     []SourceSystem
	Events            []EventType
	SourceAuthority   []SourceAuthority
	BaseMeasures      []BaseMeasure
	DerivedMeasures   []DerivedMeasure
	Dimensions        []Dimension
	Lookups           []Lookup
	Calendars         []Calendar
	DatePolicies      []DatePolicy
	KeyProfiles       []KeyProfile
	AccountingClasses []AccountingClass
	Accounts          []Account
	PostingRules      []PostingRule
	AssignmentRules   []AssignmentRule
	Scenarios         []Scenario
	LifecycleProfiles []LifecycleProfile // ALTB-07
}

var codeRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)

func isCode(s string) bool { return codeRe.MatchString(s) }

// decodeSection decodes one section strictly, item by item: an unknown field, a wrong type
// or a non-object is a finding at that item's path, never silently dropped. Items that
// decode are returned, so later checks still run on the rest.
func decodeSection[T any](name string, raw json.RawMessage) ([]T, []Finding) {
	if len(raw) == 0 {
		return nil, nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, []Finding{errf(name, "INVALID_SECTION", "section must be an array of objects")}
	}
	var out []T
	var fs []Finding
	for i, it := range items {
		dec := json.NewDecoder(bytes.NewReader(it))
		dec.DisallowUnknownFields()
		var v T
		if err := dec.Decode(&v); err != nil {
			fs = append(fs, errf(fmt.Sprintf("%s/%d", name, i), "INVALID_ITEM", "%v", err))
			continue
		}
		out = append(out, v)
	}
	return out, fs
}

// Parse decodes canonical content (as stored by the configuration store) into a Document.
func Parse(canonical []byte) (*Document, []Finding) {
	doc := &Document{}
	var s map[string]json.RawMessage
	if err := json.Unmarshal(canonical, &s); err != nil {
		return doc, []Finding{errf("", "INVALID_DOCUMENT", "content is not a JSON object: %v", err)}
	}
	var fs, f []Finding
	doc.SourceSystems, f = decodeSection[SourceSystem]("sourceSystems", s["sourceSystems"])
	fs = append(fs, f...)
	doc.Events, f = decodeSection[EventType]("events", s["events"])
	fs = append(fs, f...)
	doc.SourceAuthority, f = decodeSection[SourceAuthority]("sourceAuthority", s["sourceAuthority"])
	fs = append(fs, f...)
	doc.BaseMeasures, f = decodeSection[BaseMeasure]("baseMeasures", s["baseMeasures"])
	fs = append(fs, f...)
	doc.DerivedMeasures, f = decodeSection[DerivedMeasure]("derivedMeasures", s["derivedMeasures"])
	fs = append(fs, f...)
	doc.Dimensions, f = decodeSection[Dimension]("dimensions", s["dimensions"])
	fs = append(fs, f...)
	doc.Lookups, f = decodeSection[Lookup]("lookups", s["lookups"])
	fs = append(fs, f...)
	doc.Calendars, f = decodeSection[Calendar]("calendars", s["calendars"])
	fs = append(fs, f...)
	doc.DatePolicies, f = decodeSection[DatePolicy]("datePolicies", s["datePolicies"])
	fs = append(fs, f...)
	doc.KeyProfiles, f = decodeSection[KeyProfile]("keyProfiles", s["keyProfiles"])
	fs = append(fs, f...)
	doc.AccountingClasses, f = decodeSection[AccountingClass]("accountingClasses", s["accountingClasses"])
	fs = append(fs, f...)
	doc.Accounts, f = decodeSection[Account]("accounts", s["accounts"])
	fs = append(fs, f...)
	doc.PostingRules, f = decodeSection[PostingRule]("postingRules", s["postingRules"])
	fs = append(fs, f...)
	doc.AssignmentRules, f = decodeSection[AssignmentRule]("accountAssignmentRules", s["accountAssignmentRules"])
	fs = append(fs, f...)
	doc.Scenarios, f = decodeSection[Scenario]("scenarios", s["scenarios"])
	fs = append(fs, f...)
	doc.LifecycleProfiles, f = decodeSection[LifecycleProfile]("lifecycleProfiles", s["lifecycleProfiles"])
	fs = append(fs, f...)
	return doc, fs
}

// ParseDay accepts only a real calendar date in YYYY-MM-DD form.
func ParseDay(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil && t.Format("2006-01-02") == s
}

// validators run in order; each story appends its own.
var validators []func(*Document) []Finding

// Validate runs every section's validation over a parsed document and returns the findings
// sorted by path then code, so a report is reproducible byte for byte.
func Validate(doc *Document) []Finding {
	var fs []Finding
	for _, v := range validators {
		fs = append(fs, v(doc)...)
	}
	sortFindings(fs)
	return fs
}

// ValidateCanonical parses and validates canonical content in one step.
func ValidateCanonical(canonical []byte) []Finding {
	doc, fs := Parse(canonical)
	fs = append(fs, Validate(doc)...)
	sortFindings(fs)
	return fs
}

func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Path != fs[j].Path {
			return fs[i].Path < fs[j].Path
		}
		return fs[i].Code < fs[j].Code
	})
}
