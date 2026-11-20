package model

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// US-010 — event identifiers, business keys and the key graph.
//
// Business keys (POLICY_NO, INVOICE_NO, RECEIPT_NO, …) are configured with a format, an
// optional scope (TERM_NO is unique only within its POLICY_NO) and ownership links to other
// keys (an INVOICE_NO belongs to one POLICY_NO; an ALLOCATION_ID belongs to one RECEIPT_NO and
// one INVOICE_NO). Events assert keys together: an invoice item carries its INVOICE_NO next to
// the event's POLICY_NO; an allocation carries its INVOICE_NO next to the receipt's RECEIPT_NO.
//
// The key graph built from accepted events answers two questions: what is linked to a key
// (Related), and what a rebuild of a key must reprocess (Rebuild). A missing, malformed or
// conflicting key, or a colliding source or economic identity, becomes an exception case and
// the event changes nothing in the graph.

// KeyProfile defines one business key.
type KeyProfile struct {
	Code            string    `json:"code"`
	Description     string    `json:"description,omitempty"`
	Pattern         string    `json:"pattern,omitempty"` // a value must match it entirely
	CaseInsensitive bool      `json:"caseInsensitive,omitempty"`
	Scope           []string  `json:"scope,omitempty"` // the value is unique only within these keys
	Links           []KeyLink `json:"links,omitempty"`
}

// KeyLink relates this key to another. MANY_TO_ONE and ONE_TO_ONE are ownership: the other
// key is this key's parent, and a value can have only one parent of that key.
type KeyLink struct {
	To          string `json:"to"`
	Cardinality string `json:"cardinality"` // MANY_TO_ONE | ONE_TO_ONE | MANY_TO_MANY
}

var cardinalities = []string{"MANY_TO_ONE", "ONE_TO_ONE", "MANY_TO_MANY"}

func init() { validators = append(validators, validateKeys) }

// eventKeys lists the business keys an event carries at event level: its declared keys, or
// its economic key when it declares none.
func eventKeys(e EventType) []string {
	if len(e.Keys) > 0 {
		return e.Keys
	}
	return e.EconomicKey
}

func itemKeys(l EventList) []string { return append(append([]string{}, l.ItemKey...), l.Keys...) }

func scopeCycle(p map[string]KeyProfile, start string) bool {
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(c string) bool {
		for _, s := range p[c].Scope {
			if s == start {
				return true
			}
			if !seen[s] {
				seen[s] = true
				if walk(s) {
					return true
				}
			}
		}
		return false
	}
	return walk(start)
}

func validateKeys(d *Document) []Finding {
	var fs []Finding
	if len(d.KeyProfiles) == 0 {
		if len(d.Events) > 0 {
			fs = append(fs, warnf("keyProfiles", "MISSING_KEY_PROFILES", "no key profiles yet: keys are not format-checked and rebuilds cannot follow ownership links"))
		}
		return fs
	}
	profiles := map[string]KeyProfile{}
	for i, k := range d.KeyProfiles {
		p := fmt.Sprintf("keyProfiles/%d", i)
		if !isCode(k.Code) {
			fs = append(fs, errf(p, "INVALID_CODE", "key code %q must be an upper-case code", k.Code))
		}
		if _, dup := profiles[k.Code]; dup {
			fs = append(fs, errf(p, "DUPLICATE_KEY", "key %s is declared twice", k.Code))
		}
		profiles[k.Code] = k
		if k.Pattern != "" {
			if _, err := regexp.Compile("^(?:" + k.Pattern + ")$"); err != nil {
				fs = append(fs, errf(p, "INVALID_PATTERN", "key %s pattern %q: %v", k.Code, k.Pattern, err))
			}
		}
	}
	for i, k := range d.KeyProfiles {
		p := fmt.Sprintf("keyProfiles/%d", i)
		for _, s := range k.Scope {
			if _, ok := profiles[s]; !ok {
				fs = append(fs, errf(p, "UNKNOWN_KEY", "key %s is scoped by %s, which is not a key profile", k.Code, s))
			}
		}
		if scopeCycle(profiles, k.Code) {
			fs = append(fs, errf(p, "INVALID_SCOPE", "key %s is scoped by itself, directly or through other keys", k.Code))
		}
		for _, l := range k.Links {
			if _, ok := profiles[l.To]; !ok {
				fs = append(fs, errf(p, "UNKNOWN_KEY", "key %s links to %s, which is not a key profile", k.Code, l.To))
			}
			if l.To == k.Code {
				fs = append(fs, errf(p, "INVALID_LINK", "key %s cannot link to itself", k.Code))
			}
			if !oneOf(l.Cardinality, cardinalities) {
				fs = append(fs, errf(p, "INVALID_LINK", "cardinality %q must be one of %v", l.Cardinality, cardinalities))
			}
		}
	}
	for i, e := range d.Events {
		p := fmt.Sprintf("events/%d", i)
		top := eventKeys(e)
		known := func(where string, keys []string) {
			for _, k := range keys {
				if _, ok := profiles[k]; !ok {
					fs = append(fs, errf(p, "UNKNOWN_KEY", "event %s uses key %s (%s), which is not a key profile", e.Code, k, where))
				}
			}
		}
		scoped := func(where string, keys, carried []string) {
			for _, k := range keys {
				for _, s := range profiles[k].Scope {
					if !oneOf(s, carried) {
						fs = append(fs, errf(p, "SCOPE_NOT_CARRIED", "event %s carries %s (%s) without %s, which scopes it", e.Code, k, where, s))
					}
				}
			}
		}
		known("economicKey", e.EconomicKey)
		known("keys", e.Keys)
		for _, k := range e.EconomicKey {
			if !oneOf(k, top) {
				fs = append(fs, errf(p, "ECONOMIC_KEY_NOT_CARRIED", "event %s identifies by %s but does not carry it", e.Code, k))
			}
		}
		scoped("event level", top, top)
		for _, l := range e.Lists {
			known("list "+l.Code, itemKeys(l))
			scoped("list "+l.Code, itemKeys(l), append(append([]string{}, top...), itemKeys(l)...))
		}
	}
	return fs
}

// KeyException is a key or identity problem (an exception case, contract C7). A rejected
// event changes nothing in the graph.
type KeyException struct {
	Category           string `json:"category"` // MISSING_KEY | INVALID_KEY | UNKNOWN_CODE | KEY_CONFLICT | DUPLICATE_EVENT | TENANT_MISMATCH
	EconomicEventKey   string `json:"economicEventKey,omitempty"`
	SourceSystem       string `json:"sourceSystem,omitempty"`
	SourceEventID      string `json:"sourceEventId,omitempty"`
	SourceEventVersion int    `json:"sourceEventVersion,omitempty"`
	Detail             string `json:"detail"`
}

// KeyRef is one node of the graph. A scoped key's value is qualified by its scope's values,
// e.g. TERM_NO "P1/1"; a "/" or "\" inside a value is escaped with "\".
type KeyRef struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// AddResult says what happened to an event.
type AddResult string

const (
	Accepted  AddResult = "ACCEPTED"  // the graph now includes it
	Unchanged AddResult = "UNCHANGED" // an identical resend of an accepted event: nothing to do
	Rejected  AddResult = "REJECTED"  // an exception was recorded; the graph is unchanged
)

type ownership struct {
	child, parent KeyRef
	oneToOne      bool
}

type acceptedEvent struct {
	id      string // sourceSystem/sourceEventId
	system  string
	version int
	digest  string
	econ    string
	groups  [][]KeyRef // the key sets asserted together: the event level, then each list item
	links   []ownership
}

type keyIndex struct {
	adj      map[KeyRef]map[KeyRef]bool
	up       map[KeyRef]map[string]KeyRef // child → parent key → parent
	down     map[KeyRef]map[KeyRef]bool   // parent → children
	economic map[string]string            // sourceSystem|economicEventKey → event id
}

func newIndex() *keyIndex {
	return &keyIndex{adj: map[KeyRef]map[KeyRef]bool{}, up: map[KeyRef]map[string]KeyRef{}, down: map[KeyRef]map[KeyRef]bool{}, economic: map[string]string{}}
}

// KeyGraph is one tenant's graph of business keys. It accepts only that tenant's events, so
// keys of different tenants never meet even when their values are equal.
type KeyGraph struct {
	Tenant     string
	doc        *Document
	profiles   map[string]KeyProfile
	patterns   map[string]*regexp.Regexp
	hasLinks   bool
	order      []string
	events     map[string]*acceptedEvent
	idx        *keyIndex
	Exceptions []KeyException
}

// NewKeyGraph starts an empty graph for a tenant under a validated configuration.
func NewKeyGraph(tenant string, d *Document) *KeyGraph {
	g := &KeyGraph{Tenant: tenant, doc: d, profiles: map[string]KeyProfile{}, patterns: map[string]*regexp.Regexp{},
		events: map[string]*acceptedEvent{}, idx: newIndex()}
	for _, k := range d.KeyProfiles {
		g.profiles[k.Code] = k
		if k.Pattern != "" {
			g.patterns[k.Code], _ = regexp.Compile("^(?:" + k.Pattern + ")$") // nil if invalid: every value is then rejected
		}
		for _, l := range k.Links {
			if l.Cardinality != "MANY_TO_MANY" {
				g.hasLinks = true
			}
		}
	}
	return g
}

func escapePart(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "/", `\/`)
}

// normalize trims a raw value and applies the key's case rule and format.
func (g *KeyGraph) normalize(code, raw string) (string, string) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", "MISSING_KEY"
	}
	p, ok := g.profiles[code]
	if !ok {
		return v, ""
	}
	if p.CaseInsensitive {
		v = strings.ToUpper(v)
	}
	if p.Pattern != "" {
		if re := g.patterns[code]; re == nil || !re.MatchString(v) {
			return "", "INVALID_KEY"
		}
	}
	return v, ""
}

// parts is a key's value preceded by the values of its scope, recursively.
func (g *KeyGraph) parts(code string, vals map[string]string, depth int) ([]string, string) {
	v, ok := vals[code]
	if !ok || depth > 16 {
		return nil, code
	}
	var out []string
	for _, s := range g.profiles[code].Scope {
		p, missing := g.parts(s, vals, depth+1)
		if missing != "" {
			return nil, missing
		}
		out = append(out, p...)
	}
	return append(out, v), ""
}

func (g *KeyGraph) qualify(vals map[string]string) ([]KeyRef, string) {
	codes := make([]string, 0, len(vals))
	for k := range vals {
		codes = append(codes, k)
	}
	sort.Strings(codes)
	refs := make([]KeyRef, 0, len(codes))
	for _, k := range codes {
		ps, missing := g.parts(k, vals, 0)
		if missing != "" {
			return nil, fmt.Sprintf("%s needs %s, which scopes it", k, missing)
		}
		for i := range ps {
			ps[i] = escapePart(ps[i])
		}
		refs = append(refs, KeyRef{k, strings.Join(ps, "/")})
	}
	return refs, ""
}

// Ref builds a graph node from raw key values, e.g. Ref("TERM_NO", {"POLICY_NO": "P1",
// "TERM_NO": "1"}) — the scope's values are needed for a scoped key.
func (g *KeyGraph) Ref(key string, values map[string]string) (KeyRef, error) {
	vals := map[string]string{}
	for k, raw := range values {
		v, bad := g.normalize(k, raw)
		if bad != "" {
			return KeyRef{}, &CodeError{bad, fmt.Sprintf("%s %q is missing or does not match its format", k, raw)}
		}
		vals[k] = v
	}
	ps, missing := g.parts(key, vals, 0)
	if missing != "" {
		return KeyRef{}, &CodeError{"MISSING_KEY", fmt.Sprintf("%s needs a value for %s", key, missing)}
	}
	for i := range ps {
		ps[i] = escapePart(ps[i])
	}
	return KeyRef{key, strings.Join(ps, "/")}, nil
}

func digestOf(in EventInput) string {
	b, _ := json.Marshal(in)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// resolve turns an event into the key groups and ownership links it asserts.
func (g *KeyGraph) resolve(ev EventType, in EventInput) (*acceptedEvent, string, string) {
	declared := eventKeys(ev)
	var extra []string
	for k := range in.Keys {
		if !oneOf(k, declared) {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		return nil, "INVALID_KEY", fmt.Sprintf("event %s does not declare key(s) %v; a mapping must produce only declared keys", ev.Code, extra)
	}
	top := map[string]string{}
	for _, k := range declared {
		v, bad := g.normalize(k, in.Keys[k])
		if bad == "MISSING_KEY" {
			return nil, bad, fmt.Sprintf("event %s is missing key %s", ev.Code, k)
		}
		if bad != "" {
			return nil, bad, fmt.Sprintf("%s %q does not match its configured format", k, in.Keys[k])
		}
		top[k] = v
	}
	econ := []string{ev.Code}
	for _, k := range ev.EconomicKey {
		econ = append(econ, top[k])
	}
	e := &acceptedEvent{econ: strings.Join(econ, ":")}
	refs, why := g.qualify(top)
	if why != "" {
		return e, "MISSING_KEY", why
	}
	e.groups = append(e.groups, refs)
	for _, l := range ev.Lists {
		items := map[string]int{}
		for i, item := range in.Lists[l.Code] {
			vals := map[string]string{}
			for k, v := range top {
				vals[k] = v
			}
			for _, k := range itemKeys(l) {
				v, bad := g.normalize(k, item[k])
				if bad == "MISSING_KEY" {
					return e, bad, fmt.Sprintf("%s item %d is missing key %s", l.Code, i+1, k)
				}
				if bad != "" {
					return e, bad, fmt.Sprintf("%s item %d: %s %q does not match its configured format", l.Code, i+1, k, item[k])
				}
				vals[k] = v
			}
			refs, why := g.qualify(vals)
			if why != "" {
				return e, "MISSING_KEY", fmt.Sprintf("%s item %d: %s", l.Code, i+1, why)
			}
			var id []string
			for _, r := range refs {
				if oneOf(r.Key, l.ItemKey) {
					id = append(id, r.Key+"="+r.Value)
				}
			}
			if j, dup := items[strings.Join(id, ",")]; dup {
				return e, "KEY_CONFLICT", fmt.Sprintf("%s items %d and %d have the same item key %v", l.Code, j, i+1, id)
			}
			items[strings.Join(id, ",")] = i + 1
			e.groups = append(e.groups, refs)
		}
	}
	seen := map[ownership]bool{}
	for _, grp := range e.groups {
		for _, c := range grp {
			for _, l := range g.profiles[c.Key].Links {
				if l.Cardinality == "MANY_TO_MANY" {
					continue
				}
				for _, p := range grp {
					if p.Key == l.To {
						o := ownership{c, p, l.Cardinality == "ONE_TO_ONE"}
						if !seen[o] {
							seen[o] = true
							e.links = append(e.links, o)
						}
					}
				}
			}
		}
	}
	return e, "", ""
}

// conflict checks an event against an index without changing it.
func (x *keyIndex) conflict(e *acceptedEvent) (string, string) {
	if other, ok := x.economic[e.system+"|"+e.econ]; ok && other != e.id {
		return "DUPLICATE_EVENT", fmt.Sprintf("%s already reported economic event %s as %s; this looks like a duplicate", e.system, e.econ, other)
	}
	upNow := map[KeyRef]map[string]KeyRef{}
	parentOf := func(c KeyRef, key string) (KeyRef, bool) {
		if p, ok := upNow[c][key]; ok {
			return p, true
		}
		p, ok := x.up[c][key]
		return p, ok
	}
	for _, l := range e.links {
		if p, ok := parentOf(l.child, l.parent.Key); ok && p != l.parent {
			return "KEY_CONFLICT", fmt.Sprintf("%s %s already belongs to %s %s, not %s", l.child.Key, l.child.Value, l.parent.Key, p.Value, l.parent.Value)
		}
		if l.oneToOne {
			for c := range x.down[l.parent] {
				if c.Key == l.child.Key && c != l.child {
					return "KEY_CONFLICT", fmt.Sprintf("%s %s is already paired with %s %s", l.parent.Key, l.parent.Value, c.Key, c.Value)
				}
			}
			for c, ps := range upNow {
				if c.Key == l.child.Key && c != l.child && ps[l.parent.Key] == l.parent {
					return "KEY_CONFLICT", fmt.Sprintf("%s %s is paired with both %s %s and %s", l.parent.Key, l.parent.Value, c.Key, c.Value, l.child.Value)
				}
			}
		}
		if upNow[l.child] == nil {
			upNow[l.child] = map[string]KeyRef{}
		}
		upNow[l.child][l.parent.Key] = l.parent
	}
	return "", ""
}

func (x *keyIndex) apply(e *acceptedEvent) {
	for _, grp := range e.groups {
		for _, a := range grp {
			if x.adj[a] == nil {
				x.adj[a] = map[KeyRef]bool{}
			}
			for _, b := range grp {
				if a != b {
					x.adj[a][b] = true
				}
			}
		}
	}
	for _, l := range e.links {
		if x.up[l.child] == nil {
			x.up[l.child] = map[string]KeyRef{}
		}
		x.up[l.child][l.parent.Key] = l.parent
		if x.down[l.parent] == nil {
			x.down[l.parent] = map[KeyRef]bool{}
		}
		x.down[l.parent][l.child] = true
	}
	x.economic[e.system+"|"+e.econ] = e.id
}

// AddEvent applies one event. Its dedup identity is sourceSystem + sourceEventId +
// sourceEventVersion; its natural grain is the economic key plus each list's item key. A
// later version of an accepted event (a source correction) replaces what the earlier one
// asserted.
func (g *KeyGraph) AddEvent(in EventInput) AddResult {
	fail := func(cat, econ, detail string) AddResult {
		g.Exceptions = append(g.Exceptions, KeyException{Category: cat, EconomicEventKey: econ, SourceSystem: in.SourceSystem,
			SourceEventID: in.SourceEventID, SourceEventVersion: in.SourceEventVersion, Detail: detail})
		return Rejected
	}
	if in.TenantID != g.Tenant {
		// Foreign data is not read, and nothing of it is echoed into this tenant's exceptions.
		g.Exceptions = append(g.Exceptions, KeyException{Category: "TENANT_MISMATCH", Detail: "the event does not belong to this tenant and was not read"})
		return Rejected
	}
	if strings.TrimSpace(in.SourceSystem) == "" || strings.TrimSpace(in.SourceEventID) == "" || in.SourceEventVersion < 1 {
		return fail("MISSING_KEY", "", "every event needs sourceSystem, sourceEventId and sourceEventVersion ≥ 1: its dedup identity")
	}
	var ev *EventType
	for i := range g.doc.Events {
		if g.doc.Events[i].Code == in.EventType {
			ev = &g.doc.Events[i]
		}
	}
	if ev == nil {
		return fail("UNKNOWN_CODE", "", in.EventType+" is not in the event catalog")
	}
	id := in.SourceSystem + "/" + in.SourceEventID
	dig := digestOf(in)
	prev := g.events[id]
	if prev != nil {
		switch {
		case in.SourceEventVersion == prev.version && dig == prev.digest:
			return Unchanged
		case in.SourceEventVersion == prev.version:
			return fail("DUPLICATE_EVENT", prev.econ, fmt.Sprintf("%s version %d was already received with different content", id, prev.version))
		case in.SourceEventVersion < prev.version:
			return fail("DUPLICATE_EVENT", prev.econ, fmt.Sprintf("%s version %d is older than the accepted version %d", id, in.SourceEventVersion, prev.version))
		}
	}
	e, cat, detail := g.resolve(*ev, in)
	econ := ""
	if e != nil {
		econ = e.econ
	}
	if cat != "" {
		return fail(cat, econ, detail)
	}
	e.id, e.system, e.version, e.digest = id, in.SourceSystem, in.SourceEventVersion, dig

	idx := g.idx
	if prev != nil { // a correction: check against the graph without the version it replaces
		idx = newIndex()
		for _, o := range g.order {
			if o != id {
				idx.apply(g.events[o])
			}
		}
	}
	if cat, detail := idx.conflict(e); cat != "" {
		return fail(cat, econ, detail)
	}
	idx.apply(e)
	g.idx = idx
	if prev == nil {
		g.order = append(g.order, id)
	}
	g.events[id] = e
	return Accepted
}

func group(refs map[KeyRef]bool) map[string][]string {
	out := map[string][]string{}
	for r := range refs {
		out[r.Key] = append(out[r.Key], r.Value)
	}
	for k := range out {
		sort.Strings(out[k])
	}
	return out
}

func walk(start KeyRef, next func(KeyRef) map[KeyRef]bool) map[KeyRef]bool {
	visited := map[KeyRef]bool{start: true}
	queue := []KeyRef{start}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		for m := range next(n) {
			if !visited[m] {
				visited[m] = true
				queue = append(queue, m)
			}
		}
	}
	return visited
}

// Parents are a key value's owners, e.g. an allocation's receipt and invoice.
func (g *KeyGraph) Parents(r KeyRef) map[string]string {
	out := map[string]string{}
	for k, p := range g.idx.up[r] {
		out[k] = p.Value
	}
	return out
}

// Children are the key values a key value owns, e.g. a receipt's allocations.
func (g *KeyGraph) Children(r KeyRef) map[string][]string { return group(g.idx.down[r]) }

// Related is everything connected to a key value through any event: the full picture for an
// investigation. It can be large: a receipt split across two policies connects them.
func (g *KeyGraph) Related(r KeyRef) map[string][]string {
	all := walk(r, func(n KeyRef) map[KeyRef]bool { return g.idx.adj[n] })
	delete(all, r)
	return group(all)
}

// RebuildScope is what reprocessing one key value involves.
type RebuildScope struct {
	Start     KeyRef              `json:"start"`
	Traversal string              `json:"traversal"` // OWNERSHIP_LINKS | CONNECTED_COMPONENT
	Keys      map[string][]string `json:"keys"`
	Events    []string            `json:"events"` // sourceSystem/sourceEventId, in arrival order
}

// Rebuild finds what a rebuild of a key value must reprocess. It follows ownership links
// down (policy → invoices → allocations), then takes every event that asserted one of those
// keys, whole: a receipt with an allocation to this policy is reprocessed, but its allocation
// to another policy does not pull that policy in. Without ownership links it falls back to the
// connected component, a safe superset.
func (g *KeyGraph) Rebuild(r KeyRef) RebuildScope {
	out := RebuildScope{Start: r, Traversal: "OWNERSHIP_LINKS", Events: []string{}}
	var owned map[KeyRef]bool
	if g.hasLinks {
		owned = walk(r, func(n KeyRef) map[KeyRef]bool { return g.idx.down[n] })
	} else {
		out.Traversal = "CONNECTED_COMPONENT"
		owned = walk(r, func(n KeyRef) map[KeyRef]bool { return g.idx.adj[n] })
	}
	keys := map[KeyRef]bool{}
	for _, id := range g.order {
		touched := false
		for _, grp := range g.events[id].groups {
			for _, k := range grp {
				if owned[k] {
					touched = true
					for _, k2 := range grp {
						keys[k2] = true
					}
					break
				}
			}
		}
		if touched {
			out.Events = append(out.Events, id)
		}
	}
	for k := range owned {
		keys[k] = true
	}
	delete(keys, r)
	out.Keys = group(keys)
	return out
}
