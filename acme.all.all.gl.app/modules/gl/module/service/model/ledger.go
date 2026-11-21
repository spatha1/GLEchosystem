package model

// ALTB-04 — ledger state for the engine. A posting may read what is already posted: the open
// balance of a class for a business key (OPEN_BALANCE), for example the 0.50 left on an
// invoice that a write-off clears. The engine reads it through LedgerReader, so a lifecycle
// preview (a temporary, in-memory ledger) and a posting run (the client ledger) evaluate the
// same rules with the same code; only the ledger they read differs. A one-event preview has
// no ledger, and OPEN_BALANCE is then refused rather than guessed.

import (
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"

	"gl/formula"
)

// LedgerReader is the ledger state a posting reads.
type LedgerReader interface {
	// OpenBalance is the balance of the lines of class whose business key key has value, in
	// entity and currency, booked on or before asOf. It is signed so that the class's normal
	// side is positive: an invoice receivable with 0.50 left reads 0.50.
	OpenBalance(class, key, value, entity, currency string, asOf time.Time) (*big.Rat, error)
}

// ledgerLine is one posted line as a ledger keeps it.
type ledgerLine struct {
	step                                   int
	class, account, side, entity, currency string
	bookDate                               time.Time
	amount                                 *big.Rat
	keys                                   map[string]string
}

// TempLedger is an in-memory ledger for previews. Nothing in it is ever stored; it is thrown
// away with the preview.
type TempLedger struct {
	d     *Document
	lines []ledgerLine
}

// NewTempLedger is an empty temporary ledger for a configuration.
func NewTempLedger(d *Document) *TempLedger { return &TempLedger{d: d} }

// Post adds a balanced plan's lines, booked on its book date.
func (t *TempLedger) Post(step int, p *PostingPlan) error {
	day, ok := ParseDay(p.BookDate)
	if !ok {
		return &CodeError{"MISSING_BOOK_DATE", "the plan has no book date"}
	}
	for _, l := range p.Lines {
		r, err := formula.ParseDecimal(l.Amount)
		if err != nil {
			return err
		}
		t.lines = append(t.lines, ledgerLine{step: step, class: l.Class, account: l.Account, side: l.Side, entity: l.Entity, currency: l.Currency,
			bookDate: day, amount: r, keys: l.Keys})
	}
	return nil
}

// Load adds lines already posted (by a store), so later events read them. Step 0 marks them as
// posted before this run.
func (t *TempLedger) Load(lines []LedgerLine) error {
	for _, l := range lines {
		day, ok := ParseDay(l.BookDate)
		if !ok {
			return &CodeError{"MISSING_BOOK_DATE", "a posted line has no book date: " + l.LineID}
		}
		r, err := formula.ParseDecimal(l.Amount)
		if err != nil {
			return err
		}
		t.lines = append(t.lines, ledgerLine{class: l.Class, account: l.Account, side: l.Side, entity: l.Entity, currency: l.Currency,
			bookDate: day, amount: r, keys: l.Keys})
	}
	return nil
}

// OpenBalance implements LedgerReader.
func (t *TempLedger) OpenBalance(class, key, value, entity, currency string, asOf time.Time) (*big.Rat, error) {
	c, ok := ClassOf(t.d, class)
	if !ok {
		return nil, &formula.Error{Code: "UNKNOWN_CLASS", Message: fmt.Sprintf("OPEN_BALANCE: %s is not an accounting class", class)}
	}
	sum := new(big.Rat)
	for _, l := range t.lines {
		if l.class != class || l.keys[key] != value || l.entity != entity || l.currency != currency || l.bookDate.After(asOf) {
			continue
		}
		if l.side == "D" {
			sum.Add(sum, l.amount)
		} else {
			sum.Sub(sum, l.amount)
		}
	}
	if c.Side() == "C" {
		sum.Neg(sum)
	}
	return sum, nil
}

// LedgerBalance is one account's balance for one business key value, after a step.
type LedgerBalance struct {
	Entity   string `json:"entity"`
	Currency string `json:"currency"`
	Account  string `json:"account"`
	Class    string `json:"class"`
	Key      string `json:"key,omitempty"`   // the account's subledger key, if it has one
	Value    string `json:"value,omitempty"` // that key's value
	Debit    string `json:"debit"`
	Credit   string `json:"credit"`
	Balance  string `json:"balance"` // debit minus credit
}

// Balances is the ledger's balances by entity, currency, account and subledger key value.
func (t *TempLedger) Balances() []LedgerBalance {
	type k struct{ entity, currency, account, class, key, value string }
	sums := map[k][2]*big.Rat{}
	var order []k
	for _, l := range t.lines {
		key := ""
		for _, a := range t.d.Accounts {
			if a.Code == l.account {
				key = a.SubledgerKey
			}
		}
		x := k{l.entity, l.currency, l.account, l.class, key, l.keys[key]}
		if key == "" {
			x.value = ""
		}
		if _, ok := sums[x]; !ok {
			sums[x] = [2]*big.Rat{new(big.Rat), new(big.Rat)}
			order = append(order, x)
		}
		if l.side == "D" {
			sums[x][0].Add(sums[x][0], l.amount)
		} else {
			sums[x][1].Add(sums[x][1], l.amount)
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		return a.entity+a.currency+a.account+a.value < b.entity+b.currency+b.account+b.value
	})
	out := make([]LedgerBalance, 0, len(order))
	for _, x := range order {
		s := sums[x]
		bal := new(big.Rat).Sub(s[0], s[1])
		out = append(out, LedgerBalance{Entity: x.entity, Currency: x.currency, Account: x.account, Class: x.class, Key: x.key, Value: x.value,
			Debit: decimalOf(s[0], x.currency), Credit: decimalOf(s[1], x.currency), Balance: decimalOf(bal, x.currency)})
	}
	return out
}

func decimalOf(r *big.Rat, currency string) string {
	if s, err := formula.Exact(r, minorUnits(currency)); err == nil {
		return s
	}
	return formula.Minimal(r)
}

// ledgerCalls is the host-function table of one event: OPEN_BALANCE reads led for the event's
// own value of a business key, in the event's legal entity and currency, as of its book date.
func ledgerCalls(led LedgerReader, in EventInput, entity, asOf string) func(string, []formula.Value) (formula.Value, bool, error) {
	return func(name string, args []formula.Value) (formula.Value, bool, error) {
		if name != "OPEN_BALANCE" {
			return formula.Null, false, nil
		}
		if led == nil {
			return formula.Null, true, &formula.Error{Code: "LEDGER_STATE_UNAVAILABLE",
				Message: "OPEN_BALANCE reads posted lines: run this event in a lifecycle preview or a posting run, not on its own"}
		}
		if len(args) != 2 || args[0].Kind != formula.KString || args[1].Kind != formula.KString {
			return formula.Null, true, &formula.Error{Code: "TYPE_MISMATCH", Message: "OPEN_BALANCE takes a class and a business key, e.g. OPEN_BALANCE('AR1', 'INVOICE_NO')"}
		}
		class, key := args[0].Str, args[1].Str
		value := strings.TrimSpace(in.Keys[key])
		if value == "" {
			return formula.Null, true, &formula.Error{Code: "MISSING_KEY", Message: fmt.Sprintf("OPEN_BALANCE needs the event's %s", key)}
		}
		if entity == "" {
			return formula.Null, true, &formula.Error{Code: "MISSING_ENTITY", Message: "OPEN_BALANCE needs the event's legal entity (COMPANY)"}
		}
		day, ok := ParseDay(asOf)
		if !ok {
			return formula.Null, true, &formula.Error{Code: "MISSING_BOOK_DATE", Message: "OPEN_BALANCE needs a book date: give a transactionDate"}
		}
		bal, err := led.OpenBalance(class, key, value, entity, in.Currency, day)
		if err != nil {
			return formula.Null, true, err
		}
		return formula.Amount(bal, in.Currency), true, nil
	}
}

// OPEN_BALANCE is available to every event's formulas: derived measures and line amounts.
func ledgerSymbols(_ *Document, _ EventType, s *formula.Symbols) {
	if s.Funcs == nil {
		s.Funcs = map[string]formula.FuncSig{}
	}
	s.Funcs["OPEN_BALANCE"] = formula.FuncSig{Args: []formula.Kind{formula.KString, formula.KString}, Result: formula.KAmount}
}

var openBalanceRe = regexp.MustCompile(`OPEN_BALANCE\s*\(\s*'([^']*)'\s*,\s*'([^']*)'\s*\)`)

// validateLedgerReads checks every OPEN_BALANCE call written with literal arguments: the class
// must exist and the key must be a business key.
func validateLedgerReads(d *Document) []Finding {
	var fs []Finding
	keys := map[string]bool{}
	for _, k := range d.KeyProfiles {
		keys[k.Code] = true
	}
	check := func(path, src string) {
		for _, m := range openBalanceRe.FindAllStringSubmatch(src, -1) {
			if _, ok := ClassOf(d, m[1]); !ok {
				fs = append(fs, errf(path, "UNKNOWN_CLASS", "OPEN_BALANCE reads class %q, which is not an accounting class", m[1]))
			}
			if !keys[m[2]] {
				fs = append(fs, errf(path, "UNKNOWN_KEY", "OPEN_BALANCE reads business key %q, which is not a key profile", m[2]))
			}
		}
	}
	for i, dm := range d.DerivedMeasures {
		check(fmt.Sprintf("derivedMeasures/%d", i), dm.Formula)
	}
	for i, r := range d.PostingRules {
		for j, l := range r.Lines {
			check(fmt.Sprintf("postingRules/%d/lines/%d", i, j), l.Amount)
		}
	}
	return fs
}

func init() {
	symbolProviders = append(symbolProviders, ledgerSymbols)
	validators = append(validators, validateLedgerReads)
}
