package model

import (
	"fmt"
	"strings"

	"gl/formula"
)

// US-014 — what one event's configuration depends on.
//
// BuildEventGraph traces every configured behaviour of one event type back to the raw values
// it reads: posting-rule filters, line amounts, conditions and legal entities, derived-measure
// formulas, lookup-derived dimensions, the date policy, account-assignment matches, the
// subledger keys and dimensions accounts require, and the event's identity and business keys.
//
// Nodes are source fields (what a source must supply), calculations GL derives itself (never
// collected: derived measures, lookup-derived dimensions, date-policy values), lists, and the
// consumers that need values (rules, lines, controls, accounts), plus the accounting classes
// lines post to. The required-field manifest (manifest.go) and the configuration studio's
// lineage views both read this one graph, so "why is this field required?" has one answer.

// Node kinds.
const (
	NodeField            = "FIELD"             // a raw value a source must supply
	NodeList             = "LIST"              // a 1:N collection of the event
	NodeDerivedMeasure   = "DERIVED_MEASURE"   // computed by a formula
	NodeDerivedDimension = "DERIVED_DIMENSION" // computed by a lookup
	NodePolicyValue      = "POLICY_VALUE"      // computed by the date policy
	NodeControl          = "CONTROL"           // identity, currency, keys, period
	NodeRule             = "RULE"
	NodeLine             = "LINE"
	NodeClass            = "CLASS"
	NodeAccount          = "ACCOUNT"
)

// Edge roles: how the target uses the source.
const (
	RoleIdentity         = "IDENTITY"
	RoleCurrency         = "CURRENCY"
	RoleEconomicKey      = "ECONOMIC_KEY"
	RoleKeyGraph         = "KEY_GRAPH"
	RolePeriod           = "PERIOD"
	RoleInput            = "INPUT"
	RoleLookupInput      = "LOOKUP_INPUT"
	RoleLookupDate       = "LOOKUP_DATE"
	RoleItemIdentity     = "ITEM_IDENTITY"
	RoleFilter           = "FILTER"
	RoleAmount           = "AMOUNT"
	RoleCondition        = "CONDITION"
	RoleEntity           = "ENTITY"
	RoleDefaultEntity    = "DEFAULT_ENTITY"
	RoleExpands          = "EXPANDS"
	RoleClass            = "CLASS"
	RoleAssigns          = "ASSIGNS"
	RoleInClass          = "IN_CLASS"
	RoleMatch            = "MATCH"
	RoleSubledgerKey     = "SUBLEDGER_KEY"
	RoleAccountDimension = "ACCOUNT_DIMENSION"
)

// GraphNode is one element of an event's configuration graph.
type GraphNode struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Code     string `json:"code"`
	Label    string `json:"label"`              // human name
	Detail   string `json:"detail,omitempty"`   // the formula, filter, lookup or rule text
	Path     string `json:"path,omitempty"`     // where the configuration defines it, e.g. postingRules/0/lines/1
	Field    string `json:"field,omitempty"`    // for a source field: where the canonical event (C4) carries it
	Category string `json:"category,omitempty"` // for a source field: IDENTITY | CURRENCY | DATE | KEY | DIMENSION | MEASURE
	DataType string `json:"dataType,omitempty"` // C2 data type of a field or calculation
	List     string `json:"list,omitempty"`     // the list an item field belongs to
	Rule     string `json:"rule,omitempty"`     // the rule a line belongs to
	KeyRole  string `json:"keyRole,omitempty"`  // BUSINESS_KEY | LINK_KEY | DEDUP_KEY

	definition    string // C2 definition of a field or calculation
	reqKind       string // for a calculation: the C2 requirement it stands for (FORMULA, LOOKUP, DATE_POLICY)
	reqRef        string
	constraints   []string
	sourceSystems []string
	formulaInputs []string // for a calculation: the codes it reads
}

// GraphEdge says that To uses the value of From, in Role.
type GraphEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Role      string `json:"role"`
	Label     string `json:"label"`     // the sentence a BA reads: "amount of line AR0_DR of rule PR_PREM_ADDED"
	Mandatory bool   `json:"mandatory"` // without the value the event cannot be processed this way

	reqKind, reqRef string // the C2 requirement this use is (POSTING_RULE, FILTER, …)
}

// EventGraph is one event type's configuration graph.
type EventGraph struct {
	EventType string      `json:"eventType"`
	Nodes     []GraphNode `json:"nodes"`
	Edges     []GraphEdge `json:"edges"`

	at map[string]int
}

// Node returns a node by id.
func (g *EventGraph) Node(id string) (GraphNode, bool) {
	i, ok := g.at[id]
	if !ok {
		return GraphNode{}, false
	}
	return g.Nodes[i], true
}

type graphBuilder struct {
	d        *Document
	ev       EventType
	measures map[string]Measure
	syms     formula.Symbols
	dims     []string // the event's dimensions, carried and derivable
	policy   *DatePolicy
	g        *EventGraph
	seen     map[string]bool
}

// BuildEventGraph builds the configuration graph of one event type. It never fails on an
// invalid configuration: what cannot be resolved is left out (validation reports it).
func BuildEventGraph(d *Document, eventType string) (*EventGraph, bool) {
	for _, e := range d.Events {
		if e.Code != eventType {
			continue
		}
		m := ResolveMeasures(d)
		b := &graphBuilder{d: d, ev: e, measures: m, syms: eventSymbols(d, e, m), dims: eventDimensions(d, e),
			g: &EventGraph{EventType: e.Code, Nodes: []GraphNode{}, Edges: []GraphEdge{}, at: map[string]int{}}, seen: map[string]bool{}}
		if p, ok := policyFor(d, e.Code); ok {
			b.policy = &p
		}
		b.declare()
		b.controls()
		b.calculations()
		b.rules()
		return b.g, true
	}
	return nil, false
}

func (b *graphBuilder) add(n GraphNode) string {
	if _, ok := b.g.at[n.ID]; !ok {
		b.g.at[n.ID] = len(b.g.Nodes)
		b.g.Nodes = append(b.g.Nodes, n)
	}
	return n.ID
}

func (b *graphBuilder) has(id string) bool { _, ok := b.g.at[id]; return ok }

func (b *graphBuilder) ifHas(id string) string {
	if b.has(id) {
		return id
	}
	return ""
}

func (b *graphBuilder) link(from, to, role, label string, mandatory bool, reqKind, reqRef string) {
	if from == "" || to == "" {
		return
	}
	k := from + "\x00" + to + "\x00" + role
	if b.seen[k] {
		return
	}
	b.seen[k] = true
	b.g.Edges = append(b.g.Edges, GraphEdge{From: from, To: to, Role: role, Label: label, Mandatory: mandatory, reqKind: reqKind, reqRef: reqRef})
}

func fieldID(list, code string) string {
	if list != "" {
		return "field:" + list + "." + code
	}
	return "field:" + code
}

func (b *graphBuilder) field(n GraphNode) string {
	n.ID, n.Kind = fieldID(n.List, n.Code), NodeField
	if n.Label == "" {
		n.Label = n.definition
	}
	return b.add(n)
}

// ── declarations: every value the event can carry ───────────────────────────

func (b *graphBuilder) keyProfile(code string) (KeyProfile, bool) {
	for _, k := range b.d.KeyProfiles {
		if k.Code == code {
			return k, true
		}
	}
	return KeyProfile{}, false
}

func (b *graphBuilder) keyField(list, code, path, role string) string {
	n := GraphNode{Code: code, List: list, Category: "KEY", DataType: "STRING", Field: path, KeyRole: role, definition: "business key " + code}
	if k, ok := b.keyProfile(code); ok {
		if k.Description != "" {
			n.definition = k.Description
		}
		if k.Pattern != "" {
			n.constraints = append(n.constraints, "pattern "+k.Pattern)
		}
		if k.CaseInsensitive {
			n.constraints = append(n.constraints, "case-insensitive")
		}
		if len(k.Scope) > 0 {
			n.constraints = append(n.constraints, "unique within "+strings.Join(k.Scope, " + "))
		}
		for _, l := range k.Links {
			n.constraints = append(n.constraints, fmt.Sprintf("links to %s (%s)", l.To, l.Cardinality))
		}
	}
	n.Label = n.definition
	return b.field(n)
}

func (b *graphBuilder) dimensionLabel(code string) (label string, constraints []string) {
	label = code
	for _, x := range OOTBDimensions() {
		if x.Code == code {
			label = x.Name
		}
	}
	for _, x := range b.d.Dimensions {
		if x.Code != code {
			continue
		}
		if x.Description != "" {
			label = x.Description
		}
		if len(x.Values) > 0 {
			constraints = append(constraints, "one of "+strings.Join(x.Values, ", "))
		}
		for _, n := range x.Normalizations {
			var pairs []string
			for _, k := range sortedKeys(n.Map) {
				pairs = append(pairs, k+" → "+n.Map[k])
			}
			s := fmt.Sprintf("%s sends %s", n.SourceSystem, strings.Join(pairs, ", "))
			if n.CaseInsensitive {
				s += " (case-insensitive)"
			}
			constraints = append(constraints, s)
		}
	}
	return
}

func (b *graphBuilder) measureField(list, code, path string) string {
	m := b.measures[code]
	n := GraphNode{Code: code, List: list, Category: "MEASURE", DataType: c2Type(m.DataType), Field: path, Label: m.Name, definition: m.Definition}
	if n.definition == "" {
		n.definition = code
	}
	if n.Label == "" {
		n.Label = code
	}
	if m.Sign != "" {
		n.constraints = append(n.constraints, "sign "+strings.ToLower(m.Sign))
	}
	if m.Currency == "EVENT" {
		n.constraints = append(n.constraints, "in the event currency")
	}
	return b.field(n)
}

// c2Type maps a measure or formula type to a C2 data type.
func c2Type(t string) string {
	switch t {
	case "AMOUNT", "QUANTITY", "RATE", "NUMBER":
		return "DECIMAL"
	case "COUNT":
		return "INTEGER"
	case "BOOL":
		return "BOOLEAN"
	case "DATE":
		return "DATE"
	case "STRING":
		return "STRING"
	}
	return "DECIMAL"
}

func (b *graphBuilder) declare() {
	e := b.ev
	b.field(GraphNode{Code: "SOURCE_EVENT_ID", Category: "IDENTITY", DataType: "STRING", Field: "sourceEventId", KeyRole: "DEDUP_KEY",
		Label: "Source record id", definition: "the source system's identifier of the record"})
	b.field(GraphNode{Code: "SOURCE_EVENT_VERSION", Category: "IDENTITY", DataType: "INTEGER", Field: "sourceEventVersion", KeyRole: "DEDUP_KEY",
		Label: "Source record version", definition: "the source's version of the record; a correction has a higher version"})
	b.field(GraphNode{Code: "CURRENCY", Category: "CURRENCY", DataType: "CODE", Field: "currency", Label: "Currency",
		definition: "ISO 4217 currency of the event's amounts", constraints: []string{"ISO 4217 code"}})
	b.field(GraphNode{Code: "TRANSACTION_DATE", Category: "DATE", DataType: "DATE", Field: "dates.transactionDate", Label: "Transaction date",
		definition: "when the transaction happened at source", constraints: []string{"YYYY-MM-DD"}})
	b.field(GraphNode{Code: "EFFECTIVE_DATE", Category: "DATE", DataType: "DATE", Field: "dates.effectiveDate", Label: "Effective date",
		definition: "when the cover or change takes effect", constraints: []string{"YYYY-MM-DD"}})
	if b.policy != nil && b.policy.BookDate == "RECORDED_DATE" {
		b.field(GraphNode{Code: "RECORDED_AT", Category: "DATE", DataType: "TIMESTAMP", Field: "dates.recordedAt", Label: "Recorded at",
			definition: "when the source recorded the event", constraints: []string{"RFC 3339 instant"}})
	}
	for _, k := range eventKeyFields(e) {
		b.keyField("", k, "keys."+k, "BUSINESS_KEY")
	}
	for _, x := range e.Dimensions {
		path := "dimensions." + x
		if x == "COMPANY" {
			path = "company"
		}
		label, cons := b.dimensionLabel(x)
		b.field(GraphNode{Code: x, Category: "DIMENSION", DataType: "CODE", Field: path, Label: label, definition: label, constraints: cons})
	}
	for _, m := range e.Measures {
		b.measureField("", m, "measures."+m)
	}
	for _, l := range e.Lists {
		lid := b.add(GraphNode{ID: "list:" + l.Code, Kind: NodeList, Code: l.Code, Label: l.Code, Field: "lists." + l.Code,
			Detail: "one item per " + strings.Join(l.ItemKey, " + ")})
		for _, k := range l.ItemKey {
			id := b.keyField(l.Code, k, "lists."+l.Code+"[].keys."+k, "BUSINESS_KEY")
			b.link(id, lid, RoleItemIdentity, fmt.Sprintf("identifies each %s item", l.Code), true, "", "")
		}
		for _, k := range l.Keys {
			if !oneOf(k, l.ItemKey) {
				b.keyField(l.Code, k, "lists."+l.Code+"[].keys."+k, "LINK_KEY")
			}
		}
		for _, m := range l.Measures {
			b.measureField(l.Code, m, "lists."+l.Code+"[].measures."+m)
		}
	}
	b.authorities()
}

// eventKeyFields: the keys carried at event level, economic key first.
func eventKeyFields(e EventType) []string {
	out := append([]string{}, e.EconomicKey...)
	for _, k := range eventKeys(e) {
		if !oneOf(k, out) {
			out = append(out, k)
		}
	}
	return out
}

// authorities records, per source field, which source systems may supply it (US-005).
func (b *graphBuilder) authorities() {
	var all []string
	specific := map[string][]string{}
	for _, a := range b.d.SourceAuthority {
		if a.EventType != b.ev.Code {
			continue
		}
		if !oneOf(a.SourceSystem, all) {
			all = append(all, a.SourceSystem)
		}
		if a.Measure != AllMeasures && !oneOf(a.SourceSystem, specific[a.Measure]) {
			specific[a.Measure] = append(specific[a.Measure], a.SourceSystem)
		}
	}
	var wild []string
	for _, a := range b.d.SourceAuthority {
		if a.EventType == b.ev.Code && a.Measure == AllMeasures && !oneOf(a.SourceSystem, wild) {
			wild = append(wild, a.SourceSystem)
		}
	}
	for i := range b.g.Nodes {
		n := &b.g.Nodes[i]
		if n.Kind != NodeField {
			continue
		}
		switch {
		case n.Category == "MEASURE" && len(specific[n.Code]) > 0:
			n.sourceSystems = specific[n.Code]
		case n.Category == "MEASURE":
			n.sourceSystems = wild
		default:
			n.sourceSystems = all
		}
	}
}

// ── controls: identity, currency, keys and the book date ────────────────────

func (b *graphBuilder) control(code, label, detail string) string {
	return b.add(GraphNode{ID: "control:" + code, Kind: NodeControl, Code: code, Label: label, Detail: detail})
}

func (b *graphBuilder) controls() {
	e := b.ev
	id := b.control("IDENTITY", "Source record identity", "deduplicates by source system, source record id and version (US-010)")
	b.link(fieldID("", "SOURCE_EVENT_ID"), id, RoleIdentity, "identifies the source record, so a resend is not posted twice", true, "EVENT_RECOGNITION", e.Code)
	b.link(fieldID("", "SOURCE_EVENT_VERSION"), id, RoleIdentity, "tells a correction from a resend of the same record", true, "EVENT_RECOGNITION", e.Code)

	cur := b.control("CURRENCY", "Event currency", "every amount of the event is in this currency")
	b.link(fieldID("", "CURRENCY"), cur, RoleCurrency, "currency of every amount of "+e.Code, true, "EVENT_RECOGNITION", e.Code)

	if len(e.EconomicKey) > 0 {
		shape := e.Code + ":<" + strings.Join(e.EconomicKey, ">:<") + ">"
		econ := b.control("ECONOMIC_KEY", "Economic event key", shape)
		for _, k := range e.EconomicKey {
			b.link(fieldID("", k), econ, RoleEconomicKey, "part of the economic event key "+shape+", which posts each economic event once", true, "EVENT_RECOGNITION", e.Code)
		}
	}
	kg := b.control("KEY_GRAPH", "Key graph", "business keys link this event to the policies, invoices and receipts it concerns (US-010)")
	keyUse := func(list, k string) {
		label := "business key " + k + " in the key graph"
		if p, ok := b.keyProfile(k); ok && len(p.Links) > 0 {
			var to []string
			for _, l := range p.Links {
				to = append(to, l.To)
			}
			label += " (links to " + strings.Join(to, ", ") + ")"
		}
		b.link(fieldID(list, k), kg, RoleKeyGraph, label, true, "KEY_PROFILE", k)
	}
	for _, k := range eventKeyFields(e) {
		keyUse("", k)
	}
	for _, l := range e.Lists {
		for _, k := range itemKeys(l) {
			keyUse(l.Code, k)
		}
	}

	if p := b.policy; p != nil {
		detail := fmt.Sprintf("date policy %s: book date = %s, calendar %s", p.Code, p.BookDate, p.Calendar)
		if p.Advance {
			detail += ", advance classification"
		}
		period := b.control("PERIOD", "Book date and accounting period", detail)
		inputs := []string{"TRANSACTION_DATE", "EFFECTIVE_DATE"}
		if p.BookDate == "RECORDED_DATE" {
			inputs = append(inputs, "RECORDED_AT")
		}
		for _, in := range inputs {
			b.link(fieldID("", in), period, RolePeriod, fmt.Sprintf("the period decision of date policy %s reads %s", p.Code, in), true, "DATE_POLICY", p.Code)
		}
		values := []struct{ code, typ, def string }{
			{"BOOK_DATE", "DATE", "the date the event books on"},
			{"ACCOUNTING_PERIOD", "STRING", "the accounting period the event books into"},
		}
		if p.Advance {
			values = append(values, struct{ code, typ, def string }{"BOOKING_CLASS", "CODE", "A (advance: effective after the book period) or C (current)"})
		}
		for _, v := range values {
			pid := b.add(GraphNode{ID: "policy:" + v.code, Kind: NodePolicyValue, Code: v.code, Label: v.def, Detail: v.code + " from date policy " + p.Code,
				DataType: v.typ, definition: v.def, reqKind: "DATE_POLICY", reqRef: p.Code, formulaInputs: inputs})
			for _, in := range inputs {
				b.link(fieldID("", in), pid, RoleInput, fmt.Sprintf("input of %s (date policy %s)", v.code, p.Code), true, "DATE_POLICY", p.Code)
			}
		}
	} else {
		period := b.control("PERIOD", "Book date", "no date policy: the transaction date books the event")
		b.link(fieldID("", "TRANSACTION_DATE"), period, RolePeriod, "accounts are assigned on the book date, which is the transaction date (no date policy)", true, "ACCOUNT_ASSIGNMENT", "BOOK_DATE")
	}
}

// ── calculations: derived measures and lookup-derived dimensions ────────────

func (b *graphBuilder) calculations() {
	for _, x := range b.dims {
		if !oneOf(x, b.ev.Dimensions) {
			b.derivedDimension(x)
		}
	}
	for _, dm := range b.d.DerivedMeasures {
		b.derivedMeasure(dm.Code)
	}
}

func (b *graphBuilder) derivedMeasure(code string) string {
	id := "derived:" + code
	if b.has(id) {
		return id
	}
	idx := -1
	for i, dm := range b.d.DerivedMeasures {
		if dm.Code == code {
			idx = i
		}
	}
	if idx < 0 {
		return ""
	}
	dm := b.d.DerivedMeasures[idx]
	if len(dm.EventTypes) > 0 && !oneOf(b.ev.Code, dm.EventTypes) {
		return ""
	}
	node, perr := formula.Parse(dm.Formula)
	if perr != nil {
		return ""
	}
	if _, errs := formula.Check(node, b.syms); len(errs) > 0 {
		return "" // it does not apply to this event
	}
	label := dm.Description
	if label == "" {
		label = code + " = " + dm.Formula
	}
	ids := formula.Identifiers(node)
	b.add(GraphNode{ID: id, Kind: NodeDerivedMeasure, Code: code, Label: label, Detail: code + " = " + dm.Formula, Path: fmt.Sprintf("derivedMeasures/%d", idx),
		DataType: c2Type(dm.ResultType), definition: label, reqKind: "FORMULA", reqRef: code, formulaInputs: inputCodes(ids)})
	for _, name := range ids {
		b.link(b.resolve(name, nil), id, RoleInput, fmt.Sprintf("input of %s = %s", code, dm.Formula), true, "FORMULA", code)
	}
	return id
}

// inputCodes turns formula identifiers into the codes they read (LIST.FIELD → FIELD, LIST[] → LIST).
func inputCodes(ids []string) []string {
	var out []string
	for _, id := range ids {
		c := strings.TrimSuffix(id, "[]")
		if i := strings.IndexByte(c, '.'); i >= 0 {
			c = c[i+1:]
		}
		if !oneOf(c, out) {
			out = append(out, c)
		}
	}
	return out
}

func (b *graphBuilder) derivedDimension(code string) string {
	id := "dim:" + code
	if b.has(id) {
		return id
	}
	for _, x := range b.d.Dimensions {
		if x.Code != code || x.DerivedBy == nil {
			continue
		}
		for i, lk := range b.d.Lookups {
			if lk.Code != x.DerivedBy.Lookup {
				continue
			}
			label, _ := b.dimensionLabel(code)
			b.add(GraphNode{ID: id, Kind: NodeDerivedDimension, Code: code, Label: label,
				Detail: fmt.Sprintf("%s = lookup %s(%s)", code, lk.Code, lk.Input), Path: fmt.Sprintf("lookups/%d", i),
				DataType: "CODE", definition: label + " (derived by lookup " + lk.Code + ")", reqKind: "LOOKUP", reqRef: lk.Code, formulaInputs: []string{lk.Input}})
			b.link(b.resolve(lk.Input, nil), id, RoleLookupInput, fmt.Sprintf("input of lookup %s, which derives %s", lk.Code, code), true, "LOOKUP", lk.Code)
			dated := false
			for _, en := range lk.Entries {
				dated = dated || en.EffectiveFrom != "" || en.EffectiveTo != ""
			}
			if dated {
				which := "EFFECTIVE_DATE"
				if b.policy != nil && b.policy.LookupDate != "" {
					which = b.policy.LookupDate
				}
				b.link(fieldID("", which), id, RoleLookupDate, fmt.Sprintf("selects the effective-dated entries of lookup %s", lk.Code), false, "LOOKUP", lk.Code)
			}
			return id
		}
	}
	return ""
}

// resolve finds the node a formula identifier reads, within a list item's scope when l is set.
func (b *graphBuilder) resolve(name string, l *EventList) string {
	if strings.HasSuffix(name, "[]") {
		return b.ifHas("list:" + strings.TrimSuffix(name, "[]"))
	}
	if i := strings.IndexByte(name, '.'); i > 0 {
		return b.ifHas(fieldID(name[:i], name[i+1:]))
	}
	if l != nil {
		if id := fieldID(l.Code, name); b.has(id) {
			return id
		}
	}
	switch name {
	case "TRANSACTION_DATE", "EFFECTIVE_DATE":
		return fieldID("", name)
	case "BOOK_DATE", "ACCOUNTING_PERIOD", "BOOKING_CLASS":
		if id := b.ifHas("policy:" + name); id != "" {
			return id
		}
	}
	if _, ok := findDerived(b.d, name); ok {
		return b.derivedMeasure(name)
	}
	if id := fieldID("", name); b.has(id) {
		return id
	}
	if oneOf(name, b.dims) {
		return b.derivedDimension(name)
	}
	return ""
}

func (b *graphBuilder) resolveKey(name string, l *EventList) string {
	if l != nil {
		if id := fieldID(l.Code, name); b.has(id) {
			return id
		}
	}
	return b.ifHas(fieldID("", name))
}

func identifiers(src string) []string {
	if strings.TrimSpace(src) == "" {
		return nil
	}
	n, err := formula.Parse(src)
	if err != nil {
		return nil
	}
	return formula.Identifiers(n)
}

// ── rules, lines, classes and accounts ──────────────────────────────────────

func sideWord(s string) string {
	if s == "C" {
		return "Credit"
	}
	return "Debit"
}

func (b *graphBuilder) rules() {
	for i, r := range b.d.PostingRules {
		if r.EventType != b.ev.Code || !r.IsActive() {
			continue // a deactivated rule reads nothing
		}
		rp := fmt.Sprintf("postingRules/%d", i)
		filter := strings.TrimSpace(r.Filter)
		detail := "applies to every " + b.ev.Code
		if filter != "" {
			detail = "applies when " + filter
		}
		label := r.Description
		if label == "" {
			label = r.Code
		}
		rid := b.add(GraphNode{ID: "rule:" + r.Code, Kind: NodeRule, Code: r.Code, Label: label, Detail: detail, Path: rp})
		for _, name := range identifiers(filter) {
			b.link(b.resolve(name, nil), rid, RoleFilter, fmt.Sprintf("filter of rule %s: %s", r.Code, filter), false, "FILTER", r.Code)
		}
		for j, l := range r.Lines {
			var list *EventList
			for k := range b.ev.Lists {
				if b.ev.Lists[k].Code == l.ExpandBy {
					list = &b.ev.Lists[k]
				}
			}
			where := fmt.Sprintf("line %s of rule %s", l.Code, r.Code)
			ref := r.Code + "/" + l.Code
			text := fmt.Sprintf("%s %s = %s", sideWord(l.Side), l.Class, l.Amount)
			if list != nil {
				text += " per " + list.Code + " item"
			}
			label := l.Explanation
			if label == "" {
				label = text
			}
			lid := b.add(GraphNode{ID: "line:" + ref, Kind: NodeLine, Code: l.Code, Label: label, Detail: text, Path: fmt.Sprintf("%s/lines/%d", rp, j), Rule: r.Code})
			for _, name := range identifiers(l.Amount) {
				b.link(b.resolve(name, list), lid, RoleAmount, fmt.Sprintf("amount of %s (%s)", where, text), true, "POSTING_RULE", ref)
			}
			for _, name := range identifiers(l.When) {
				b.link(b.resolve(name, list), lid, RoleCondition, fmt.Sprintf("condition of %s: %s", where, strings.TrimSpace(l.When)), false, "POSTING_RULE", ref)
			}
			if strings.TrimSpace(l.Entity) != "" {
				for _, name := range identifiers(l.Entity) {
					b.link(b.resolve(name, list), lid, RoleEntity, fmt.Sprintf("legal entity of %s: %s", where, strings.TrimSpace(l.Entity)), true, "POSTING_RULE", ref)
				}
			} else {
				b.link(b.resolve("COMPANY", list), lid, RoleDefaultEntity, fmt.Sprintf("legal entity of %s (the event's COMPANY)", where), true, "POSTING_RULE", ref)
			}
			if list != nil {
				b.link("list:"+list.Code, lid, RoleExpands, fmt.Sprintf("%s posts one leg per %s item", where, list.Code), true, "POSTING_RULE", ref)
			}
			cid := b.class(l.Class)
			b.link(lid, cid, RoleClass, fmt.Sprintf("%s posts to class %s", where, l.Class), true, "", "")
			b.accounts(l.Class, list)
		}
	}
}

func (b *graphBuilder) class(code string) string {
	id := "class:" + code
	if b.has(id) {
		return id
	}
	n := GraphNode{ID: id, Kind: NodeClass, Code: code, Label: code}
	for i, c := range b.d.AccountingClasses {
		if c.Code == code {
			n.Label, n.Path = c.Name, fmt.Sprintf("accountingClasses/%d", i)
			n.Detail = fmt.Sprintf("%s, normal side %s", strings.ToLower(c.Nature), sideWord(c.Side()))
		}
	}
	return b.add(n)
}

func describeMatch(r AssignmentRule) string {
	if len(r.Match) == 0 {
		return r.Code + " (any line)"
	}
	var parts []string
	for _, k := range sortedKeys(r.Match) {
		parts = append(parts, k+" = "+r.Match[k])
	}
	return r.Code + " (" + strings.Join(parts, ", ") + ")"
}

func (b *graphBuilder) accounts(class string, list *EventList) {
	cid := "class:" + class
	var rules []AssignmentRule
	for _, r := range b.d.AssignmentRules {
		if r.Class == class {
			rules = append(rules, r)
		}
	}
	for i, a := range b.d.Accounts {
		if a.Class != class {
			continue
		}
		label := a.Name
		if label == "" {
			label = a.Code
		}
		aid := b.add(GraphNode{ID: "account:" + a.Code, Kind: NodeAccount, Code: a.Code, Label: label, Detail: "ledger " + a.LedgerOf(), Path: fmt.Sprintf("accounts/%d", i)})
		var by []string
		for _, r := range rules {
			if r.Account == a.Code {
				by = append(by, describeMatch(r))
			}
		}
		if len(by) > 0 {
			b.link(cid, aid, RoleAssigns, "chosen by "+strings.Join(by, "; "), true, "", "")
		} else {
			b.link(cid, aid, RoleInClass, "in class "+class+", but no assignment rule chooses it", false, "", "")
		}
		if a.SubledgerKey != "" {
			b.link(b.resolveKey(a.SubledgerKey, list), aid, RoleSubledgerKey, fmt.Sprintf("account %s requires subledger key %s on every line", a.Code, a.SubledgerKey), true, "ACCOUNT_ASSIGNMENT", a.Code)
		}
		for _, x := range a.Dimensions {
			b.link(b.resolve(x, list), aid, RoleAccountDimension, fmt.Sprintf("account %s requires dimension %s on every line", a.Code, x), true, "ACCOUNT_ASSIGNMENT", a.Code)
		}
	}
	for _, r := range rules {
		aid := "account:" + r.Account
		if !b.has(aid) {
			continue
		}
		for _, k := range sortedKeys(r.Match) {
			var src string
			switch k {
			case "ENTITY":
				continue // the line's legal entity, already needed by the line
			case "CURRENCY":
				src = fieldID("", "CURRENCY")
			default:
				src = b.resolve(k, list)
			}
			// The value is mandatory only when every rule of the class names it: otherwise a
			// rule without it can still choose an account.
			every := true
			for _, o := range rules {
				if _, ok := o.Match[k]; !ok {
					every = false
				}
			}
			b.link(src, aid, RoleMatch, fmt.Sprintf("assignment rule %s chooses %s for class %s when %s = %s", r.Code, r.Account, class, k, r.Match[k]),
				every, "ACCOUNT_ASSIGNMENT", r.Code)
		}
	}
}
