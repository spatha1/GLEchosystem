package model

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// Change impact: what a new configuration version changes, and what that affects — the
// events whose configuration it touches, the fields they need (and so the source mappings),
// the scenarios' journals, and the downstream stages that will follow the new version.

// ItemChange is one configuration item added, removed or changed between two versions.
type ItemChange struct {
	Section string   `json:"section"`
	Key     string   `json:"key"`    // the item's code (sourceAuthority: event/measure/source/from)
	Change  string   `json:"change"` // ADDED | REMOVED | CHANGED
	Fields  []string `json:"fields"` // the item's fields that changed; lines/<code> for one line of a rule
	Path    string   `json:"path"`   // where the item is (in the new version; in the old one when removed)
}

// Ref is the item as section:key.
func (c ItemChange) Ref() string { return c.Section + ":" + c.Key }

func itemKey(section string, i int, item map[string]any) string {
	if section == "sourceAuthority" {
		s := func(k string) string { v, _ := item[k].(string); return v }
		return authorityKey(SourceAuthority{EventType: s("eventType"), Measure: s("measure"), SourceSystem: s("sourceSystem"), EffectiveFrom: s("effectiveFrom")})
	}
	if c, ok := item["code"].(string); ok && c != "" {
		return c
	}
	return fmt.Sprintf("#%d", i)
}

func sectionsOf(raw []byte) map[string][]map[string]any {
	var s map[string][]map[string]any
	_ = json.Unmarshal(raw, &s)
	if s == nil {
		s = map[string][]map[string]any{}
	}
	return s
}

// codedList reads a field as a list of objects with codes (a rule's lines), if it is one.
func codedList(v any) (map[string]any, []string, bool) {
	xs, ok := v.([]any)
	if !ok || len(xs) == 0 {
		return nil, nil, false
	}
	m := map[string]any{}
	var order []string
	for _, x := range xs {
		o, ok := x.(map[string]any)
		if !ok {
			return nil, nil, false
		}
		c, ok := o["code"].(string)
		if !ok || c == "" {
			return nil, nil, false
		}
		m[c] = o
		order = append(order, c)
	}
	return m, order, true
}

func changedFields(a, b map[string]any) []string {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	var names []string
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	out := []string{}
	for _, k := range names {
		if reflect.DeepEqual(a[k], b[k]) {
			continue
		}
		am, aOrder, aok := codedList(a[k])
		bm, bOrder, bok := codedList(b[k])
		if aok && bok {
			var sub []string
			for _, c := range bOrder {
				if old, ok := am[c]; !ok {
					sub = append(sub, k+"/"+c+" added")
				} else if !reflect.DeepEqual(old, bm[c]) {
					sub = append(sub, k+"/"+c)
				}
			}
			for _, c := range aOrder {
				if _, ok := bm[c]; !ok {
					sub = append(sub, k+"/"+c+" removed")
				}
			}
			if len(sub) == 0 {
				sub = append(sub, k+" reordered")
			}
			out = append(out, sub...)
			continue
		}
		out = append(out, k)
	}
	return out
}

// DiffContent lists the items that differ between two contents, section by section.
func DiffContent(from, to []byte) []ItemChange {
	a, b := sectionsOf(from), sectionsOf(to)
	sections := map[string]bool{}
	for k := range a {
		sections[k] = true
	}
	for k := range b {
		sections[k] = true
	}
	var names []string
	for k := range sections {
		names = append(names, k)
	}
	sort.Strings(names)
	out := []ItemChange{}
	for _, sec := range names {
		old := map[string]map[string]any{}
		oldAt := map[string]int{}
		for i, it := range a[sec] {
			k := itemKey(sec, i, it)
			old[k], oldAt[k] = it, i
		}
		seen := map[string]bool{}
		for i, it := range b[sec] {
			k := itemKey(sec, i, it)
			seen[k] = true
			path := fmt.Sprintf("%s/%d", sec, i)
			prev, existed := old[k]
			switch {
			case !existed:
				out = append(out, ItemChange{Section: sec, Key: k, Change: "ADDED", Fields: []string{}, Path: path})
			case !reflect.DeepEqual(prev, it):
				out = append(out, ItemChange{Section: sec, Key: k, Change: "CHANGED", Fields: changedFields(prev, it), Path: path})
			}
		}
		for i, it := range a[sec] {
			if k := itemKey(sec, i, it); !seen[k] {
				out = append(out, ItemChange{Section: sec, Key: k, Change: "REMOVED", Fields: []string{}, Path: fmt.Sprintf("%s/%d", sec, oldAt[k])})
			}
		}
	}
	return out
}

// DescribeChanges renders changes as short sentences for an audit trail.
func DescribeChanges(cs []ItemChange) []string {
	out := []string{}
	for _, c := range cs {
		s := fmt.Sprintf("%s %s %s", strings.ToLower(c.Change), c.Section, c.Key)
		if len(c.Fields) > 0 {
			s += " (" + strings.Join(c.Fields, ", ") + ")"
		}
		out = append(out, s)
	}
	return out
}

// Impact is what changing from one version to another affects.
type Impact struct {
	From       ConfigRef        `json:"from"`
	To         ConfigRef        `json:"to"`
	Changes    []ItemChange     `json:"changes"`
	Events     []EventImpact    `json:"events"`
	Manifest   ManifestDiff     `json:"manifest"`
	Scenarios  []ScenarioImpact `json:"scenarios"`
	Downstream []Stage          `json:"downstream"`
}

// EventImpact is one event type the change affects, and through which items.
type EventImpact struct {
	EventType     string   `json:"eventType"`
	Because       []string `json:"because"` // section:key of the changed items it depends on
	FieldsChanged int      `json:"fieldsChanged"`
	Remap         int      `json:"remap"` // fields whose source mapping must be added, reviewed or dropped
}

// ScenarioImpact compares one scenario's run under both versions.
type ScenarioImpact struct {
	Code        string          `json:"code"`
	EventType   string          `json:"eventType"`
	Before      *ScenarioResult `json:"before,omitempty"`
	After       *ScenarioResult `json:"after,omitempty"`
	BeforeLegs  []string        `json:"beforeLegs"`
	AfterLegs   []string        `json:"afterLegs"`
	Changed     bool            `json:"changed"`
	Differences []string        `json:"differences"`
}

// Stage is one stage of the GL flow with its state for a version.
type Stage struct {
	Stage  string `json:"stage"`
	Label  string `json:"label"`
	Status string `json:"status"` // DONE | PENDING | FAILED | NOT_IMPLEMENTED
	Detail string `json:"detail"`
	Story  string `json:"story,omitempty"`
}

// NotImplementedStages are the stages of the GL flow after simulation that do not exist yet.
func NotImplementedStages() []Stage {
	return []Stage{
		{Stage: "posted", Label: "Posted", Status: "NOT_IMPLEMENTED", Story: "US-019..US-021", Detail: "journal drafts and atomic posting are not built yet: nothing is posted"},
		{Stage: "subledger", Label: "Subledger", Status: "NOT_IMPLEMENTED", Story: "US-022..US-024", Detail: "receivable tracking and allocations are not built yet"},
		{Stage: "reconciled", Label: "Reconciled", Status: "NOT_IMPLEMENTED", Story: "US-028, US-038", Detail: "reconciliation equations and control balances are not built yet"},
		{Stage: "reported", Label: "Reports and files", Status: "NOT_IMPLEMENTED", Story: "US-039, US-040", Detail: "reports, extracts and TRF files are not built yet"},
	}
}

// ComputeImpact compares two versions of one configuration.
func ComputeImpact(fromDoc, toDoc *Document, fromRaw, toRaw []byte, tenant string, from, to ConfigRef) Impact {
	im := Impact{From: from, To: to, Changes: DiffContent(fromRaw, toRaw), Events: []EventImpact{}, Scenarios: []ScenarioImpact{}}
	fromM, _ := CompileManifest(fromDoc, tenant, from)
	toM, _ := CompileManifest(toDoc, tenant, to)
	im.Manifest = DiffManifests(fromM, toM)

	fieldChanges := map[string][2]int{}
	for _, e := range im.Manifest.Events {
		n, r := 0, 0
		for _, f := range e.Fields {
			n++
			if f.Remap {
				r++
			}
		}
		fieldChanges[e.EventType] = [2]int{n, r}
	}
	codes := []string{}
	refs := map[string]map[string]bool{}
	for _, d := range []*Document{fromDoc, toDoc} {
		for _, e := range d.Events {
			if _, ok := refs[e.Code]; !ok {
				refs[e.Code] = map[string]bool{}
				codes = append(codes, e.Code)
			}
			for k := range eventRefs(d, e) {
				refs[e.Code][k] = true
			}
		}
	}
	for _, code := range codes {
		var because []string
		for _, c := range im.Changes {
			if refs[code][c.Ref()] {
				because = append(because, c.Ref())
			}
		}
		fc := fieldChanges[code]
		if len(because) > 0 || fc[0] > 0 {
			im.Events = append(im.Events, EventImpact{EventType: code, Because: append([]string{}, because...), FieldsChanged: fc[0], Remap: fc[1]})
		}
	}

	var order []string
	defs := map[string]Scenario{}
	for _, d := range []*Document{toDoc, fromDoc} {
		for _, s := range d.Scenarios {
			if _, ok := defs[s.Code]; !ok {
				defs[s.Code] = s
				order = append(order, s.Code)
			}
		}
	}
	hasEvent := func(d *Document, code string) bool {
		for _, e := range d.Events {
			if e.Code == code {
				return true
			}
		}
		return false
	}
	for _, code := range order {
		s := defs[code]
		si := ScenarioImpact{Code: code, EventType: s.EventType, BeforeLegs: []string{}, AfterLegs: []string{}, Differences: []string{}}
		if hasEvent(fromDoc, s.EventType) {
			r := RunScenario(fromDoc, from, s)
			si.Before, si.BeforeLegs = &r, LegSummary(r.Plan)
			if r.Error != nil {
				si.BeforeLegs = []string{}
			}
		}
		if hasEvent(toDoc, s.EventType) {
			r := RunScenario(toDoc, to, s)
			si.After, si.AfterLegs = &r, LegSummary(r.Plan)
			if r.Error != nil {
				si.AfterLegs = []string{}
			}
		}
		si.Differences = legDifferences(si)
		si.Changed = len(si.Differences) > 0
		im.Scenarios = append(im.Scenarios, si)
	}

	remap := 0
	for _, e := range im.Events {
		remap += e.Remap
	}
	mapping := Stage{Stage: "mapped", Label: "Data Workbench mapping", Status: "NOT_IMPLEMENTED", Story: "US-015, US-016",
		Detail: "no source mappings exist yet, so none goes stale"}
	if remap > 0 {
		mapping.Detail = fmt.Sprintf("%d field(s) would need a source mapping added, reviewed or dropped once mappings exist", remap)
	}
	im.Downstream = append([]Stage{mapping}, NotImplementedStages()...)
	return im
}

func errorText(r *ScenarioResult) string {
	if r == nil || r.Error == nil {
		return ""
	}
	return r.Error.Code
}

func legDifferences(si ScenarioImpact) []string {
	out := []string{}
	switch {
	case si.Before == nil && si.After != nil:
		return []string{"new scenario"}
	case si.After == nil:
		return []string{"the event no longer exists"}
	}
	if a, b := errorText(si.Before), errorText(si.After); a != b {
		switch {
		case a == "":
			out = append(out, "now refused: "+b)
		case b == "":
			out = append(out, "no longer refused (was "+a+")")
		default:
			out = append(out, "refusal "+a+" → "+b)
		}
	}
	if si.Before.Plan != nil && si.After.Plan != nil && si.Before.Plan.Rule != si.After.Plan.Rule {
		out = append(out, fmt.Sprintf("rule %s → %s", si.Before.Plan.Rule, si.After.Plan.Rule))
	}
	before := map[string]int{}
	for _, l := range si.BeforeLegs {
		before[l]++
	}
	for _, l := range si.AfterLegs {
		if before[l] > 0 {
			before[l]--
			continue
		}
		out = append(out, "new leg "+l)
	}
	for _, l := range si.BeforeLegs {
		if before[l] > 0 {
			before[l]--
			out = append(out, "leg gone "+l)
		}
	}
	if si.Before.Status != si.After.Status {
		out = append(out, fmt.Sprintf("status %s → %s", si.Before.Status, si.After.Status))
	}
	return out
}
