package model

import (
	"fmt"
	"sort"
	"strings"
)

// The configuration studio's read model: one event type with everything configured for it
// and how it connects — its sources and authority, measures and formulas, dimensions and
// lookups, keys, date policy, posting rules with each line's class and candidate accounts,
// scenarios, the dependency graph, its manifest and the findings that concern it. Everything
// here is computed from the version's content; nothing is stored.

// EventSummary is one event type in a version's overview.
type EventSummary struct {
	Code           string   `json:"code"`
	Description    string   `json:"description,omitempty"`
	SourceSystems  []string `json:"sourceSystems"`
	Measures       []string `json:"measures"`
	Lists          []string `json:"lists"`
	Rules          int      `json:"rules"`
	ActiveRules    int      `json:"activeRules"`
	Lines          int      `json:"lines"`
	RequiredFields int      `json:"requiredFields"`
	DerivedValues  int      `json:"derivedValues"`
	Scenarios      int      `json:"scenarios"`
	Errors         int      `json:"errors"`
	Warnings       int      `json:"warnings"`
}

// EventView is one event type explained.
type EventView struct {
	Event         EventType         `json:"event"`
	Path          string            `json:"path"`
	SourceSystems []SourceSystem    `json:"sourceSystems"`
	Authority     []SourceAuthority `json:"authority"`
	Measures      []MeasureUse      `json:"measures"`
	Derived       []DerivedUse      `json:"derived"`
	Dimensions    []DimensionUse    `json:"dimensions"`
	Lookups       []Lookup          `json:"lookups"`
	Keys          []KeyUse          `json:"keys"`
	DatePolicy    *DatePolicy       `json:"datePolicy,omitempty"`
	Calendar      *Calendar         `json:"calendar,omitempty"`
	Rules         []RuleView        `json:"rules"`
	Scenarios     []Scenario        `json:"scenarios"`
	Graph         *EventGraph       `json:"graph"`
	Manifest      ManifestEvent     `json:"manifest"`
	Explanation   EventExplanation  `json:"explanation"`
	Findings      []Finding         `json:"findings"`
}

// MeasureUse is a base measure the event carries (at event level, or per item of a list).
type MeasureUse struct {
	Measure
	List     string `json:"list,omitempty"`
	Required bool   `json:"required"`
}

// DerivedUse is a derived measure that applies to the event.
type DerivedUse struct {
	DerivedMeasure
	Path     string   `json:"path"`
	Inputs   []string `json:"inputs"`
	Required bool     `json:"required"`
}

// DimensionUse is a dimension the event carries or derives.
type DimensionUse struct {
	Code           string          `json:"code"`
	Label          string          `json:"label"`
	Carried        bool            `json:"carried"` // false: derived by a lookup
	Values         []string        `json:"values,omitempty"`
	Normalizations []Normalization `json:"normalizations,omitempty"`
	DerivedBy      string          `json:"derivedBy,omitempty"`
	Required       bool            `json:"required"`
}

// KeyUse is a business key with the roles it plays for the event.
type KeyUse struct {
	KeyProfile
	Roles []string `json:"roles"` // ECONOMIC_KEY, EVENT_KEY, ITEM_KEY <list>, LINK_KEY <list>
}

// RuleView is a posting rule of the event with its lines explained.
type RuleView struct {
	Rule     PostingRule `json:"rule"`
	Path     string      `json:"path"`
	IsActive bool        `json:"isActive"`
	Lines    []LineView  `json:"lines"`
}

// LineView is one line template with its class and the accounts it can be assigned.
type LineView struct {
	Line       LineTemplate `json:"line"`
	Path       string       `json:"path"`
	Class      *ClassInfo   `json:"class,omitempty"`
	Candidates []Candidate  `json:"candidates"`
}

// ClassInfo is an accounting class with its derived normal side and statement.
type ClassInfo struct {
	AccountingClass
	NormalSide string `json:"normalSide"`
	Statement  string `json:"statement"`
}

// Candidate is an assignment rule that can choose an account for a line's class.
type Candidate struct {
	Rule    AssignmentRule `json:"rule"`
	Path    string         `json:"path"`
	Account *Account       `json:"account,omitempty"`
}

func findingsUnder(fs []Finding, paths []string) []Finding {
	out := []Finding{}
	for _, f := range fs {
		for _, p := range paths {
			if f.Path == p || strings.HasPrefix(f.Path, p+"/") {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// eventPaths lists the configuration paths that belong to an event: the event itself, its
// authority entries, rules, the derived measures that apply to it and its scenarios.
func eventPaths(d *Document, e EventType, g *EventGraph) []string {
	var paths []string
	for i, x := range d.Events {
		if x.Code == e.Code {
			paths = append(paths, fmt.Sprintf("events/%d", i))
		}
	}
	for i, a := range d.SourceAuthority {
		if a.EventType == e.Code {
			paths = append(paths, fmt.Sprintf("sourceAuthority/%d", i))
		}
	}
	for i, r := range d.PostingRules {
		if r.EventType == e.Code {
			paths = append(paths, fmt.Sprintf("postingRules/%d", i))
		}
	}
	for _, n := range g.Nodes {
		if n.Kind == NodeDerivedMeasure && n.Path != "" {
			paths = append(paths, n.Path)
		}
	}
	for i, s := range d.Scenarios {
		if s.EventType == e.Code {
			paths = append(paths, fmt.Sprintf("scenarios/%d", i))
		}
	}
	return paths
}

// SummarizeEvents gives the overview of every event type of a version.
func SummarizeEvents(d *Document, findings []Finding, tenant string, ref ConfigRef) []EventSummary {
	m, x := CompileManifest(d, tenant, ref)
	out := []EventSummary{}
	for i, e := range d.Events {
		g, _ := BuildEventGraph(d, e.Code)
		s := EventSummary{Code: e.Code, Description: e.Description, SourceSystems: x.Events[i].SourceSystems, Measures: append([]string{}, e.Measures...), Lists: []string{}}
		for _, l := range e.Lists {
			s.Lists = append(s.Lists, l.Code)
			s.Measures = append(s.Measures, l.Measures...)
		}
		for _, r := range d.PostingRules {
			if r.EventType != e.Code {
				continue
			}
			s.Rules++
			if r.IsActive() {
				s.ActiveRules++
				s.Lines += len(r.Lines)
			}
		}
		for _, f := range m.Events[i].Fields {
			if f.Kind == "SOURCE" {
				s.RequiredFields++
			} else {
				s.DerivedValues++
			}
		}
		for _, sc := range d.Scenarios {
			if sc.EventType == e.Code {
				s.Scenarios++
			}
		}
		for _, f := range findingsUnder(findings, eventPaths(d, e, g)) {
			if f.Severity == "error" {
				s.Errors++
			} else {
				s.Warnings++
			}
		}
		out = append(out, s)
	}
	return out
}

// ExplainEvent builds the studio view of one event type.
func ExplainEvent(d *Document, findings []Finding, tenant string, ref ConfigRef, eventType string) (*EventView, bool) {
	idx := -1
	for i, e := range d.Events {
		if e.Code == eventType {
			idx = i
		}
	}
	if idx < 0 {
		return nil, false
	}
	e := d.Events[idx]
	g, _ := BuildEventGraph(d, e.Code)
	m, x := CompileManifest(d, tenant, ref)
	v := &EventView{Event: e, Path: fmt.Sprintf("events/%d", idx), Graph: g, Manifest: m.Events[idx], Explanation: x.Events[idx],
		SourceSystems: []SourceSystem{}, Authority: []SourceAuthority{}, Measures: []MeasureUse{}, Derived: []DerivedUse{},
		Dimensions: []DimensionUse{}, Lookups: []Lookup{}, Keys: []KeyUse{}, Rules: []RuleView{}, Scenarios: []Scenario{}}

	required := map[string]bool{}
	for _, f := range v.Manifest.Fields {
		required[f.ListCode+"."+f.FieldCode] = true
	}
	for _, a := range d.SourceAuthority {
		if a.EventType == e.Code {
			v.Authority = append(v.Authority, a)
		}
	}
	for _, s := range d.SourceSystems {
		for _, a := range v.Authority {
			if a.SourceSystem == s.Code {
				v.SourceSystems = append(v.SourceSystems, s)
				break
			}
		}
	}
	measures := ResolveMeasures(d)
	measure := func(code string) Measure {
		m := measures[code]
		if m.Code == "" {
			m.Code = code
		}
		return m
	}
	for _, code := range e.Measures {
		v.Measures = append(v.Measures, MeasureUse{Measure: measure(code), Required: required["."+code]})
	}
	for _, l := range e.Lists {
		for _, code := range l.Measures {
			v.Measures = append(v.Measures, MeasureUse{Measure: measure(code), List: l.Code, Required: required[l.Code+"."+code]})
		}
	}
	for _, n := range g.Nodes {
		switch n.Kind {
		case NodeDerivedMeasure:
			if dm, ok := findDerived(d, n.Code); ok {
				v.Derived = append(v.Derived, DerivedUse{DerivedMeasure: dm, Path: n.Path, Inputs: n.formulaInputs, Required: required["."+n.Code]})
			}
		case NodeDerivedDimension:
			for _, lk := range d.Lookups {
				if lk.Code == n.reqRef {
					v.Lookups = append(v.Lookups, lk)
				}
			}
		}
	}
	for _, code := range eventDimensions(d, e) {
		label, _ := (&graphBuilder{d: d}).dimensionLabel(code)
		u := DimensionUse{Code: code, Label: label, Carried: oneOf(code, e.Dimensions), Required: required["."+code]}
		for _, x := range d.Dimensions {
			if x.Code == code {
				u.Values, u.Normalizations = x.Values, x.Normalizations
				if x.DerivedBy != nil {
					u.DerivedBy = x.DerivedBy.Lookup
				}
			}
		}
		v.Dimensions = append(v.Dimensions, u)
	}
	roles := map[string][]string{}
	var order []string
	role := func(k, r string) {
		if _, ok := roles[k]; !ok {
			order = append(order, k)
		}
		if !oneOf(r, roles[k]) {
			roles[k] = append(roles[k], r)
		}
	}
	for _, k := range e.EconomicKey {
		role(k, "ECONOMIC_KEY")
	}
	for _, k := range eventKeys(e) {
		role(k, "EVENT_KEY")
	}
	for _, l := range e.Lists {
		for _, k := range l.ItemKey {
			role(k, "ITEM_KEY "+l.Code)
		}
		for _, k := range l.Keys {
			if !oneOf(k, l.ItemKey) {
				role(k, "LINK_KEY "+l.Code)
			}
		}
	}
	for _, k := range order {
		p, ok := (&graphBuilder{d: d}).keyProfile(k)
		if !ok {
			p = KeyProfile{Code: k}
		}
		v.Keys = append(v.Keys, KeyUse{KeyProfile: p, Roles: roles[k]})
	}
	if p, ok := policyFor(d, e.Code); ok {
		v.DatePolicy = &p
		for _, c := range d.Calendars {
			if c.Code == p.Calendar {
				c := c
				v.Calendar = &c
			}
		}
	}
	for i, r := range d.PostingRules {
		if r.EventType != e.Code {
			continue
		}
		rv := RuleView{Rule: r, Path: fmt.Sprintf("postingRules/%d", i), IsActive: r.IsActive(), Lines: []LineView{}}
		for j, l := range r.Lines {
			lv := LineView{Line: l, Path: fmt.Sprintf("%s/lines/%d", rv.Path, j), Candidates: []Candidate{}}
			if c, ok := ClassOf(d, l.Class); ok {
				lv.Class = &ClassInfo{AccountingClass: c, NormalSide: c.Side(), Statement: c.Statement()}
			}
			for k, a := range d.AssignmentRules {
				if a.Class != l.Class {
					continue
				}
				cand := Candidate{Rule: a, Path: fmt.Sprintf("accountAssignmentRules/%d", k)}
				for _, acc := range d.Accounts {
					if acc.Code == a.Account {
						acc := acc
						cand.Account = &acc
					}
				}
				lv.Candidates = append(lv.Candidates, cand)
			}
			sort.SliceStable(lv.Candidates, func(a, b int) bool { return precedes(lv.Candidates[a].Rule, lv.Candidates[b].Rule) < 0 })
			rv.Lines = append(rv.Lines, lv)
		}
		v.Rules = append(v.Rules, rv)
	}
	for _, s := range d.Scenarios {
		if s.EventType == e.Code {
			v.Scenarios = append(v.Scenarios, s)
		}
	}
	v.Findings = findingsUnder(findings, eventPaths(d, e, g))
	return v, true
}

// eventRefs lists every configuration item (section:key) an event depends on, so a change
// to any of them can be traced to the events it affects.
func eventRefs(d *Document, e EventType) map[string]bool {
	refs := map[string]bool{"events:" + e.Code: true}
	g, _ := BuildEventGraph(d, e.Code)
	for _, n := range g.Nodes {
		switch n.Kind {
		case NodeField:
			switch n.Category {
			case "KEY":
				refs["keyProfiles:"+n.Code] = true
			case "DIMENSION":
				refs["dimensions:"+n.Code] = true
			case "MEASURE":
				refs["baseMeasures:"+n.Code] = true
			}
		case NodeDerivedMeasure:
			refs["derivedMeasures:"+n.Code] = true
		case NodeDerivedDimension:
			refs["dimensions:"+n.Code] = true
			refs["lookups:"+n.reqRef] = true
		case NodeClass:
			refs["accountingClasses:"+n.Code] = true
			for _, a := range d.AssignmentRules {
				if a.Class == n.Code {
					refs["accountAssignmentRules:"+a.Code] = true
				}
			}
		case NodeAccount:
			refs["accounts:"+n.Code] = true
		}
	}
	for _, r := range d.PostingRules {
		if r.EventType == e.Code {
			refs["postingRules:"+r.Code] = true // active or not: (de)activation affects the event
		}
	}
	for _, a := range d.SourceAuthority {
		if a.EventType == e.Code {
			refs["sourceAuthority:"+authorityKey(a)] = true
			refs["sourceSystems:"+a.SourceSystem] = true
		}
	}
	if p, ok := policyFor(d, e.Code); ok {
		refs["datePolicies:"+p.Code] = true
		refs["calendars:"+p.Calendar] = true
	}
	for _, s := range d.Scenarios {
		if s.EventType == e.Code {
			refs["scenarios:"+s.Code] = true
		}
	}
	return refs
}

func authorityKey(a SourceAuthority) string {
	return a.EventType + "/" + a.Measure + "/" + a.SourceSystem + "/" + a.EffectiveFrom
}
