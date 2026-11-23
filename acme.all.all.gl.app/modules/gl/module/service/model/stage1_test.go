package model

import (
	"strings"
	"testing"
)

func TestStandardClassLibraryIsWellFormedAndOnlyProposed(t *testing.T) {
	l := StandardClasses()
	if l.Status != "PROPOSED" || len(l.Classes) < 20 {
		t.Fatalf("library %s %s with %d classes", l.Version, l.Status, len(l.Classes))
	}
	seen := map[string]bool{}
	for _, c := range l.Classes {
		if !isCode(c.Code) || seen[c.Code] {
			t.Errorf("class code %q is not a unique upper-case code", c.Code)
		}
		seen[c.Code] = true
		if !oneOf(c.Nature, natures) || (c.NormalSide != "D" && c.NormalSide != "C") || c.Name == "" || c.Definition == "" || c.Group == "" {
			t.Errorf("class %s is incomplete: %+v", c.Code, c)
		}
		// nothing is approved until the product owner approves it
		if c.Status != "PROPOSED" {
			t.Errorf("class %s is %s: the library has not been approved", c.Code, c.Status)
		}
	}
	if _, ok := StandardClassOf("POLICY_RECEIVABLE"); !ok {
		t.Error("POLICY_RECEIVABLE must be in the library")
	}
	if _, ok := StandardClassOf("AR0"); ok {
		t.Error("the library never adopts a source system's class code")
	}
}

// stage1Content: a client chart imported from a source system: accounts without a class,
// source classes mapped explicitly, rules that carry their source row.
func stage1Content(edit func(c map[string]any)) map[string]any {
	c := map[string]any{
		"dimensions": []any{
			map[string]any{"code": "TRANSACTION_TYPE_CODE", "custom": true, "open": true},
			map[string]any{"code": "ACTIVITY_TYPE", "custom": true, "open": true},
		},
		"accountingClasses": []any{
			map[string]any{"code": "POLICY_RECEIVABLE", "name": "Policy receivable", "nature": "ASSET", "custom": true},
			map[string]any{"code": "PREMIUM_WRITTEN", "name": "Premium written", "nature": "INCOME", "custom": true},
			map[string]any{"code": "TAX_PAYABLE", "name": "Tax payable", "nature": "LIABILITY", "custom": true},
		},
		"accounts": []any{
			map[string]any{"code": "120010", "name": "Debtors control", "activeFrom": "2020-07-01", "type": "ASSET", "normalBalance": "D", "source": map[string]any{"file": "F1", "row": 1}},
			map[string]any{"code": "400000", "name": "Premium written", "activeFrom": "2020-07-01", "source": map[string]any{"file": "F1", "row": 2}},
			map[string]any{"code": "230015", "name": "GST", "activeFrom": "2020-07-01", "source": map[string]any{"file": "F1", "row": 3}},
		},
		"sourceFiles": []any{
			map[string]any{"id": "F1", "name": "coa.xlsx", "sheet": "COA", "kind": "COA", "rows": 3},
			map[string]any{"id": "F2", "name": "matrix.xlsx", "sheet": "Rules", "kind": "ASSIGNMENTS", "rows": 3, "conditions": []any{"TRANSACTION_TYPE_CODE"}},
		},
		"sourceClasses": []any{
			map[string]any{"system": "PAS", "code": "AR0", "meaning": "Open receivable", "mappings": []any{map[string]any{"class": "POLICY_RECEIVABLE"}}},
			map[string]any{"system": "PAS", "code": "PW0", "meaning": "Premium and tax source", "mappings": []any{
				map[string]any{"class": "PREMIUM_WRITTEN", "when": map[string]any{"TRANSACTION_TYPE_CODE": "PREM"}},
				map[string]any{"class": "TAX_PAYABLE", "when": map[string]any{"TRANSACTION_TYPE_CODE": "TAX"}},
			}},
		},
		"accountAssignmentRules": []any{
			map[string]any{"code": "AA_1", "class": "POLICY_RECEIVABLE", "account": "120010", "source": map[string]any{"file": "F2", "row": 1, "values": map[string]any{"AccountingClassCode": "AR0", "TransactionTypeCode": "*"}}},
			map[string]any{"code": "AA_2", "class": "PREMIUM_WRITTEN", "account": "400000", "match": map[string]any{"TRANSACTION_TYPE_CODE": "PREM"}, "source": map[string]any{"file": "F2", "row": 2}},
			map[string]any{"code": "AA_3", "class": "TAX_PAYABLE", "account": "230015", "match": map[string]any{"TRANSACTION_TYPE_CODE": "TAX"}, "source": map[string]any{"file": "F2", "row": 3}},
		},
	}
	if edit != nil {
		edit(c)
	}
	return c
}

func TestStage1ContentValidatesAndAccountsTakeTheirClassFromAssignmentRules(t *testing.T) {
	d := docOf(t, stage1Content(nil))
	// 120010 declares no class: it serves the class its rules give it, and nothing else
	if _, err := CheckPosting(d, PostingLine{Account: "120010", Class: "POLICY_RECEIVABLE", BookDate: day("2026-07-15")}); err != nil {
		t.Fatalf("posting the rule's class: %v", err)
	}
	_, err := CheckPosting(d, PostingLine{Account: "120010", Class: "TAX_PAYABLE", BookDate: day("2026-07-15")})
	if codeOf(err) != "CLASS_MISMATCH" {
		t.Fatalf("another class must be refused, got %v", err)
	}
	if got := codes(AccountsFor(d, AccountQuery{Class: "PREMIUM_WRITTEN"})); strings.Join(got, ",") != "400000" {
		t.Errorf("accounts for PREMIUM_WRITTEN: %v", got)
	}
	// the same source class gives different classes, and so accounts, under different conditions
	for _, tc := range []struct{ tt, class, account string }{{"PREM", "PREMIUM_WRITTEN", "400000"}, {"TAX", "TAX_PAYABLE", "230015"}} {
		class, _ := MapSourceClass(d, "PAS", "PW0", map[string]string{"TRANSACTION_TYPE_CODE": tc.tt})
		if class != tc.class {
			t.Fatalf("PW0 with %s maps to %q", tc.tt, class)
		}
		a, err := AssignAccount(d, ConfigRef{}, class, map[string]string{"TRANSACTION_TYPE_CODE": tc.tt}, day("2026-07-15"))
		if err != nil || a.Account != tc.account {
			t.Fatalf("%s → %s: %+v %v", tc.tt, class, a, err)
		}
	}
	if class, why := MapSourceClass(d, "PAS", "PW0", map[string]string{"TRANSACTION_TYPE_CODE": "FEE"}); class != "" || !strings.Contains(why, "no mapping") {
		t.Errorf("no mapping applies to FEE: %q %q", class, why)
	}
}

func TestStage1Findings(t *testing.T) {
	cases := map[string]struct {
		edit func(c map[string]any)
		code string
	}{
		"a proposed standard class cannot be validated": {func(c map[string]any) {
			c["accountingClasses"].([]any)[0] = map[string]any{"code": "POLICY_RECEIVABLE", "name": "Policy receivable", "nature": "ASSET", "standard": "POLICY_RECEIVABLE"}
		}, "STANDARD_CLASS_NOT_APPROVED"},
		"an unknown standard class": {func(c map[string]any) {
			c["accountingClasses"].([]any)[0] = map[string]any{"code": "X", "name": "X", "nature": "ASSET", "standard": "NOT_IN_LIBRARY"}
		}, "UNKNOWN_STANDARD_CLASS"},
		"a nature different from the standard": {func(c map[string]any) {
			c["accountingClasses"].([]any)[0] = map[string]any{"code": "POLICY_RECEIVABLE", "name": "Policy receivable", "nature": "LIABILITY", "standard": "POLICY_RECEIVABLE"}
		}, "STANDARD_CLASS_DIFFERS"},
		"a mapping to an unknown class": {func(c map[string]any) {
			c["sourceClasses"].([]any)[0].(map[string]any)["mappings"] = []any{map[string]any{"class": "NOPE"}}
		}, "UNKNOWN_CLASS"},
		"two unconditional mappings": {func(c map[string]any) {
			c["sourceClasses"].([]any)[0].(map[string]any)["mappings"] = []any{map[string]any{"class": "PREMIUM_WRITTEN"}, map[string]any{"class": "TAX_PAYABLE"}}
		}, "AMBIGUOUS_SOURCE_MAPPING"},
		"a source class listed twice": {func(c map[string]any) {
			c["sourceClasses"] = append(c["sourceClasses"].([]any), map[string]any{"system": "PAS", "code": "AR0"})
		}, "DUPLICATE_SOURCE_CLASS"},
		"a row of a file that was not imported": {func(c map[string]any) {
			c["accounts"].([]any)[0].(map[string]any)["source"] = map[string]any{"file": "F9", "row": 1}
		}, "UNKNOWN_SOURCE_FILE"},
		"a row beyond the file": {func(c map[string]any) {
			c["accounts"].([]any)[0].(map[string]any)["source"] = map[string]any{"file": "F1", "row": 7}
		}, "INVALID_SOURCE_ROW"},
		"an account serving an unknown class": {func(c map[string]any) {
			c["accounts"].([]any)[1].(map[string]any)["classes"] = []any{"NOPE"}
		}, "UNKNOWN_CLASS"},
		"a declared class other than the rule's": {func(c map[string]any) {
			c["accounts"].([]any)[0].(map[string]any)["class"] = "TAX_PAYABLE"
		}, "CLASS_MISMATCH"},
		"an invalid file kind": {func(c map[string]any) {
			c["sourceFiles"].([]any)[0].(map[string]any)["kind"] = "SPREADSHEET"
		}, "INVALID_KIND"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fs := validateMap(stage1Content(tc.edit))
			var errs []string
			for _, f := range fs {
				if f.Severity == "error" {
					errs = append(errs, f.Code)
				}
			}
			if !hasCode(errorsOf(fs), tc.code) {
				t.Fatalf("want error %s, got %v", tc.code, errs)
			}
		})
	}
	// an unmapped source class is a warning: it can be drafted
	fs := validateMap(stage1Content(func(c map[string]any) {
		c["sourceClasses"] = append(c["sourceClasses"].([]any), map[string]any{"system": "PAS", "code": "BF2", "meaning": "Fee closed receivable"})
	}))
	if hasCode(errorsOf(fs), "SOURCE_CLASS_UNMAPPED") || !hasCode(fs, "SOURCE_CLASS_UNMAPPED") {
		t.Errorf("an unmapped source class must be a warning: %+v", fs)
	}
}
