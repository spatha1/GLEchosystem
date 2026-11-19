package dbtest

import (
	"encoding/json"
	"strings"
	"testing"
)

// US-009 through the store: the simulator returns the period decision — book date and
// effective date kept apart, the booking class, and the steps that explain it.
func TestSimulatorReturnsThePeriodDecision(t *testing.T) {
	needDB(t)
	c := catalog(map[string]any{"eventType": "PAYMENT_RECEIVED", "measure": "*", "sourceSystem": "BILLING", "intakeMode": "RAW_EVENT"})
	c["calendars"] = []any{map[string]any{"code": "AU_SYD", "timezone": "Australia/Sydney", "weekend": []any{"SAT", "SUN"}, "holidays": []any{"2026-01-26"}}}
	c["datePolicies"] = []any{map[string]any{"code": "DEFAULT", "calendar": "AU_SYD", "bookDate": "TRANSACTION_DATE", "advance": true, "closeAfterBusinessDay": 3}}
	c["derivedMeasures"] = []any{map[string]any{"code": "ADVANCE_PREMIUM", "formula": "IF(BOOKING_CLASS == 'A', PREMIUM, 0)", "resultType": "AMOUNT", "eventTypes": []any{"PREM_ADDED"}}}
	id := uniq("DATES")
	mustStatus(t, do(t, "POST", "/configs", tenantA, ba, map[string]any{"configId": id, "effectiveFrom": "2026-01-01", "content": c, "reason": "date policy"}), 201)
	mustStatus(t, do(t, "POST", "/configs/"+id+"/versions/1/validate", tenantA, ba, nil), 200)

	e5 := map[string]any{"eventType": "PREM_ADDED", "currency": "AUD", "transactionDate": "2026-01-20", "effectiveDate": "2026-02-01",
		"measures": map[string]string{"PREMIUM": "200.00", "TAX": "0.00"}}
	r := do(t, "POST", "/configs/"+id+"/versions/1/evaluate", tenantA, viewer, e5)
	mustStatus(t, r, 200)
	var body struct {
		Period struct {
			BookDate, EffectiveDate, AccountingPeriod, BookingClass string
			Steps                                                   []string
		} `json:"period"`
	}
	if err := json.Unmarshal([]byte(r.Raw), &body); err != nil {
		t.Fatal(err)
	}
	p := body.Period
	if p.BookDate != "2026-01-20" || p.EffectiveDate != "2026-02-01" || p.AccountingPeriod != "2026-01" || p.BookingClass != "A" || len(p.Steps) < 2 {
		t.Errorf("period decision: %+v", p)
	}
	if !strings.Contains(r.Raw, `"value":"200 AUD"`) {
		t.Errorf("ADVANCE_PREMIUM: %s", r.Raw)
	}
	bad := map[string]any{"eventType": "PREM_ADDED", "currency": "AUD", "transactionDate": "2026-02-30", "effectiveDate": "2026-02-01", "measures": map[string]string{"PREMIUM": "1"}}
	if b := do(t, "POST", "/configs/"+id+"/versions/1/evaluate", tenantA, viewer, bad); b.Status != 422 || b.code() != "INVALID_INPUT" {
		t.Errorf("impossible date: %s", b.Raw)
	}
}
