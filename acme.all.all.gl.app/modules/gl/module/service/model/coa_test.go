package model

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// demoClasses: the semantic roles of the GL-TEST-001 demo.
func demoClasses() []any {
	class := func(code, name, nature string) map[string]any {
		return map[string]any{"code": code, "name": name, "nature": nature}
	}
	return []any{
		class("AR0", "Policy receivable", "ASSET"),
		class("AR1", "Invoice receivable", "ASSET"),
		class("PW0", "Premium written", "INCOME"),
		class("TX0", "Tax payable", "LIABILITY"),
		class("PR0", "Cash at bank", "ASSET"),
		class("IC0", "Intercompany due from", "ASSET"),
		class("IC1", "Intercompany due to", "LIABILITY"),
		class("WO", "Premium write-off", "EXPENSE"),
	}
}

// demoAccounts: client A's chart for the demo classes.
func demoAccounts() []any {
	acct := func(code, name, class string, extra map[string]any) map[string]any {
		a := map[string]any{"code": code, "name": name, "class": class, "activeFrom": "2025-07-01"}
		for k, v := range extra {
			a[k] = v
		}
		return a
	}
	return []any{
		acct("9948500", "Policy receivable", "AR0", map[string]any{"subledgerKey": "POLICY_NO"}),
		acct("9948523", "Policy receivable — motor", "AR0", map[string]any{"subledgerKey": "POLICY_NO", "entities": []any{"C01"}}),
		acct("9948600", "Invoice receivable", "AR1", map[string]any{"subledgerKey": "INVOICE_NO"}),
		acct("4100100", "Gross written premium", "PW0", nil),
		acct("2200100", "GST payable", "TX0", map[string]any{"currencies": []any{"AUD"}}),
		acct("1000900", "Operating bank C09", "PR0", map[string]any{"entities": []any{"C09"}, "dimensions": []any{"BANK_ACCOUNT"}}),
		acct("1500109", "Due from C09", "IC0", nil),
		acct("2500101", "Due to C01", "IC1", nil),
		acct("6100100", "Premium written off", "WO", map[string]any{"subledgerKey": "INVOICE_NO"}),
		acct("6100199", "Old write-off account", "WO", map[string]any{"activeTo": "2026-01-01"}),
	}
}

func coaCatalog(edit func(c map[string]any)) map[string]any {
	c := dimCatalog("C09")
	c["keyProfiles"] = keyProfiles()
	c["accountingClasses"] = demoClasses()
	c["accounts"] = demoAccounts()
	if edit != nil {
		edit(c)
	}
	return c
}

func coaDoc(t *testing.T, edit func(c map[string]any)) *Document {
	t.Helper()
	doc, fs := parse(t, coaCatalog(edit))
	if e := errorsOf(fs); len(e) > 0 {
		t.Fatalf("%+v", e)
	}
	return doc
}

func codes(as []Account) []string {
	out := []string{}
	for _, a := range as {
		out = append(out, a.Code)
	}
	return out
}

func codeOf(err error) string {
	var ce *CodeError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// AC: multiple clients can assign different codes to the same semantic class.
func TestClientsMapTheSameClassToTheirOwnCodes(t *testing.T) {
	a := coaDoc(t, nil)
	b := coaDoc(t, func(c map[string]any) {
		c["accounts"] = []any{
			map[string]any{"code": "1200-AR", "name": "Debtors", "class": "AR0", "activeFrom": "2020-01-01", "subledgerKey": "POLICY_NO"},
			map[string]any{"code": "1210-INV", "name": "Billed debtors", "class": "AR1", "activeFrom": "2020-01-01"},
		}
	})
	jan10 := day("2026-01-10")
	if got := codes(AccountsFor(a, AccountQuery{Class: "AR0", Day: jan10})); !reflect.DeepEqual(got, []string{"9948500", "9948523"}) {
		t.Errorf("client A's AR0: %v", got)
	}
	if got := codes(AccountsFor(b, AccountQuery{Class: "AR0", Day: jan10})); !reflect.DeepEqual(got, []string{"1200-AR"}) {
		t.Errorf("client B's AR0: %v", got)
	}
	// The class means the same thing in both: nature, side and statement come from the class.
	for _, d := range []*Document{a, b} {
		c, ok := ClassOf(d, "AR0")
		if !ok || c.Side() != "D" || c.Statement() != "BALANCE_SHEET" {
			t.Errorf("AR0 is a debit-normal balance-sheet asset: %+v", c)
		}
	}
	if pw, _ := ClassOf(a, "PW0"); pw.Side() != "C" || pw.Statement() != "PROFIT_AND_LOSS" {
		t.Errorf("PW0 is credit-normal income: %+v", pw)
	}
	// Client A's code means nothing to client B.
	line := PostingLine{Account: "9948500", Class: "AR0", BookDate: jan10, Entity: "C01", Currency: "AUD", Keys: map[string]string{"POLICY_NO": "P1"}}
	if _, err := CheckPosting(a, line); err != nil {
		t.Errorf("client A posts to 9948500: %v", err)
	}
	if _, err := CheckPosting(b, line); codeOf(err) != "MISSING_ACCOUNT" {
		t.Errorf("client B has no 9948500: %v", err)
	}
	// Lookup filters: entity, currency, ledger.
	if got := codes(AccountsFor(a, AccountQuery{Class: "AR0", Entity: "C09", Day: jan10})); !reflect.DeepEqual(got, []string{"9948500"}) {
		t.Errorf("9948523 is C01 only: %v", got)
	}
	if got := codes(AccountsFor(a, AccountQuery{Class: "TX0", Currency: "NZD", Day: jan10})); len(got) != 0 {
		t.Errorf("GST payable is AUD only: %v", got)
	}
	if got := codes(AccountsFor(a, AccountQuery{Ledger: "IFRS17"})); len(got) != 0 {
		t.Errorf("nothing is in an IFRS17 ledger: %v", got)
	}
}

// AC: inactive or missing account mappings block posting — each reason explicit.
func TestInactiveOrMissingAccountsBlockPosting(t *testing.T) {
	d := coaDoc(t, nil)
	ok := PostingLine{Account: "6100100", Class: "WO", BookDate: day("2026-01-15"), Entity: "C01", Currency: "AUD", Keys: map[string]string{"INVOICE_NO": "INV2"}}
	if a, err := CheckPosting(d, ok); err != nil || a.Name != "Premium written off" {
		t.Fatalf("E4 write-off posts: %+v %v", a, err)
	}
	with := func(f func(l *PostingLine)) PostingLine {
		l := ok
		l.Keys = map[string]string{"INVOICE_NO": "INV2"}
		f(&l)
		return l
	}
	cases := []struct {
		name, code, detail string
		line               PostingLine
	}{
		{"no mapping", "MISSING_ACCOUNT", "no account is mapped for class WO", with(func(l *PostingLine) { l.Account = "" })},
		{"unknown account", "MISSING_ACCOUNT", "account 6100999 is not in ledger PRIMARY", with(func(l *PostingLine) { l.Account = "6100999" })},
		{"other ledger", "MISSING_ACCOUNT", "not in ledger IFRS17", with(func(l *PostingLine) { l.Ledger = "IFRS17" })},
		{"closed account", "INACTIVE_ACCOUNT", "account 6100199 is active from 2025-07-01 until 2026-01-01; the line books on 2026-01-15", with(func(l *PostingLine) { l.Account = "6100199" })},
		{"before activation", "INACTIVE_ACCOUNT", "the line books on 2025-06-30", with(func(l *PostingLine) { l.BookDate = day("2025-06-30") })},
		{"wrong class", "CLASS_MISMATCH", "account 6100100 serves class WO, not AR1", with(func(l *PostingLine) { l.Class = "AR1" })},
		{"wrong entity", "ENTITY_NOT_ALLOWED", "account 1000900 accepts entities [C09]", with(func(l *PostingLine) {
			l.Account, l.Class, l.Dimensions = "1000900", "PR0", map[string]string{"BANK_ACCOUNT": "BANK-C09"}
		})},
		{"wrong currency", "CURRENCY_NOT_ALLOWED", "accepts currencies [AUD], not \"NZD\"", with(func(l *PostingLine) { l.Account, l.Class, l.Currency = "2200100", "TX0", "NZD" })},
		{"no subledger key", "MISSING_SUBLEDGER_KEY", "needs subledger key INVOICE_NO", with(func(l *PostingLine) { l.Keys = nil })},
		{"no required dimension", "MISSING_DIMENSION", "needs dimension BANK_ACCOUNT", with(func(l *PostingLine) { l.Account, l.Class, l.Entity = "1000900", "PR0", "C09" })},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := CheckPosting(d, c.line)
			if codeOf(err) != c.code || !strings.Contains(err.Error(), c.detail) {
				t.Errorf("got %v", err)
			}
		})
	}
	// The day an account closes is already closed: [activeFrom, activeTo).
	if _, err := CheckPosting(d, with(func(l *PostingLine) { l.Account, l.BookDate = "6100199", day("2025-12-31") })); err != nil {
		t.Errorf("the last active day posts: %v", err)
	}
	if _, err := CheckPosting(d, with(func(l *PostingLine) { l.Account, l.BookDate = "6100199", day("2026-01-01") })); codeOf(err) != "INACTIVE_ACCOUNT" {
		t.Errorf("activeTo is exclusive: %v", err)
	}
	// A class nobody mapped is flagged at validation time, before anything posts.
	_, fs := parse(t, coaCatalog(func(c map[string]any) {
		c["accountingClasses"] = append(c["accountingClasses"].([]any), map[string]any{"code": "AP0", "name": "Advance premium", "nature": "LIABILITY"})
	}))
	if !hasCode(fs, "CLASS_WITHOUT_ACCOUNT") {
		t.Errorf("an unmapped class is reported: %+v", fs)
	}
	if _, fs := parse(t, p1Catalog("POLICY")); !hasCode(fs, "MISSING_COA") {
		t.Errorf("no chart of accounts is reported: %+v", fs)
	}
}

// AC: account-level key requirements can be validated before data intake.
func TestAccountKeyRequirementsAreCheckedBeforeIntake(t *testing.T) {
	d := coaDoc(t, nil)
	if r := RequirementsOf(d, "AR1"); !reflect.DeepEqual(r.Keys, []string{"INVOICE_NO"}) || !reflect.DeepEqual(r.Accounts, []string{"9948600"}) {
		t.Errorf("AR1 requirements: %+v", r)
	}
	if r := RequirementsOf(d, "PR0"); !reflect.DeepEqual(r.Dimensions, []string{"BANK_ACCOUNT"}) || len(r.Keys) != 0 {
		t.Errorf("PR0 requirements: %+v", r)
	}
	cases := []struct {
		event, list, class string
		want               []string
	}{
		{"PAYMENT_RECEIVED", "ALLOCATION_LIST", "AR1", nil},                // each allocation carries its INVOICE_NO
		{"PAYMENT_RECEIVED", "", "AR1", []string{"MISSING_SUBLEDGER_KEY"}}, // the receipt itself has no invoice
		{"PAYMENT_RECEIVED", "", "PR0", nil},                               // the payment carries BANK_ACCOUNT
		{"PREM_ADDED", "", "PR0", []string{"MISSING_DIMENSION"}},           // a premium has no bank account
		{"PREM_ADDED", "", "AR0", nil},                                     // POLICY_NO is the premium's key
		{"PAYMENT_RECEIVED", "", "AR0", []string{"MISSING_SUBLEDGER_KEY"}}, // receipts are not keyed by policy here
		{"INVOICED", "INVOICE_LIST", "AR1", nil},                           // each invoice item carries INVOICE_NO
		{"PREM_ADDED", "", "AP0", []string{"UNKNOWN_CLASS"}},               // not a class
		{"PREM_ADDED", "ALLOCATION_LIST", "AR0", []string{"UNKNOWN_LIST"}}, // not this event's list
		{"WRITE_OFF", "", "WO", []string{"UNKNOWN_EVENT"}},                 // not in the catalog
	}
	for _, c := range cases {
		var got []string
		for _, f := range CheckCarrier(d, c.event, c.list, c.class) {
			got = append(got, f.Code)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s/%s → %s: got %v, want %v", c.event, c.list, c.class, got, c.want)
		}
	}
	// A dimension derived by lookup counts: BANKING_COMPANY comes from BANK_ACCOUNT.
	derived := coaDoc(t, func(c map[string]any) {
		c["accounts"] = append(c["accounts"].([]any), map[string]any{"code": "1000999", "name": "Cash clearing", "class": "PR0", "activeFrom": "2025-07-01", "dimensions": []any{"BANKING_COMPANY"}})
	})
	if fs := CheckCarrier(derived, "PAYMENT_RECEIVED", "", "PR0"); len(fs) != 0 {
		t.Errorf("a derivable dimension is carried: %+v", fs)
	}
}

func TestChartOfAccountsValidation(t *testing.T) {
	acct := func(c map[string]any, i int) map[string]any { return c["accounts"].([]any)[i].(map[string]any) }
	cases := map[string]func(c map[string]any){
		"INVALID_CODE":         func(c map[string]any) { c["accountingClasses"].([]any)[0].(map[string]any)["code"] = "ar0" },
		"DUPLICATE_CLASS":      func(c map[string]any) { c["accountingClasses"].([]any)[1].(map[string]any)["code"] = "AR0" },
		"INVALID_NATURE":       func(c map[string]any) { c["accountingClasses"].([]any)[0].(map[string]any)["nature"] = "RECEIVABLE" },
		"INVALID_SIDE":         func(c map[string]any) { c["accountingClasses"].([]any)[0].(map[string]any)["normalSide"] = "DR" },
		"MISSING_NAME":         func(c map[string]any) { acct(c, 0)["name"] = " " },
		"INVALID_ACCOUNT_CODE": func(c map[string]any) { acct(c, 0)["code"] = "99 48" },
		"DUPLICATE_ACCOUNT":    func(c map[string]any) { acct(c, 1)["code"] = "9948500" },
		"UNKNOWN_CLASS":        func(c map[string]any) { acct(c, 0)["class"] = "AR9" },
		"INVALID_DATE":         func(c map[string]any) { acct(c, 0)["activeFrom"] = "01/07/2025" },
		"INVALID_WINDOW":       func(c map[string]any) { acct(c, 0)["activeTo"] = "2025-07-01" },
		"INVALID_CURRENCY":     func(c map[string]any) { acct(c, 4)["currencies"] = []any{"aud"} },
		"UNKNOWN_CODE":         func(c map[string]any) { acct(c, 1)["entities"] = []any{"C77"} },
		"UNKNOWN_KEY":          func(c map[string]any) { acct(c, 2)["subledgerKey"] = "CLAIM_NO" },
		"UNKNOWN_DIMENSION":    func(c map[string]any) { acct(c, 5)["dimensions"] = []any{"COST_CENTRE"} },
	}
	for code, edit := range cases {
		_, fs := parse(t, coaCatalog(edit))
		if !hasCode(errorsOf(fs), code) {
			t.Errorf("%s: %+v", code, errorsOf(fs))
		}
	}
	// The same code may exist in another ledger (e.g. a statutory ledger next to the primary).
	_, fs := parse(t, coaCatalog(func(c map[string]any) { acct(c, 1)["code"], acct(c, 1)["ledger"] = "9948500", "STATUTORY" }))
	if e := errorsOf(fs); len(e) > 0 {
		t.Errorf("same code, other ledger: %+v", e)
	}
	// Entities need the COMPANY dimension to exist.
	_, fs = parse(t, coaCatalog(func(c map[string]any) {
		var kept []any
		for _, x := range c["dimensions"].([]any) {
			if x.(map[string]any)["code"] != "COMPANY" {
				kept = append(kept, x)
			}
		}
		c["dimensions"] = kept
		for _, e := range c["events"].([]any) {
			delete(e.(map[string]any), "dimensions")
		}
	}))
	if !hasCode(errorsOf(fs), "UNKNOWN_DIMENSION") {
		t.Errorf("entities without COMPANY: %+v", errorsOf(fs))
	}
	// Unknown fields are findings, never silently dropped.
	_, fs = parse(t, coaCatalog(func(c map[string]any) { acct(c, 0)["costCentre"] = "X" }))
	if len(errorsOf(fs)) == 0 {
		t.Error("an unknown account field must be reported")
	}
}
