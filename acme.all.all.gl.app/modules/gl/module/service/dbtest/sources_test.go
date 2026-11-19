package dbtest

import (
	"strings"
	"testing"
)

func catalog(paymentAuthorities ...map[string]any) map[string]any {
	auth := []any{
		map[string]any{"eventType": "PREM_ADDED", "measure": "*", "sourceSystem": "POLICY", "intakeMode": "RAW_EVENT"},
	}
	for _, a := range paymentAuthorities {
		auth = append(auth, a)
	}
	return map[string]any{
		"sourceSystems": []any{map[string]any{"code": "POLICY", "kind": "POLICY"}, map[string]any{"code": "BILLING", "kind": "BILLING"}},
		"events": []any{
			map[string]any{"code": "PREM_ADDED", "economicKey": []any{"POLICY_NO", "TERM_NO"}, "measures": []any{"PREMIUM", "TAX"}},
			map[string]any{"code": "PAYMENT_RECEIVED", "economicKey": []any{"RECEIPT_NO"}, "measures": []any{"PAY_AMT"}},
		},
		"sourceAuthority": auth,
	}
}

// US-005 through the store: validation (the gate to approval) applies the source-authority
// rules, so an ambiguous or incomplete configuration can never be approved.
func TestValidationAppliesSourceAuthorityRules(t *testing.T) {
	needDB(t)
	cases := []struct {
		name    string
		content map[string]any
		code    string // "" = must validate
	}{
		{"billing for payments", catalog(map[string]any{"eventType": "PAYMENT_RECEIVED", "measure": "*", "sourceSystem": "BILLING", "intakeMode": "RAW_EVENT"}), ""},
		{"two open authorities", catalog(
			map[string]any{"eventType": "PAYMENT_RECEIVED", "measure": "*", "sourceSystem": "BILLING", "intakeMode": "RAW_EVENT"},
			map[string]any{"eventType": "PAYMENT_RECEIVED", "measure": "*", "sourceSystem": "POLICY", "intakeMode": "RAW_EVENT"}), "AMBIGUOUS_SOURCE_AUTHORITY"},
		{"no payment authority", catalog(), "MISSING_SOURCE_AUTHORITY"},
		{"explicit switch on a date", catalog(
			map[string]any{"eventType": "PAYMENT_RECEIVED", "measure": "*", "sourceSystem": "BILLING", "intakeMode": "RAW_EVENT", "effectiveTo": "2026-07-01"},
			map[string]any{"eventType": "PAYMENT_RECEIVED", "measure": "*", "sourceSystem": "POLICY", "intakeMode": "RAW_EVENT", "effectiveFrom": "2026-07-01"}), ""},
	}
	for _, k := range cases {
		id := uniq("SRC")
		mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "content": k.content, "reason": k.name}), 201)
		v := do(t, "POST", "/configs/"+id+"/versions/1/validate", tenantA, ba, nil)
		if k.code == "" {
			if v.Status != 200 {
				t.Errorf("%s: should validate, got %s", k.name, v.Raw)
			}
			continue
		}
		if v.Status != 422 || !strings.Contains(v.Raw, `"code":"`+k.code+`"`) {
			t.Errorf("%s: want 422 with %s, got %s", k.name, k.code, v.Raw)
		}
		if a := do(t, "POST", "/configs/"+id+"/versions/1/approve", tenantA, rev, reason("try anyway")); a.code() != "INVALID_STATE" {
			t.Errorf("%s: an invalid configuration reached approval: %s", k.name, a.Raw)
		}
	}
}
