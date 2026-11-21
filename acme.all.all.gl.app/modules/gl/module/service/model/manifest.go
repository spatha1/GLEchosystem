package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// US-014 — the required-field manifest (contract C2).
//
// CompileManifest reads each event's configuration graph (lineage.go) and lists the fields
// the configuration needs, with why: a source field is required when some configured
// behaviour reads it — directly, or through a formula, a lookup, a list or the date policy.
// What GL derives itself (derived measures, lookup-derived dimensions such as BANKING_COMPANY,
// date-policy values such as BOOKING_CLASS) is listed as DERIVED with what it is derived
// from, and is never requested from a source: its inputs are. Fields an event declares but
// nothing reads are left out of the manifest and reported as unused in the explanation.
//
// The manifest never names physical tables or columns. Its digest covers only what it asks
// for, not the configuration version, so a configuration change that does not change the
// fields keeps the digest — and the mappings bound to it stay valid (C3).

// ManifestSchemaVersion is the C2 contract version.
const ManifestSchemaVersion = "gl.manifest/v1"

// Manifest is a required-field manifest (C2).
type Manifest struct {
	SchemaVersion  string          `json:"schemaVersion"`
	TenantID       string          `json:"tenantId"`
	ConfigID       string          `json:"configId"`
	ConfigVersion  int             `json:"configVersion"`
	ConfigDigest   string          `json:"configDigest"`
	ManifestDigest string          `json:"manifestDigest"`
	Events         []ManifestEvent `json:"events"`
}

// ManifestEvent is one event type's fields.
type ManifestEvent struct {
	EventType    string          `json:"eventType"`
	SourceSystem string          `json:"sourceSystem,omitempty"` // when exactly one source system is authoritative
	Fields       []ManifestField `json:"fields"`
}

// ManifestField is one field the configuration needs.
type ManifestField struct {
	FieldCode   string        `json:"fieldCode"`
	Definition  string        `json:"definition,omitempty"`
	Kind        string        `json:"kind"` // SOURCE | DERIVED | LEDGER_STATE
	DataType    string        `json:"dataType"`
	Cardinality string        `json:"cardinality"` // ONE | LIST
	ListCode    string        `json:"listCode,omitempty"`
	Mandatory   bool          `json:"mandatory"`
	KeyRole     string        `json:"keyRole,omitempty"`
	DerivedFrom []string      `json:"derivedFrom,omitempty"`
	RequiredBy  []Requirement `json:"requiredBy"`
}

// Requirement is one configuration element that needs a field.
type Requirement struct {
	Kind string `json:"kind"` // POSTING_RULE | FORMULA | FILTER | KEY_PROFILE | DATE_POLICY | ACCOUNT_ASSIGNMENT | LOOKUP | EVENT_RECOGNITION | RECONCILIATION
	Ref  string `json:"ref"`
}

// ManifestExplanation is the manifest's lineage in full, for people: every use of every
// field with the chain through formulas, lookups and lists, the fields declared but unused,
// and where the canonical event carries each field.
type ManifestExplanation struct {
	Events []EventExplanation `json:"events"`
}

// EventExplanation explains one event type's manifest.
type EventExplanation struct {
	EventType     string             `json:"eventType"`
	Description   string             `json:"description,omitempty"`
	SourceSystems []string           `json:"sourceSystems"`
	Fields        []FieldExplanation `json:"fields"` // the manifest's fields, in its order
	Lists         []FieldExplanation `json:"lists"`  // the event's lists and what needs them
	Unused        []FieldExplanation `json:"unused"` // declared by the event, read by nothing
}

// FieldExplanation is one field with every reason it is needed.
type FieldExplanation struct {
	FieldCode     string        `json:"fieldCode"`
	ListCode      string        `json:"listCode,omitempty"`
	Kind          string        `json:"kind"`     // SOURCE | DERIVED | LIST
	Category      string        `json:"category"` // IDENTITY | CURRENCY | DATE | KEY | DIMENSION | MEASURE | FORMULA | LOOKUP | DATE_POLICY | LIST
	Label         string        `json:"label"`
	Path          string        `json:"path,omitempty"`   // where the canonical event (C4) carries it
	Detail        string        `json:"detail,omitempty"` // the formula, lookup or policy computing a derived value
	Node          string        `json:"node"`             // the event graph node
	Mandatory     bool          `json:"mandatory"`
	SourceSystems []string      `json:"sourceSystems,omitempty"`
	Constraints   []string      `json:"constraints,omitempty"`
	DerivedFrom   []string      `json:"derivedFrom,omitempty"`
	Reasons       []FieldReason `json:"reasons"`
}

// FieldReason is one use of a field.
type FieldReason struct {
	Kind      string   `json:"kind"` // the C2 requirement kind of the use
	Ref       string   `json:"ref"`
	Role      string   `json:"role"`
	Detail    string   `json:"detail"`        // "amount of line AR0_DR of rule PR_PREM_ADDED (Debit AR0 = GROSS)"
	Via       []string `json:"via,omitempty"` // the calculations in between, e.g. "GROSS = PREMIUM + TAX"
	Mandatory bool     `json:"mandatory"`
	Consumer  string   `json:"consumer"` // the event graph node that uses it
}

var (
	intermediateKinds = map[string]bool{NodeList: true, NodeDerivedMeasure: true, NodeDerivedDimension: true, NodePolicyValue: true}
	consumerKinds     = map[string]bool{NodeControl: true, NodeRule: true, NodeLine: true, NodeAccount: true}
	calculationKinds  = map[string]string{NodeDerivedMeasure: "FORMULA", NodeDerivedDimension: "LOOKUP", NodePolicyValue: "DATE_POLICY"}
)

type reach struct {
	edge      GraphEdge
	via       []string // intermediate node ids, nearest first
	mandatory bool
}

// reaches lists every use of a node: each path through calculations and lists that ends at
// a consumer.
func (g *EventGraph) reaches(from string) []reach {
	out := map[string][]GraphEdge{}
	for _, e := range g.Edges {
		out[e.From] = append(out[e.From], e)
	}
	var res []reach
	onPath := map[string]bool{from: true}
	var walk func(id string, via []string, mandatory bool)
	walk = func(id string, via []string, mandatory bool) {
		for _, e := range out[id] {
			n, ok := g.Node(e.To)
			if !ok {
				continue
			}
			m := mandatory && e.Mandatory
			switch {
			case consumerKinds[n.Kind]:
				res = append(res, reach{edge: e, via: append([]string{}, via...), mandatory: m})
			case intermediateKinds[n.Kind] && !onPath[n.ID]:
				onPath[n.ID] = true
				walk(n.ID, append(via, n.ID), m)
				delete(onPath, n.ID)
			}
		}
	}
	walk(from, nil, true)
	return res
}

func addReq(rs []Requirement, kind, ref string) []Requirement {
	if kind == "" || ref == "" {
		return rs
	}
	for _, r := range rs {
		if r.Kind == kind && r.Ref == ref {
			return rs
		}
	}
	return append(rs, Requirement{Kind: kind, Ref: ref})
}

// explain turns a node's uses into C2 requirements and reasons.
func (g *EventGraph) explain(id string) (reqs []Requirement, reasons []FieldReason, mandatory bool) {
	reqs, reasons = []Requirement{}, []FieldReason{}
	seen := map[string]bool{}
	for _, r := range g.reaches(id) {
		var via []string
		for _, v := range r.via {
			n, _ := g.Node(v)
			reqs = addReq(reqs, n.reqKind, n.reqRef)
			if n.Kind == NodeList {
				via = append(via, "each "+n.Code+" item")
			} else {
				via = append(via, n.Detail)
			}
		}
		reqs = addReq(reqs, r.edge.reqKind, r.edge.reqRef)
		k := r.edge.To + "\x00" + r.edge.Role + "\x00" + strings.Join(r.via, ",")
		if seen[k] {
			continue
		}
		seen[k] = true
		reasons = append(reasons, FieldReason{Kind: r.edge.reqKind, Ref: r.edge.reqRef, Role: r.edge.Role, Detail: r.edge.Label, Via: via,
			Mandatory: r.mandatory, Consumer: r.edge.To})
		mandatory = mandatory || r.mandatory
	}
	return
}

func categoryOf(n GraphNode) string {
	if c, ok := calculationKinds[n.Kind]; ok {
		return c
	}
	if n.Kind == NodeList {
		return "LIST"
	}
	return n.Category
}

// CompileManifest compiles the C2 manifest of a configuration version, and its explanation.
func CompileManifest(d *Document, tenant string, ref ConfigRef) (Manifest, ManifestExplanation) {
	m := Manifest{SchemaVersion: ManifestSchemaVersion, TenantID: tenant, ConfigID: ref.ConfigID, ConfigVersion: ref.Version,
		ConfigDigest: ref.ContentDigest, Events: []ManifestEvent{}}
	x := ManifestExplanation{Events: []EventExplanation{}}
	for _, e := range d.Events {
		g, _ := BuildEventGraph(d, e.Code)
		me := ManifestEvent{EventType: e.Code, Fields: []ManifestField{}}
		ex := EventExplanation{EventType: e.Code, Description: e.Description, SourceSystems: []string{},
			Fields: []FieldExplanation{}, Lists: []FieldExplanation{}, Unused: []FieldExplanation{}}
		for _, a := range d.SourceAuthority {
			if a.EventType == e.Code && !oneOf(a.SourceSystem, ex.SourceSystems) {
				ex.SourceSystems = append(ex.SourceSystems, a.SourceSystem)
			}
		}
		if len(ex.SourceSystems) == 1 {
			me.SourceSystem = ex.SourceSystems[0]
		}
		var derived []ManifestField
		var derivedWhy []FieldExplanation
		for _, n := range g.Nodes {
			switch {
			case n.Kind == NodeField:
				reqs, reasons, mandatory := g.explain(n.ID)
				fx := FieldExplanation{FieldCode: n.Code, ListCode: n.List, Kind: "SOURCE", Category: n.Category, Label: n.Label, Path: n.Field, Node: n.ID,
					Mandatory: mandatory, SourceSystems: n.sourceSystems, Constraints: n.constraints, Reasons: reasons}
				if len(reasons) == 0 {
					ex.Unused = append(ex.Unused, fx)
					continue
				}
				f := ManifestField{FieldCode: n.Code, Definition: n.definition, Kind: "SOURCE", DataType: n.DataType, Cardinality: "ONE",
					Mandatory: mandatory, KeyRole: n.KeyRole, RequiredBy: reqs}
				if n.List != "" {
					f.Cardinality, f.ListCode = "LIST", n.List
				}
				me.Fields = append(me.Fields, f)
				ex.Fields = append(ex.Fields, fx)
			case n.Kind == NodeList:
				_, reasons, mandatory := g.explain(n.ID)
				ex.Lists = append(ex.Lists, FieldExplanation{FieldCode: n.Code, Kind: "LIST", Category: "LIST", Label: n.Label, Path: n.Field, Detail: n.Detail,
					Node: n.ID, Mandatory: mandatory, Reasons: reasons})
			case calculationKinds[n.Kind] != "":
				reqs, reasons, mandatory := g.explain(n.ID)
				fx := FieldExplanation{FieldCode: n.Code, Kind: "DERIVED", Category: categoryOf(n), Label: n.Label, Detail: n.Detail, Node: n.ID,
					Mandatory: mandatory, DerivedFrom: n.formulaInputs, Reasons: reasons}
				if len(reasons) == 0 {
					ex.Unused = append(ex.Unused, fx)
					continue
				}
				derived = append(derived, ManifestField{FieldCode: n.Code, Definition: n.definition, Kind: "DERIVED", DataType: n.DataType, Cardinality: "ONE",
					Mandatory: mandatory, DerivedFrom: n.formulaInputs, RequiredBy: reqs})
				derivedWhy = append(derivedWhy, fx)
			case n.Kind == NodeLedgerState:
				// Read from posted lines: listed so the dependency is visible, never requested.
				reqs, reasons, mandatory := g.explain(n.ID)
				derived = append(derived, ManifestField{FieldCode: n.Code, Definition: n.definition, Kind: "LEDGER_STATE", DataType: n.DataType, Cardinality: "ONE",
					Mandatory: mandatory, DerivedFrom: n.formulaInputs, RequiredBy: reqs})
				derivedWhy = append(derivedWhy, FieldExplanation{FieldCode: n.Code, Kind: "LEDGER_STATE", Category: "LEDGER_STATE", Label: n.Label, Detail: n.Detail,
					Node: n.ID, Mandatory: mandatory, DerivedFrom: n.formulaInputs, Reasons: reasons})
			}
		}
		me.Fields = append(me.Fields, derived...)
		ex.Fields = append(ex.Fields, derivedWhy...)
		m.Events = append(m.Events, me)
		x.Events = append(x.Events, ex)
	}
	m.ManifestDigest = manifestDigest(m.Events)
	return m, x
}

func manifestDigest(events []ManifestEvent) string {
	b, _ := json.Marshal(events)
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ── change detection ─────────────────────────────────────────────────────────

// ManifestDiff is what changed between two manifests, field by field.
type ManifestDiff struct {
	FromVersion int           `json:"fromVersion"`
	ToVersion   int           `json:"toVersion"`
	FromDigest  string        `json:"fromDigest"`
	ToDigest    string        `json:"toDigest"`
	Changed     bool          `json:"changed"`
	Events      []EventChange `json:"events"` // only events whose fields changed
}

// EventChange is one event type's field changes.
type EventChange struct {
	EventType string        `json:"eventType"`
	Change    string        `json:"change"` // ADDED | REMOVED | CHANGED
	Fields    []FieldChange `json:"fields"`
}

// FieldChange is one field's change. Remap says a source mapping must be added, reviewed or
// dropped because of it (C3).
type FieldChange struct {
	FieldCode string   `json:"fieldCode"`
	ListCode  string   `json:"listCode,omitempty"`
	Kind      string   `json:"kind"`   // SOURCE | DERIVED, after the change (before it, when removed)
	Change    string   `json:"change"` // ADDED | REMOVED | CHANGED
	Details   []string `json:"details"`
	Remap     bool     `json:"remap"`
}

func fieldKey(f ManifestField) string { return f.ListCode + "." + f.FieldCode }

func reqText(r Requirement) string { return r.Kind + " " + r.Ref }

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// DiffManifests compares two manifests of the same configuration.
func DiffManifests(from, to Manifest) ManifestDiff {
	d := ManifestDiff{FromVersion: from.ConfigVersion, ToVersion: to.ConfigVersion, FromDigest: from.ManifestDigest, ToDigest: to.ManifestDigest,
		Changed: from.ManifestDigest != to.ManifestDigest, Events: []EventChange{}}
	before := map[string]ManifestEvent{}
	for _, e := range from.Events {
		before[e.EventType] = e
	}
	after := map[string]bool{}
	for _, e := range to.Events {
		after[e.EventType] = true
		old, existed := before[e.EventType]
		ec := EventChange{EventType: e.EventType, Change: "CHANGED", Fields: diffFields(old.Fields, e.Fields)}
		if !existed {
			ec.Change = "ADDED"
		}
		if len(ec.Fields) > 0 || !existed {
			d.Events = append(d.Events, ec)
		}
	}
	for _, e := range from.Events {
		if !after[e.EventType] {
			d.Events = append(d.Events, EventChange{EventType: e.EventType, Change: "REMOVED", Fields: diffFields(e.Fields, nil)})
		}
	}
	return d
}

func diffFields(from, to []ManifestField) []FieldChange {
	out := []FieldChange{}
	old := map[string]ManifestField{}
	for _, f := range from {
		old[fieldKey(f)] = f
	}
	now := map[string]bool{}
	for _, f := range to {
		now[fieldKey(f)] = true
		o, existed := old[fieldKey(f)]
		if !existed {
			det := []string{}
			for _, r := range f.RequiredBy {
				det = append(det, "required by "+reqText(r))
			}
			out = append(out, FieldChange{FieldCode: f.FieldCode, ListCode: f.ListCode, Kind: f.Kind, Change: "ADDED", Details: det, Remap: f.Kind == "SOURCE"})
			continue
		}
		det := []string{}
		remap := false
		if o.Kind != f.Kind {
			det = append(det, fmt.Sprintf("kind %s → %s", o.Kind, f.Kind))
			remap = true
		}
		if o.DataType != f.DataType {
			det = append(det, fmt.Sprintf("data type %s → %s", o.DataType, f.DataType))
			remap = true
		}
		if o.Cardinality != f.Cardinality {
			det = append(det, fmt.Sprintf("cardinality %s → %s", o.Cardinality, f.Cardinality))
			remap = true
		}
		if o.Mandatory != f.Mandatory {
			det = append(det, fmt.Sprintf("mandatory %s → %s", yesNo(o.Mandatory), yesNo(f.Mandatory)))
		}
		if o.KeyRole != f.KeyRole {
			det = append(det, fmt.Sprintf("key role %q → %q", o.KeyRole, f.KeyRole))
		}
		if strings.Join(o.DerivedFrom, ",") != strings.Join(f.DerivedFrom, ",") {
			det = append(det, fmt.Sprintf("derived from %s → %s", strings.Join(o.DerivedFrom, ", "), strings.Join(f.DerivedFrom, ", ")))
		}
		was := map[string]bool{}
		for _, r := range o.RequiredBy {
			was[reqText(r)] = true
		}
		is := map[string]bool{}
		for _, r := range f.RequiredBy {
			is[reqText(r)] = true
			if !was[reqText(r)] {
				det = append(det, "now also required by "+reqText(r))
			}
		}
		for _, r := range o.RequiredBy {
			if !is[reqText(r)] {
				det = append(det, "no longer required by "+reqText(r))
			}
		}
		if o.Definition != f.Definition {
			det = append(det, "definition changed")
		}
		if len(det) > 0 {
			kind := f.Kind
			out = append(out, FieldChange{FieldCode: f.FieldCode, ListCode: f.ListCode, Kind: kind, Change: "CHANGED", Details: det,
				Remap: remap && (o.Kind == "SOURCE" || f.Kind == "SOURCE")})
		}
	}
	for _, f := range from {
		if !now[fieldKey(f)] {
			out = append(out, FieldChange{FieldCode: f.FieldCode, ListCode: f.ListCode, Kind: f.Kind, Change: "REMOVED",
				Details: []string{"no configured behaviour reads it any more"}, Remap: f.Kind == "SOURCE"})
		}
	}
	return out
}
