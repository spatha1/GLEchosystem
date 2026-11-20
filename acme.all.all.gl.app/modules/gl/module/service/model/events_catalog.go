package model

import (
	_ "embed"
	"encoding/json"
)

// The OOTB insurance event catalog: templates for the business events of general insurance
// (policy lifecycle, billing and cash, distribution, claims, reinsurance, period end), each
// with its purpose, typical source, economic key, measures, dimensions and lists, and the
// business keys they use. A template is a starting point: applying it writes an ordinary
// event into a configuration, which the client then adapts — nothing about an event is fixed
// in code. The catalog is global and read-only, like the measure catalog (US-006).

//go:embed ootb_events.json
var ootbEventsJSON []byte

// EventTemplate is one catalog event.
type EventTemplate struct {
	Code        string      `json:"code"`
	Name        string      `json:"name"`
	Area        string      `json:"area"`
	Purpose     string      `json:"purpose"`
	SourceKind  string      `json:"sourceKind"`
	EconomicKey []string    `json:"economicKey"`
	Keys        []string    `json:"keys,omitempty"`
	Measures    []string    `json:"measures"`
	Dimensions  []string    `json:"dimensions"`
	Lists       []EventList `json:"lists,omitempty"`
}

// EventArea groups catalog events.
type EventArea struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// EventCatalog is the OOTB event catalog.
type EventCatalog struct {
	CatalogVersion string          `json:"catalogVersion"`
	Scope          string          `json:"scope"`
	Status         string          `json:"status"` // PROPOSED until the product owner approves the catalog
	Areas          []EventArea     `json:"areas"`
	Events         []EventTemplate `json:"events"`
	Keys           []KeyProfile    `json:"keys"`
}

var ootbEvents = func() (c EventCatalog) {
	if err := json.Unmarshal(ootbEventsJSON, &c); err != nil {
		panic("model: ootb_events.json: " + err.Error())
	}
	return c
}()

// OOTBEventCatalog returns a copy of the global event catalog.
func OOTBEventCatalog() EventCatalog {
	var c EventCatalog
	_ = json.Unmarshal(ootbEventsJSON, &c)
	return c
}

// Event is the configuration event a template starts.
func (t EventTemplate) Event() EventType {
	return EventType{Code: t.Code, Description: t.Purpose, EconomicKey: append([]string{}, t.EconomicKey...), Keys: append([]string{}, t.Keys...),
		Measures: append([]string{}, t.Measures...), Dimensions: append([]string{}, t.Dimensions...), Lists: append([]EventList{}, t.Lists...)}
}

// KeysFor returns the catalog key profiles a template needs: the keys it carries and every
// key they are scoped by or linked to, in catalog order.
func (c EventCatalog) KeysFor(t EventTemplate) []KeyProfile {
	byCode := map[string]KeyProfile{}
	for _, k := range c.Keys {
		byCode[k.Code] = k
	}
	need := map[string]bool{}
	var visit func(string)
	visit = func(code string) {
		if need[code] {
			return
		}
		need[code] = true
		k := byCode[code]
		for _, s := range k.Scope {
			visit(s)
		}
		for _, l := range k.Links {
			visit(l.To)
		}
	}
	for _, k := range append(append([]string{}, t.EconomicKey...), t.Keys...) {
		visit(k)
	}
	for _, l := range t.Lists {
		for _, k := range itemKeys(l) {
			visit(k)
		}
	}
	var out []KeyProfile
	for _, k := range c.Keys {
		if need[k.Code] {
			out = append(out, k)
		}
	}
	return out
}
