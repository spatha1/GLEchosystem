package model

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// US-011 — chart of accounts and accounting classes.
//
// An accounting class is a semantic role: AR0 policy receivable, AR1 invoice receivable, PW0
// premium written, TX0 tax payable, PR0 cash, IC0/IC1 intercompany due from/to, WO write-off.
// Posting rules (US-012) speak in classes; each client maps classes to its own ledger account
// codes, so one rule set serves clients with different charts. An account is active over
// [activeFrom, activeTo), may be limited to legal entities and currencies, and may require a
// subledger key (a US-010 business key) and dimensions on every line. Posting to a missing,
// inactive or unsuitable account is refused, never redirected.

// AccountingClass is a semantic accounting role.
type AccountingClass struct {
	Code        string `json:"code"`
	Name        string `json:"name"`
	Nature      string `json:"nature"`               // ASSET | LIABILITY | EQUITY | INCOME | EXPENSE
	NormalSide  string `json:"normalSide,omitempty"` // D | C; default from the nature
	Description string `json:"description,omitempty"`
}

// Account is one client ledger account.
type Account struct {
	Code         string   `json:"code"` // the client's ledger code, e.g. 9948523
	Name         string   `json:"name"`
	Class        string   `json:"class"`
	Ledger       string   `json:"ledger,omitempty"`     // the ledger it belongs to; default PRIMARY
	Entities     []string `json:"entities,omitempty"`   // legal entities (COMPANY values) that may post; empty: all
	Currencies   []string `json:"currencies,omitempty"` // transaction currencies it accepts; empty: all
	ActiveFrom   string   `json:"activeFrom"`
	ActiveTo     string   `json:"activeTo,omitempty"`     // exclusive
	SubledgerKey string   `json:"subledgerKey,omitempty"` // business key every line must carry
	Dimensions   []string `json:"dimensions,omitempty"`   // dimensions every line must carry
	Description  string   `json:"description,omitempty"`
}

const defaultLedger = "PRIMARY"

var (
	natures       = []string{"ASSET", "LIABILITY", "EQUITY", "INCOME", "EXPENSE"}
	accountCodeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,39}$`)
)

// LedgerOf is the account's ledger.
func (a Account) LedgerOf() string {
	if a.Ledger == "" {
		return defaultLedger
	}
	return a.Ledger
}

// ActiveOn says whether the account accepts postings booked on day.
func (a Account) ActiveOn(day time.Time) bool {
	from, ok := ParseDay(a.ActiveFrom)
	if !ok || day.Before(from) {
		return false
	}
	if a.ActiveTo != "" {
		if to, ok := ParseDay(a.ActiveTo); !ok || !day.Before(to) {
			return false
		}
	}
	return true
}

// Side is the class's normal balance side.
func (c AccountingClass) Side() string {
	if c.NormalSide != "" {
		return c.NormalSide
	}
	switch c.Nature {
	case "ASSET", "EXPENSE":
		return "D"
	}
	return "C"
}

// Statement is where the class reports: BALANCE_SHEET or PROFIT_AND_LOSS.
func (c AccountingClass) Statement() string {
	if c.Nature == "INCOME" || c.Nature == "EXPENSE" {
		return "PROFIT_AND_LOSS"
	}
	return "BALANCE_SHEET"
}

func init() { validators = append(validators, validateCOA) }

func validateCOA(d *Document) []Finding {
	var fs []Finding
	if len(d.AccountingClasses) == 0 && len(d.Accounts) == 0 {
		if len(d.Events) > 0 {
			fs = append(fs, warnf("accounts", "MISSING_COA", "no chart of accounts yet: every posting is blocked until classes and accounts are configured"))
		}
		return fs
	}
	classes := map[string]bool{}
	for i, c := range d.AccountingClasses {
		p := fmt.Sprintf("accountingClasses/%d", i)
		if !isCode(c.Code) {
			fs = append(fs, errf(p, "INVALID_CODE", "class code %q must be an upper-case code", c.Code))
		}
		if classes[c.Code] {
			fs = append(fs, errf(p, "DUPLICATE_CLASS", "class %s is declared twice", c.Code))
		}
		classes[c.Code] = true
		if strings.TrimSpace(c.Name) == "" {
			fs = append(fs, errf(p, "MISSING_NAME", "class %s needs a name", c.Code))
		}
		if !oneOf(c.Nature, natures) {
			fs = append(fs, errf(p, "INVALID_NATURE", "class %s nature %q must be one of %v", c.Code, c.Nature, natures))
		}
		if c.NormalSide != "" && c.NormalSide != "D" && c.NormalSide != "C" {
			fs = append(fs, errf(p, "INVALID_SIDE", "class %s normalSide %q must be D or C", c.Code, c.NormalSide))
		}
	}
	idx := dims(d)
	keys := map[string]bool{}
	for _, k := range d.KeyProfiles {
		keys[k.Code] = true
	}
	company, companyOK := idx["COMPANY"]
	seen := map[string]bool{}
	used := map[string]bool{}
	for i, a := range d.Accounts {
		p := fmt.Sprintf("accounts/%d", i)
		if !accountCodeRe.MatchString(a.Code) {
			fs = append(fs, errf(p, "INVALID_ACCOUNT_CODE", "account code %q: letters, digits, '.', '_' or '-', at most 40", a.Code))
		}
		if a.Ledger != "" && !isCode(a.Ledger) {
			fs = append(fs, errf(p, "INVALID_CODE", "ledger %q must be an upper-case code", a.Ledger))
		}
		id := a.LedgerOf() + "/" + a.Code
		if seen[id] {
			fs = append(fs, errf(p, "DUPLICATE_ACCOUNT", "account %s is declared twice in ledger %s", a.Code, a.LedgerOf()))
		}
		seen[id] = true
		if strings.TrimSpace(a.Name) == "" {
			fs = append(fs, errf(p, "MISSING_NAME", "account %s needs a name", a.Code))
		}
		if !classes[a.Class] {
			fs = append(fs, errf(p, "UNKNOWN_CLASS", "account %s is mapped to class %q, which is not an accounting class", a.Code, a.Class))
		}
		used[a.Class] = true
		from, fromOK := ParseDay(a.ActiveFrom)
		if !fromOK {
			fs = append(fs, errf(p, "INVALID_DATE", "account %s activeFrom %q must be a date (YYYY-MM-DD)", a.Code, a.ActiveFrom))
		}
		if a.ActiveTo != "" {
			to, ok := ParseDay(a.ActiveTo)
			if !ok {
				fs = append(fs, errf(p, "INVALID_DATE", "account %s activeTo %q must be a date (YYYY-MM-DD)", a.Code, a.ActiveTo))
			} else if fromOK && !to.After(from) {
				fs = append(fs, errf(p, "INVALID_WINDOW", "account %s is active from %s to %s: the end must be after the start", a.Code, a.ActiveFrom, a.ActiveTo))
			}
		}
		for _, c := range a.Currencies {
			if !currencyRe(c) {
				fs = append(fs, errf(p, "INVALID_CURRENCY", "account %s currency %q must be an ISO 4217 code", a.Code, c))
			}
		}
		if len(a.Entities) > 0 && !companyOK {
			fs = append(fs, errf(p, "UNKNOWN_DIMENSION", "account %s is limited to entities, but the COMPANY dimension is not configured", a.Code))
		}
		for _, e := range a.Entities {
			if companyOK && !idx.open("COMPANY") && !oneOf(e, company.Values) {
				fs = append(fs, errf(p, "UNKNOWN_CODE", "account %s entity %q is not a COMPANY value", a.Code, e))
			}
		}
		if a.SubledgerKey != "" && !keys[a.SubledgerKey] {
			fs = append(fs, errf(p, "UNKNOWN_KEY", "account %s requires subledger key %s, which is not a key profile", a.Code, a.SubledgerKey))
		}
		for _, x := range a.Dimensions {
			if _, ok := idx[x]; !ok {
				fs = append(fs, errf(p, "UNKNOWN_DIMENSION", "account %s requires dimension %s, which is not configured", a.Code, x))
			}
		}
	}
	for i, c := range d.AccountingClasses {
		if !used[c.Code] {
			fs = append(fs, warnf(fmt.Sprintf("accountingClasses/%d", i), "CLASS_WITHOUT_ACCOUNT", "class %s has no account: posting to it is blocked", c.Code))
		}
	}
	return fs
}

// ClassOf finds an accounting class.
func ClassOf(d *Document, code string) (AccountingClass, bool) {
	for _, c := range d.AccountingClasses {
		if c.Code == code {
			return c, true
		}
	}
	return AccountingClass{}, false
}

// AccountQuery filters the chart of accounts; empty fields and a zero Day match everything.
type AccountQuery struct {
	Class, Ledger, Entity, Currency string
	Day                             time.Time
}

// AccountsFor is the COA lookup: the accounts a line could use, in configuration order.
func AccountsFor(d *Document, q AccountQuery) []Account {
	var out []Account
	for _, a := range d.Accounts {
		switch {
		case q.Class != "" && a.Class != q.Class,
			q.Ledger != "" && a.LedgerOf() != q.Ledger,
			q.Entity != "" && len(a.Entities) > 0 && !oneOf(q.Entity, a.Entities),
			q.Currency != "" && len(a.Currencies) > 0 && !oneOf(q.Currency, a.Currencies),
			!q.Day.IsZero() && !a.ActiveOn(q.Day):
			continue
		}
		out = append(out, a)
	}
	return out
}

// PostingLine is what a journal line offers its account.
type PostingLine struct {
	Account    string
	Ledger     string
	Class      string
	BookDate   time.Time
	Entity     string
	Currency   string
	Keys       map[string]string
	Dimensions map[string]string
}

// CheckPosting is the posting gate: the line may post to the account only if the account
// exists in the ledger, serves the line's class, is active on the book date, accepts the
// entity and currency, and the line carries the account's subledger key and dimensions.
func CheckPosting(d *Document, l PostingLine) (Account, error) {
	ledger := l.Ledger
	if ledger == "" {
		ledger = defaultLedger
	}
	if l.Account == "" {
		return Account{}, &CodeError{"MISSING_ACCOUNT", fmt.Sprintf("no account is mapped for class %s", l.Class)}
	}
	var acct *Account
	for i := range d.Accounts {
		if d.Accounts[i].Code == l.Account && d.Accounts[i].LedgerOf() == ledger {
			acct = &d.Accounts[i]
		}
	}
	if acct == nil {
		return Account{}, &CodeError{"MISSING_ACCOUNT", fmt.Sprintf("account %s is not in ledger %s of this chart of accounts", l.Account, ledger)}
	}
	a := *acct
	day := l.BookDate.Format("2006-01-02")
	switch {
	case a.Class != l.Class:
		return a, &CodeError{"CLASS_MISMATCH", fmt.Sprintf("account %s serves class %s, not %s", a.Code, a.Class, l.Class)}
	case !a.ActiveOn(l.BookDate):
		window := "from " + a.ActiveFrom
		if a.ActiveTo != "" {
			window += " until " + a.ActiveTo
		}
		return a, &CodeError{"INACTIVE_ACCOUNT", fmt.Sprintf("account %s is active %s; the line books on %s", a.Code, window, day)}
	case len(a.Entities) > 0 && !oneOf(l.Entity, a.Entities):
		return a, &CodeError{"ENTITY_NOT_ALLOWED", fmt.Sprintf("account %s accepts entities %v, not %q", a.Code, a.Entities, l.Entity)}
	case len(a.Currencies) > 0 && !oneOf(l.Currency, a.Currencies):
		return a, &CodeError{"CURRENCY_NOT_ALLOWED", fmt.Sprintf("account %s accepts currencies %v, not %q", a.Code, a.Currencies, l.Currency)}
	case a.SubledgerKey != "" && strings.TrimSpace(l.Keys[a.SubledgerKey]) == "":
		return a, &CodeError{"MISSING_SUBLEDGER_KEY", fmt.Sprintf("account %s needs subledger key %s on every line", a.Code, a.SubledgerKey)}
	}
	for _, x := range a.Dimensions {
		if strings.TrimSpace(l.Dimensions[x]) == "" {
			return a, &CodeError{"MISSING_DIMENSION", fmt.Sprintf("account %s needs dimension %s on every line", a.Code, x)}
		}
	}
	return a, nil
}

// ClassRequirement is what any line of a class may be asked to carry: the union over the
// class's accounts, because which account a line resolves to (US-013) is known only later.
type ClassRequirement struct {
	Class      string   `json:"class"`
	Accounts   []string `json:"accounts"`
	Keys       []string `json:"keys"`
	Dimensions []string `json:"dimensions"`
}

// RequirementsOf collects a class's account-level requirements.
func RequirementsOf(d *Document, class string) ClassRequirement {
	r := ClassRequirement{Class: class, Accounts: []string{}, Keys: []string{}, Dimensions: []string{}}
	keys, dimset := map[string]bool{}, map[string]bool{}
	for _, a := range d.Accounts {
		if a.Class != class {
			continue
		}
		r.Accounts = append(r.Accounts, a.Code)
		if a.SubledgerKey != "" {
			keys[a.SubledgerKey] = true
		}
		for _, x := range a.Dimensions {
			dimset[x] = true
		}
	}
	for k := range keys {
		r.Keys = append(r.Keys, k)
	}
	for x := range dimset {
		r.Dimensions = append(r.Dimensions, x)
	}
	sort.Strings(r.Keys)
	sort.Strings(r.Dimensions)
	return r
}

// CheckCarrier validates before any data arrives that an event can feed lines of a class:
// the event (and, for lines expanded per item, the list's items) must carry every subledger
// key and dimension the class's accounts require. Posting rules (US-012) run it for each line
// template.
func CheckCarrier(d *Document, eventType, list, class string) []Finding {
	path := "events/" + eventType
	var ev *EventType
	for i := range d.Events {
		if d.Events[i].Code == eventType {
			ev = &d.Events[i]
		}
	}
	if ev == nil {
		return []Finding{errf(path, "UNKNOWN_EVENT", "%s is not in the event catalog", eventType)}
	}
	if _, ok := ClassOf(d, class); !ok {
		return []Finding{errf(path, "UNKNOWN_CLASS", "%s is not an accounting class", class)}
	}
	keys := append([]string{}, eventKeys(*ev)...)
	where := "event " + eventType
	if list != "" {
		found := false
		for _, l := range ev.Lists {
			if l.Code == list {
				keys = append(keys, itemKeys(l)...)
				found = true
			}
		}
		if !found {
			return []Finding{errf(path, "UNKNOWN_LIST", "event %s has no list %s", eventType, list)}
		}
		where += " list " + list
	}
	r := RequirementsOf(d, class)
	var fs []Finding
	if len(r.Accounts) == 0 {
		fs = append(fs, errf(path, "MISSING_ACCOUNT", "class %s has no account: lines of it cannot post", class))
	}
	for _, k := range r.Keys {
		if !oneOf(k, keys) {
			fs = append(fs, errf(path, "MISSING_SUBLEDGER_KEY", "class %s accounts need subledger key %s, which %s does not carry", class, k, where))
		}
	}
	dimsHere := eventDimensions(d, *ev)
	for _, x := range r.Dimensions {
		if !oneOf(x, dimsHere) {
			fs = append(fs, errf(path, "MISSING_DIMENSION", "class %s accounts need dimension %s, which %s does not carry or derive", class, x, where))
		}
	}
	return fs
}
