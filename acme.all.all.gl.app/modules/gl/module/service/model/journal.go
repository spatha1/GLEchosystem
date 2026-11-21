package model

// ALTB-05 — journal entries (contract C6) from a posting plan. A posting run turns each event's
// plan into one entry per legal entity and currency, each balanced on its own, linked by one
// group. The plan comes from the same engine call as a preview (PreviewPostingWith); this file
// only gives it ledger identities.

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"gl/formula"
)

// JournalSchemaVersion is the C6 schema the entries follow.
const JournalSchemaVersion = "gl.journal/v1"

// JournalEntry is one posted entry (C6): one per event, legal entity and currency.
type JournalEntry struct {
	SchemaVersion    string        `json:"schemaVersion"`
	JournalID        string        `json:"journalId"`
	GroupID          string        `json:"groupId"`
	TenantID         string        `json:"tenantId"`
	EconomicEventKey string        `json:"economicEventKey"`
	EventType        string        `json:"eventType"`
	LegalEntity      string        `json:"legalEntity"`
	Currency         string        `json:"currency"`
	IntakeMode       string        `json:"intakeMode"`
	BookingDate      string        `json:"bookingDate"`
	EffectiveDate    string        `json:"effectiveDate,omitempty"`
	AccountingPeriod string        `json:"accountingPeriod"`
	Status           string        `json:"status"`
	ConfigID         string        `json:"configId"`
	ConfigVersion    int           `json:"configVersion"`
	Lines            []JournalLine `json:"lines"`
}

// JournalLine is one line of an entry (C6).
type JournalLine struct {
	LineID          string            `json:"lineId"`
	Account         string            `json:"account"`
	AccountingClass string            `json:"accountingClass"`
	Side            string            `json:"side"`
	Amount          string            `json:"amount"`
	Dimensions      map[string]string `json:"dimensions,omitempty"`
	SubledgerKey    string            `json:"subledgerKey,omitempty"`
	SourceRowIDs    []string          `json:"sourceRowIds"`
	RuleID          string            `json:"ruleId"`
	RuleVersion     int               `json:"ruleVersion"`
	Explanation     string            `json:"explanation,omitempty"`
}

// LedgerLine is one posted line with what the ledger reads back (OPEN_BALANCE and balances):
// a store saves these and loads them into a ledger.
type LedgerLine struct {
	JournalID string            `json:"journalId"`
	LineID    string            `json:"lineId"`
	Class     string            `json:"class"`
	Account   string            `json:"account"`
	Side      string            `json:"side"`
	Entity    string            `json:"entity"`
	Currency  string            `json:"currency"`
	BookDate  string            `json:"bookDate"`
	Amount    string            `json:"amount"`
	Keys      map[string]string `json:"keys,omitempty"`
}

// EconomicEventKey identifies the economic event independently of source and intake mode:
// the event type and its economic key values, e.g. INVOICED:RUN1:P1:1.
func EconomicEventKey(d *Document, in EventInput) string {
	parts := []string{in.EventType}
	for _, e := range d.Events {
		if e.Code == in.EventType {
			for _, k := range e.EconomicKey {
				parts = append(parts, strings.TrimSpace(in.Keys[k]))
			}
		}
	}
	return strings.Join(parts, ":")
}

func intakeModeOf(d *Document, in EventInput) string {
	for _, a := range d.SourceAuthority {
		if a.EventType == in.EventType && (in.SourceSystem == "" || a.SourceSystem == in.SourceSystem) && a.IntakeMode != "" {
			return a.IntakeMode
		}
	}
	return "RAW_EVENT"
}

// Journals turns a balanced, fully assigned plan into C6 entries, and the lines a ledger keeps.
// Line ids are stable: the same event and configuration always give the same ids.
func Journals(d *Document, tenant string, in EventInput, p *PostingPlan) ([]JournalEntry, []LedgerLine, error) {
	if p == nil || len(p.Lines) == 0 {
		return nil, nil, &CodeError{"UNBALANCED", "nothing to post"}
	}
	if !p.Balanced {
		return nil, nil, &CodeError{"UNBALANCED", "the plan does not balance for every legal entity and currency"}
	}
	period := ""
	if p.Period != nil {
		period = p.Period.AccountingPeriod
	}
	if period == "" && len(p.BookDate) >= 7 {
		period = p.BookDate[:7]
	}
	econ := EconomicEventKey(d, in)
	row := in.SourceSystem + ":" + in.SourceEventID
	if in.SourceEventID == "" {
		row = econ
	}
	subledger := map[string]string{}
	for _, a := range d.Accounts {
		subledger[a.Code] = a.SubledgerKey
	}
	type ek struct{ entity, currency string }
	entries := map[ek]*JournalEntry{}
	var order []ek
	var kept []LedgerLine
	for _, l := range p.Lines {
		if l.Problem != "" || l.Account == "" {
			return nil, nil, &CodeError{"MISSING_ACCOUNT", fmt.Sprintf("line %d (%s %s) has no usable account: %s", l.LineNo, l.Class, l.Side, l.Problem)}
		}
		k := ek{l.Entity, l.Currency}
		e, ok := entries[k]
		if !ok {
			e = &JournalEntry{SchemaVersion: JournalSchemaVersion, JournalID: "J:" + econ + ":" + l.Entity + ":" + l.Currency, GroupID: "G:" + econ,
				TenantID: tenant, EconomicEventKey: econ, EventType: in.EventType, LegalEntity: l.Entity, Currency: l.Currency,
				IntakeMode: intakeModeOf(d, in), BookingDate: p.BookDate, EffectiveDate: in.EffectiveDate, AccountingPeriod: period,
				Status: "posted", ConfigID: p.Config.ConfigID, ConfigVersion: p.Config.Version}
			entries[k] = e
			order = append(order, k)
		}
		id := e.JournalID + "/" + l.Template
		src := row
		if l.Item > 0 {
			id += "/" + strconv.Itoa(l.Item)
			src += "#" + strconv.Itoa(l.Item)
		}
		jl := JournalLine{LineID: id, Account: l.Account, AccountingClass: l.Class, Side: l.Side, Amount: l.Amount, SourceRowIDs: []string{src},
			RuleID: p.Rule, RuleVersion: p.Config.Version, Explanation: l.Explanation}
		if len(l.Dimensions) > 0 {
			jl.Dimensions = l.Dimensions
		}
		if key := subledger[l.Account]; key != "" {
			jl.SubledgerKey = key + ":" + l.Keys[key]
		}
		e.Lines = append(e.Lines, jl)
		kept = append(kept, LedgerLine{JournalID: e.JournalID, LineID: id, Class: l.Class, Account: l.Account, Side: l.Side, Entity: l.Entity,
			Currency: l.Currency, BookDate: p.BookDate, Amount: l.Amount, Keys: l.Keys})
	}
	out := make([]JournalEntry, 0, len(order))
	for _, k := range order {
		e := entries[k]
		if err := balanced(*e); err != nil {
			return nil, nil, err
		}
		out = append(out, *e)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].JournalID < out[j].JournalID })
	return out, kept, nil
}

// balanced checks C6's invariant on one entry: debits equal credits exactly.
func balanced(e JournalEntry) error {
	dr, cr := new(big.Rat), new(big.Rat)
	for _, l := range e.Lines {
		r, err := formula.ParseDecimal(l.Amount)
		if err != nil {
			return err
		}
		if l.Side == "D" {
			dr.Add(dr, r)
		} else {
			cr.Add(cr, r)
		}
	}
	if dr.Cmp(cr) != 0 || len(e.Lines) < 2 {
		return &CodeError{"UNBALANCED", fmt.Sprintf("entry %s does not balance", e.JournalID)}
	}
	return nil
}

// InputDigest identifies an event's content: a resend with the same digest changes nothing.
func InputDigest(in EventInput) string { return "sha256:" + digestOf(in) }
