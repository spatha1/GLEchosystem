// Package contracts is the specification's executable walk-through of the canonical
// contracts (design/02_canonical_contracts). It is NOT the GL engine: it holds only the
// contract shapes, strict decoding and the invariants every implementation must keep,
// so a reviewer can run the premium, invoice and payment paths through the interface
// definitions without any service running. Standard library only.
package contracts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	codeRe     = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	dateRe     = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
	decimalRe  = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)
	currencyRe = regexp.MustCompile(`^[A-Z]{3}$`)
)

// SourceAuthority is one C1 sourceAuthority entry.
type SourceAuthority struct {
	EventType    string `json:"eventType"`
	Measure      string `json:"measure"`
	SourceSystem string `json:"sourceSystem"`
	IntakeMode   string `json:"intakeMode"`
}

// AuthoritySetup is a walk-through configuration fragment.
type AuthoritySetup struct {
	Description     string            `json:"description"`
	SourceAuthority []SourceAuthority `json:"sourceAuthority"`
}

// ListItem is one 1:N child of an event (e.g. one allocation of a receipt).
type ListItem struct {
	ItemID     string            `json:"itemId"`
	Keys       map[string]string `json:"keys,omitempty"`
	Measures   map[string]string `json:"measures,omitempty"`
	Dimensions map[string]string `json:"dimensions,omitempty"`
}

// Event is C4, the canonical financial event.
type Event struct {
	SchemaVersion      string                `json:"schemaVersion"`
	TenantID           string                `json:"tenantId"`
	SourceSystem       string                `json:"sourceSystem"`
	SourceEventID      string                `json:"sourceEventId"`
	SourceEventVersion int                   `json:"sourceEventVersion"`
	EventType          string                `json:"eventType"`
	EconomicEventKey   string                `json:"economicEventKey"`
	Company            string                `json:"company"`
	Currency           string                `json:"currency"`
	Dates              EventDates            `json:"dates"`
	Keys               map[string]string     `json:"keys"`
	Measures           map[string]string     `json:"measures"`
	Dimensions         map[string]string     `json:"dimensions,omitempty"`
	Lists              map[string][]ListItem `json:"lists,omitempty"`
	SourceRowIDs       []string              `json:"sourceRowIds"`
	MappingID          string                `json:"mappingId,omitempty"`
	MappingVersion     int                   `json:"mappingVersion,omitempty"`
	CollectedAt        string                `json:"collectedAt,omitempty"`
}

// EventDates keeps transaction, effective and recording time apart (never merged).
type EventDates struct {
	TransactionDate string `json:"transactionDate"`
	EffectiveDate   string `json:"effectiveDate"`
	RecordedAt      string `json:"recordedAt,omitempty"`
}

// SourceJournalLine is one line of a pre-balanced source journal (C5).
type SourceJournalLine struct {
	SourceLineID string            `json:"sourceLineId"`
	AccountRef   string            `json:"accountRef"`
	Side         string            `json:"side"`
	Amount       string            `json:"amount"`
	Dimensions   map[string]string `json:"dimensions,omitempty"`
}

// SourceJournal is C5.
type SourceJournal struct {
	SchemaVersion        string              `json:"schemaVersion"`
	TenantID             string              `json:"tenantId"`
	SourceSystem         string              `json:"sourceSystem"`
	SourceJournalID      string              `json:"sourceJournalId"`
	SourceJournalVersion int                 `json:"sourceJournalVersion"`
	EconomicEventKey     string              `json:"economicEventKey"`
	EventType            string              `json:"eventType"`
	Company              string              `json:"company"`
	Currency             string              `json:"currency"`
	BookingDate          string              `json:"bookingDate"`
	Lines                []SourceJournalLine `json:"lines"`
	SourceRowIDs         []string            `json:"sourceRowIds"`
	MappingID            string              `json:"mappingId,omitempty"`
	MappingVersion       int                 `json:"mappingVersion,omitempty"`
}

// AccountRef maps a source system's account reference to the client COA.
type AccountRef struct {
	Account         string `json:"account"`
	AccountingClass string `json:"accountingClass"`
}

// SourceJournalFeed is the walk-through file for setup B.
type SourceJournalFeed struct {
	Description   string                `json:"description"`
	AccountRefMap map[string]AccountRef `json:"accountRefMap"`
	Journals      []SourceJournal       `json:"journals"`
}

// JournalLine is a C6 line.
type JournalLine struct {
	LineID          string            `json:"lineId"`
	Account         string            `json:"account"`
	AccountingClass string            `json:"accountingClass"`
	Side            string            `json:"side"`
	Amount          string            `json:"amount"`
	Dimensions      map[string]string `json:"dimensions,omitempty"`
	SubledgerKey    string            `json:"subledgerKey,omitempty"`
	SourceRowIDs    []string          `json:"sourceRowIds"`
	SourceLineID    string            `json:"sourceLineId,omitempty"`
	RuleID          string            `json:"ruleId,omitempty"`
	RuleVersion     int               `json:"ruleVersion,omitempty"`
	Explanation     string            `json:"explanation,omitempty"`
}

// JournalEntry is C6: one per posting group, legal entity and currency.
type JournalEntry struct {
	SchemaVersion    string        `json:"schemaVersion"`
	JournalID        string        `json:"journalId"`
	GroupID          string        `json:"groupId"`
	TenantID         string        `json:"tenantId"`
	EconomicEventKey string        `json:"economicEventKey"`
	EventType        string        `json:"eventType,omitempty"`
	LegalEntity      string        `json:"legalEntity"`
	Currency         string        `json:"currency"`
	IntakeMode       string        `json:"intakeMode"`
	BookingDate      string        `json:"bookingDate"`
	EffectiveDate    string        `json:"effectiveDate,omitempty"`
	AccountingPeriod string        `json:"accountingPeriod"`
	Status           string        `json:"status"`
	ConfigID         string        `json:"configId"`
	ConfigVersion    int           `json:"configVersion"`
	MappingID        string        `json:"mappingId,omitempty"`
	MappingVersion   int           `json:"mappingVersion,omitempty"`
	SourceJournalID  string        `json:"sourceJournalId,omitempty"`
	ReversalOf       string        `json:"reversalOf,omitempty"`
	CorrectionOf     string        `json:"correctionOf,omitempty"`
	Lines            []JournalLine `json:"lines"`
}

// DecodeStrict reads a JSON file into v and rejects any field the contract does not define.
func DecodeStrict(path string, v any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// Cents parses a decimal string into integer minor units of a 2-decimal currency. It refuses
// floats in disguise: exponents, more than 2 decimals, or anything that is not a plain decimal.
func Cents(s string) (int64, error) {
	if !decimalRe.MatchString(s) {
		return 0, fmt.Errorf("%q is not a decimal string", s)
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, _ := strings.Cut(s, ".")
	if len(frac) > 2 {
		return 0, fmt.Errorf("%q has more than 2 decimal places", s)
	}
	frac = (frac + "00")[:2]
	n, err := strconv.ParseInt(whole+frac, 10, 64)
	if err != nil {
		return 0, err
	}
	if neg {
		n = -n
	}
	return n, nil
}

// ValidateEvent checks the C4 invariants that do not need configuration.
func ValidateEvent(e Event) []string {
	var p []string
	req := map[string]string{"tenantId": e.TenantID, "sourceSystem": e.SourceSystem, "sourceEventId": e.SourceEventID,
		"eventType": e.EventType, "economicEventKey": e.EconomicEventKey, "company": e.Company}
	for k, v := range req {
		if v == "" {
			p = append(p, "missing "+k)
		}
	}
	if e.SchemaVersion != "gl.event/v1" {
		p = append(p, "schemaVersion must be gl.event/v1")
	}
	for _, c := range []string{e.TenantID, e.EventType} {
		if !codeRe.MatchString(c) {
			p = append(p, fmt.Sprintf("%q is not a code", c))
		}
	}
	if !currencyRe.MatchString(e.Currency) {
		p = append(p, "currency must be ISO 4217")
	}
	if !dateRe.MatchString(e.Dates.TransactionDate) || !dateRe.MatchString(e.Dates.EffectiveDate) {
		p = append(p, "transactionDate and effectiveDate must be YYYY-MM-DD")
	}
	if e.SourceEventVersion < 1 {
		p = append(p, "sourceEventVersion must be >= 1")
	}
	if len(e.SourceRowIDs) == 0 {
		p = append(p, "sourceRowIds must not be empty")
	}
	for k, v := range e.Measures {
		if _, err := Cents(v); err != nil {
			p = append(p, "measure "+k+": "+err.Error())
		}
	}
	for list, items := range e.Lists {
		seen := map[string]bool{}
		for _, it := range items {
			if it.ItemID == "" || seen[it.ItemID] {
				p = append(p, "list "+list+": every item needs a unique itemId")
			}
			seen[it.ItemID] = true
			for k, v := range it.Measures {
				if _, err := Cents(v); err != nil {
					p = append(p, "list "+list+" measure "+k+": "+err.Error())
				}
			}
		}
	}
	sort.Strings(p)
	return p
}

// ValidateAuthority returns the (eventType, measure) pairs that do not have exactly one authority.
// Ambiguity is a configuration error, never resolved at runtime (01 §3).
func ValidateAuthority(setup AuthoritySetup) []string {
	count := map[string]int{}
	for _, a := range setup.SourceAuthority {
		count[a.EventType+"/"+a.Measure]++
	}
	var bad []string
	for k, n := range count {
		if n != 1 {
			bad = append(bad, fmt.Sprintf("%s has %d authorities", k, n))
		}
	}
	sort.Strings(bad)
	return bad
}

// Authoritative returns the authority entry that governs posting for an event type, by any of
// its measures, and whether the given source system and intake mode hold it.
func Authoritative(setup AuthoritySetup, eventType, sourceSystem, intakeMode string) bool {
	for _, a := range setup.SourceAuthority {
		if a.EventType == eventType && a.SourceSystem == sourceSystem && a.IntakeMode == intakeMode {
			return true
		}
	}
	return false
}

// Balance returns Σdebits − Σcredits of an entry in minor units.
func Balance(j JournalEntry) (int64, error) {
	var sum int64
	for _, l := range j.Lines {
		n, err := Cents(l.Amount)
		if err != nil {
			return 0, fmt.Errorf("%s: %w", l.LineID, err)
		}
		if n <= 0 {
			return 0, fmt.Errorf("%s: line amounts are positive magnitudes", l.LineID)
		}
		switch l.Side {
		case "D":
			sum += n
		case "C":
			sum -= n
		default:
			return 0, fmt.Errorf("%s: side must be D or C", l.LineID)
		}
	}
	return sum, nil
}

// ImportSourceJournal turns a pre-balanced source journal into a journal entry WITHOUT adding,
// dropping or re-deriving any line: one entry line per source line, same side and amount, the
// source line identity kept. An unknown account reference or an unbalanced journal is refused.
func ImportSourceJournal(sj SourceJournal, refs map[string]AccountRef, configID string, configVersion int) (JournalEntry, error) {
	j := JournalEntry{
		SchemaVersion: "gl.journal/v1", JournalID: "J:" + sj.EconomicEventKey + ":" + sj.Company + ":" + sj.Currency,
		GroupID: "G:" + sj.EconomicEventKey, TenantID: sj.TenantID, EconomicEventKey: sj.EconomicEventKey,
		EventType: sj.EventType, LegalEntity: sj.Company, Currency: sj.Currency, IntakeMode: "SOURCE_JOURNAL",
		BookingDate: sj.BookingDate, AccountingPeriod: sj.BookingDate[:7], Status: "posted",
		ConfigID: configID, ConfigVersion: configVersion, MappingID: sj.MappingID, MappingVersion: sj.MappingVersion,
		SourceJournalID: sj.SourceJournalID,
	}
	if len(sj.SourceRowIDs) != len(sj.Lines) {
		return JournalEntry{}, fmt.Errorf("source journal %s: one sourceRowId per line is required for lineage", sj.SourceJournalID)
	}
	for i, l := range sj.Lines {
		ref, ok := refs[l.AccountRef]
		if !ok {
			return JournalEntry{}, fmt.Errorf("%s: account reference %q has no COA mapping (MISSING_ACCOUNT)", l.SourceLineID, l.AccountRef)
		}
		j.Lines = append(j.Lines, JournalLine{
			LineID: fmt.Sprintf("%s/%s", j.JournalID, l.SourceLineID), Account: ref.Account, AccountingClass: ref.AccountingClass,
			Side: l.Side, Amount: l.Amount, Dimensions: l.Dimensions, SourceRowIDs: []string{sj.SourceRowIDs[i]},
			SourceLineID: l.SourceLineID, Explanation: "imported from " + sj.SourceSystem + " journal " + sj.SourceJournalID,
		})
	}
	bal, err := Balance(j)
	if err != nil {
		return JournalEntry{}, err
	}
	if bal != 0 {
		return JournalEntry{}, fmt.Errorf("source journal %s is not balanced (%d) (UNBALANCED)", sj.SourceJournalID, bal)
	}
	return j, nil
}

// Manifest is C2, the required-field manifest compiled from one configuration version.
type Manifest struct {
	SchemaVersion  string `json:"schemaVersion"`
	TenantID       string `json:"tenantId"`
	ConfigID       string `json:"configId"`
	ConfigVersion  int    `json:"configVersion"`
	ConfigDigest   string `json:"configDigest"`
	ManifestDigest string `json:"manifestDigest"`
	Events         []struct {
		EventType    string          `json:"eventType"`
		SourceSystem string          `json:"sourceSystem,omitempty"`
		Fields       []ManifestField `json:"fields"`
	} `json:"events"`
}

// ManifestField is one C2 field.
type ManifestField struct {
	FieldCode   string   `json:"fieldCode"`
	Definition  string   `json:"definition,omitempty"`
	Kind        string   `json:"kind"`
	DataType    string   `json:"dataType"`
	Cardinality string   `json:"cardinality"`
	ListCode    string   `json:"listCode,omitempty"`
	Mandatory   bool     `json:"mandatory"`
	KeyRole     string   `json:"keyRole,omitempty"`
	DerivedFrom []string `json:"derivedFrom,omitempty"`
	RequiredBy  []struct {
		Kind string `json:"kind"`
		Ref  string `json:"ref"`
	} `json:"requiredBy"`
}

// C2 enumerations, as the schema declares them.
var (
	ManifestKinds         = []string{"SOURCE", "DERIVED", "LEDGER_STATE"}
	ManifestDataTypes     = []string{"STRING", "DECIMAL", "INTEGER", "DATE", "TIMESTAMP", "BOOLEAN", "CODE"}
	ManifestCardinalities = []string{"ONE", "LIST"}
	ManifestKeyRoles      = []string{"BUSINESS_KEY", "LINK_KEY", "DEDUP_KEY", "NONE"}
	ManifestRequirements  = []string{"POSTING_RULE", "FORMULA", "FILTER", "KEY_PROFILE", "DATE_POLICY", "ACCOUNT_ASSIGNMENT", "LOOKUP", "EVENT_RECOGNITION", "RECONCILIATION"}
)

var digestRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func in(s string, set []string) bool {
	for _, x := range set {
		if x == s {
			return true
		}
	}
	return false
}

// ValidateManifest checks the C2 invariants: shapes and enumerations, a reason for every
// field, a list code exactly on LIST fields, DERIVED fields saying what they derive from and
// SOURCE fields not, and no field listed twice.
func ValidateManifest(m Manifest) []string {
	var p []string
	if m.SchemaVersion != "gl.manifest/v1" {
		p = append(p, "schemaVersion must be gl.manifest/v1")
	}
	if !codeRe.MatchString(m.TenantID) || !codeRe.MatchString(m.ConfigID) || m.ConfigVersion < 1 {
		p = append(p, "tenantId, configId and configVersion identify the configuration version")
	}
	if !digestRe.MatchString(m.ConfigDigest) || !digestRe.MatchString(m.ManifestDigest) {
		p = append(p, "configDigest and manifestDigest must be sha256 digests")
	}
	for _, e := range m.Events {
		seen := map[string]bool{}
		for _, f := range e.Fields {
			at := e.EventType + " " + f.ListCode + "." + f.FieldCode
			switch {
			case !codeRe.MatchString(f.FieldCode):
				p = append(p, at+": fieldCode must be a code")
			case seen[f.ListCode+"."+f.FieldCode]:
				p = append(p, at+": listed twice")
			case !in(f.Kind, ManifestKinds) || !in(f.DataType, ManifestDataTypes) || !in(f.Cardinality, ManifestCardinalities):
				p = append(p, at+": kind, dataType or cardinality outside C2")
			case f.KeyRole != "" && !in(f.KeyRole, ManifestKeyRoles):
				p = append(p, at+": keyRole outside C2")
			case (f.Cardinality == "LIST") != (f.ListCode != ""):
				p = append(p, at+": listCode is required exactly on LIST fields")
			case f.Kind == "DERIVED" && len(f.DerivedFrom) == 0:
				p = append(p, at+": a DERIVED field says what it is derived from")
			case f.Kind == "SOURCE" && len(f.DerivedFrom) > 0:
				p = append(p, at+": a SOURCE field is collected, not derived")
			case len(f.RequiredBy) == 0:
				p = append(p, at+": every field needs a reason (requiredBy)")
			}
			seen[f.ListCode+"."+f.FieldCode] = true
			for _, r := range f.RequiredBy {
				if !in(r.Kind, ManifestRequirements) || r.Ref == "" {
					p = append(p, at+": requiredBy "+r.Kind+" "+r.Ref+" is outside C2")
				}
			}
		}
	}
	return p
}
