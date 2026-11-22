package model

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// withOutputs adds a 60-character layout and three profiles over GL-TEST-001.
func withOutputs(c map[string]any) map[string]any {
	c["outputLayouts"] = []any{map[string]any{"code": "L60", "recordLength": 60, "fields": []any{
		map[string]any{"name": "ACCOUNT", "start": 1, "length": 8, "source": "ACCOUNT"},
		map[string]any{"name": "PERIOD", "start": 9, "length": 7, "source": "PERIOD"},
		map[string]any{"name": "DATE", "start": 16, "length": 8, "source": "DATE", "format": "yyyyMMdd"},
		map[string]any{"name": "AMOUNT", "start": 24, "length": 12, "source": "AMOUNT", "format": "IMPLIED", "decimals": 3, "align": "RIGHT", "pad": "0"},
		map[string]any{"name": "SIDE", "start": 36, "length": 1, "source": "SIDE"},
		map[string]any{"name": "LOB", "start": 37, "length": 5, "source": "DIMENSION", "refs": []any{"LOB"}, "lengths": []any{4, 5}},
		map[string]any{"name": "DESC", "start": 42, "length": 15, "source": "CONSTANT", "value": "GL EXPORT"},
		map[string]any{"name": "TYPE", "start": 57, "length": 2, "source": "JOURNAL_TYPE", "value": "GJ"},
	}}}
	c["outputProfiles"] = []any{
		map[string]any{"code": "SUMMARY_C01", "format": "FIXED_WIDTH", "layout": "L60", "fileName": "GL-{TO:yyMMdd}.{PROFILE}.trf", "entities": []any{"C01"},
			"sort": []any{"ACCOUNT", "PERIOD", "DATE"}, "sections": []any{
				map[string]any{"group": "SUMMARY", "groupBy": []any{"ACCOUNT", "PERIOD", "DATE", "DIM:LOB"}}}},
		map[string]any{"code": "CASH_DETAIL", "format": "FIXED_WIDTH", "layout": "L60",
			"fieldOverrides": []any{map[string]any{"name": "DESC", "source": "KEY", "refs": []any{"RECEIPT_NO", "POLICY_NO"}, "indent": 2, "lengths": []any{2}}},
			"sections": []any{map[string]any{"accounts": []any{"1000900"}, "group": "LINE", "dailyOffset": "MONTH_END",
				"offsetOverrides": []any{map[string]any{"name": "DESC", "source": "CONSTANT", "value": "Day reversal"}}}}},
		map[string]any{"code": "CSV_ALL", "format": "DELIMITED", "layout": "L60", "header": true, "delimiter": ",",
			"fieldOverrides": []any{map[string]any{"name": "DESC", "source": "CONSTANT", "value": "say \"hi\", ok"}},
			"sections":       []any{map[string]any{"group": "NET", "groupBy": []any{"ENTITY", "ACCOUNT"}}}},
		map[string]any{"code": "JSON_ALL", "format": "JSON_LINES", "layout": "L60", "sections": []any{map[string]any{"group": "LINE"}}},
	}
	return c
}

func outputLines(t *testing.T, d *Document, codes ...string) []OutputLine {
	t.Helper()
	var lines []LedgerLine
	var js []JournalEntry
	for _, e := range scenarioEvents(t, d, codes...) {
		led := NewTempLedger(d)
		p, err := PreviewPostingWith(d, fixtureRef, e.Input, led)
		if err != nil {
			t.Fatal(err)
		}
		j, l, err := Journals(d, "CLIENT_A", e.Input, p)
		if err != nil {
			t.Fatal(err)
		}
		js, lines = append(js, j...), append(lines, l...)
	}
	ol, err := OutputLinesOf(d, lines, js)
	if err != nil {
		t.Fatal(err)
	}
	return ol
}

func oday(s string) time.Time { t, _ := ParseDay(s); return t }

// ALTB-10: a fixed-width file: summed per account, period, date, side and grouped analysis,
// every field at its position, sorted, balanced.
func TestFixedWidthSummaryFile(t *testing.T) {
	d := docOf(t, withOutputs(fixtureMap(t)))
	ol := outputLines(t, d, "E1", "E2", "E3")
	f, err := ProduceFile(d, "SUMMARY_C01", ol, oday("2026-01-01"), oday("2026-01-31"))
	if err != nil {
		t.Fatal(err)
	}
	recs := strings.Split(strings.TrimSuffix(f.Content, "\n"), "\n")
	if f.Name != "GL-260131.SUMMARY_C01.trf" || f.Records != len(recs) || !f.Balanced || f.Debit != f.Credit {
		t.Fatalf("file %s: %d records, balanced %v, %s / %s, checks %v", f.Name, f.Records, f.Balanced, f.Debit, f.Credit, f.Checks)
	}
	for _, r := range recs {
		if len(r) != 60 {
			t.Fatalf("record length %d: %q", len(r), r)
		}
	}
	// E1 premium 1,000.00 credit on 4100100, January 2026 = fiscal 2026 period 007 (year from July).
	want := "4100100 202600720260101000001000000CMOTORGL EXPORT      GJ  "
	found := false
	for _, r := range recs {
		found = found || r == want
	}
	if !found {
		t.Errorf("premium record:\nwant %q\nhave %q", want, recs)
	}
	for i := 1; i < len(recs); i++ {
		if recs[i][:23] < recs[i-1][:23] {
			t.Errorf("not sorted by account, period, date: %q after %q", recs[i][:23], recs[i-1][:23])
		}
	}
	// Only C01 lines: the C09 cash and due-to legs of E3 are not in this file.
	if strings.Contains(f.Content, "1000900") || strings.Contains(f.Content, "2500101") {
		t.Errorf("another entity's lines are in the file")
	}
}

// ALTB-10: one record per line with a key as the description (written only when it has an
// allowed length), plus one opposite record per account and day dated at the month end.
func TestDetailFileWithDailyOffset(t *testing.T) {
	d := docOf(t, withOutputs(fixtureMap(t)))
	f, err := ProduceFile(d, "CASH_DETAIL", outputLines(t, d, "E1", "E2", "E3"), oday("2026-01-10"), oday("2026-01-10"))
	if err != nil {
		t.Fatal(err)
	}
	recs := strings.Split(strings.TrimSuffix(f.Content, "\n"), "\n")
	if len(recs) != 2 || !f.Balanced {
		t.Fatalf("detail: %q balanced %v", recs, f.Balanced)
	}
	if !strings.HasPrefix(recs[0][15:], "20260110000001099500D") || recs[0][41:45] != "  R1" {
		t.Errorf("cash line: %q", recs[0])
	}
	if !strings.HasPrefix(recs[1][15:], "20260131000001099500C") || !strings.HasPrefix(recs[1][41:], "Day reversal") {
		t.Errorf("month-end offset: %q", recs[1])
	}
}

// ALTB-10: the same lines as a CSV with a header (values quoted when needed) and as JSON lines.
func TestDelimitedAndJSONFiles(t *testing.T) {
	d := docOf(t, withOutputs(fixtureMap(t)))
	ol := outputLines(t, d, "E1", "E2", "E3")
	csv, err := ProduceFile(d, "CSV_ALL", ol, oday("2026-01-01"), oday("2026-01-31"))
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSuffix(csv.Content, "\n"), "\n")
	if rows[0] != "ACCOUNT,PERIOD,DATE,AMOUNT,SIDE,LOB,DESC,TYPE" || !strings.HasSuffix(csv.Name, ".csv") || !csv.Balanced {
		t.Fatalf("csv header %q name %s balanced %v", rows[0], csv.Name, csv.Balanced)
	}
	if !strings.Contains(csv.Content, `,"say ""hi"", ok",GJ`) {
		t.Errorf("quoting: %q", rows[1])
	}
	js, err := ProduceFile(d, "JSON_ALL", ol, oday("2026-01-01"), oday("2026-01-31"))
	if err != nil {
		t.Fatal(err)
	}
	var first map[string]string
	if err := json.Unmarshal([]byte(strings.Split(js.Content, "\n")[0]), &first); err != nil || first["ACCOUNT"] == "" || js.Records != 12 {
		t.Errorf("json lines: %v %v %d", first, err, js.Records)
	}
	if _, err := ProduceFile(d, "NOPE", ol, oday("2026-01-01"), oday("2026-01-31")); ErrorCode(err) != "UNKNOWN_PROFILE" {
		t.Errorf("unknown profile: %v", err)
	}
}

// ALTB-10: reversals the GL posts for reversing journals are left out unless the profile asks:
// the receiving ledger reverses them itself.
func TestReversalsAreLeftOutOfFilesByDefault(t *testing.T) {
	c := withOutputs(withReversingPremium(fixtureMap(t)))
	d := docOf(t, c)
	ol := outputLines(t, d, "E1")
	f, _ := ProduceFile(d, "JSON_ALL", ol, oday("2026-01-01"), oday("2026-02-28"))
	if f.Records != 3 || !strings.Contains(f.Content, `"TYPE":"RJ"`) {
		t.Errorf("without reversals: %d records\n%s", f.Records, f.Content)
	}
	c["outputProfiles"].([]any)[3].(map[string]any)["reversals"] = "INCLUDE"
	d = docOf(t, c)
	if f, _ := ProduceFile(d, "JSON_ALL", outputLines(t, d, "E1"), oday("2026-01-01"), oday("2026-02-28")); f.Records != 6 {
		t.Errorf("with reversals: %d records", f.Records)
	}
}

// ALTB-10: layouts and profiles are checked.
func TestOutputConfigurationIsValidated(t *testing.T) {
	if fs := validateMap(withOutputs(fixtureMap(t))); len(errorsOf(fs)) > 0 {
		t.Fatalf("valid outputs: %+v", errorsOf(fs))
	}
	for name, c := range map[string]struct {
		edit func(map[string]any)
		code string
	}{
		"overlap":   {func(c map[string]any) { field(c, 1)["start"] = 5 }, "OVERLAPPING_FIELDS"},
		"outside":   {func(c map[string]any) { field(c, 7)["length"] = 9 }, "OUTSIDE_RECORD"},
		"source":    {func(c map[string]any) { field(c, 0)["source"] = "NOPE" }, "UNKNOWN_SOURCE"},
		"dimension": {func(c map[string]any) { field(c, 5)["refs"] = []any{"NOPE"} }, "UNKNOWN_DIMENSION"},
		"layout":    {func(c map[string]any) { prof(c, 0)["layout"] = "NOPE" }, "UNKNOWN_LAYOUT"},
		"format":    {func(c map[string]any) { prof(c, 0)["format"] = "XML" }, "INVALID_FORMAT"},
		"account":   {func(c map[string]any) { prof(c, 1)["sections"].([]any)[0].(map[string]any)["accounts"] = []any{"999"} }, "UNKNOWN_ACCOUNT"},
		"group":     {func(c map[string]any) { prof(c, 0)["sections"].([]any)[0].(map[string]any)["group"] = "SOME" }, "INVALID_GROUP"},
		"override": {func(c map[string]any) {
			prof(c, 1)["fieldOverrides"] = []any{map[string]any{"name": "NOPE", "source": "CONSTANT"}}
		}, "UNKNOWN_FIELD"},
	} {
		m := withOutputs(fixtureMap(t))
		c.edit(m)
		if fs := validateMap(m); !hasCode(fs, c.code) {
			t.Errorf("%s: want %s, got %v", name, c.code, findingCodes(fs))
		}
	}
}

func field(c map[string]any, i int) map[string]any {
	return c["outputLayouts"].([]any)[0].(map[string]any)["fields"].([]any)[i].(map[string]any)
}
func prof(c map[string]any, i int) map[string]any {
	return c["outputProfiles"].([]any)[i].(map[string]any)
}

// ALTB-10: periods follow the financial year of the default date policy (GL-TEST-001: July).
func TestFiscalPeriodsFollowTheFinancialYear(t *testing.T) {
	d := docOf(t, fixtureMap(t))
	for date, want := range map[string][2]int{"2026-06-30": {2026, 12}, "2026-07-01": {2027, 1}, "2026-07-31": {2027, 1}, "2027-01-15": {2027, 7}} {
		if y, p := fiscalPeriod(d, oday(date)); y != want[0] || p != want[1] {
			t.Errorf("%s: %d/%d, want %d/%d", date, y, p, want[0], want[1])
		}
	}
}
