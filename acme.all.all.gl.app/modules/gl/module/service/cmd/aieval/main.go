// Command aieval measures the AI configuration assistant on a BRD: it runs the four steps
// (events, measures, formulas, debit/credit lines) against the real provider, accepting every
// item, and reports attempts, BRD quotes found, test cases balanced, problems and gaps.
//
// Every run calls the provider and costs money; nothing is written to any database.
//
//	GL_AI_SETTINGS=C:\path\ai-settings.env go run ./cmd/aieval \
//	   -brd ../../../../../acme.all.all.gl.spec/_harness/fixtures/brd/motor-renewals-brd.md \
//	   -config ../../../../../acme.all.all.gl.spec/_harness/fixtures/GL-TEST-001.config.json \
//	   -out ai-eval.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gl/ai"
	"gl/model"
)

func main() {
	brdPath := flag.String("brd", "", "the BRD (.docx, .txt or .md)")
	configPath := flag.String("config", "", "a configuration whose reference data (chart of accounts, keys, calendars) the run starts from")
	effective := flag.String("effective", "2026-12-01", "the date test cases are booked on")
	out := flag.String("out", "", "write the report as JSON to this file")
	flag.Parse()
	if *brdPath == "" || *configPath == "" {
		fmt.Fprintln(os.Stderr, "usage: aieval -brd <file> -config <configuration.json> [-out report.json]")
		os.Exit(2)
	}
	d := ai.Load(os.Getenv("GL_AI_SETTINGS"))
	if st := d.Status(); !st.Available {
		fmt.Fprintln(os.Stderr, st.Reason)
		os.Exit(1)
	}
	raw, err := os.ReadFile(*brdPath)
	fail(err)
	text, err := ai.ExtractText(filepath.Base(*brdPath), raw)
	fail(err)
	cfg, err := os.ReadFile(*configPath)
	fail(err)
	var wrapped map[string]json.RawMessage
	if json.Unmarshal(cfg, &wrapped) == nil && wrapped["content"] != nil {
		cfg = wrapped["content"]
	}
	content, err := ai.ReferenceOnly(cfg)
	fail(err)
	check := func(c []byte) ([]model.Finding, []byte, error) {
		var m map[string]any
		if err := json.Unmarshal(c, &m); err != nil {
			return nil, nil, err
		}
		canonical, _ := json.Marshal(m)
		return model.ValidateCanonical(canonical), canonical, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	rep := ai.Evaluate(ctx, d, ai.BRD{Name: filepath.Base(*brdPath), Text: text}, content, *effective, check)

	fmt.Printf("BRD %s · model %s\n", rep.BRD, rep.Model)
	fmt.Printf("%-9s %8s %6s %10s %8s %5s %8s %8s\n", "step", "attempts", "items", "quotes ok", "cases ok", "gaps", "problems", "tokens")
	for _, s := range rep.Steps {
		fmt.Printf("%-9s %8d %6d %5d/%-4d %4d/%-3d %5d %8d %8d", s.Step, s.Attempts, s.Items, s.QuotesFound, s.Quotes, s.CasesBalanced, s.Cases, s.Gaps, len(s.Problems), s.Tokens)
		if s.Error != "" {
			fmt.Printf("  error: %s", s.Error)
		}
		fmt.Println()
		for _, p := range s.Problems {
			fmt.Println("   problem:", p)
		}
	}
	fmt.Printf("events configured: %v · blocking findings left (other than account numbers and dimension values): %d · %s\n",
		rep.Events, rep.Findings, map[bool]string{true: "PASS", false: "NEEDS WORK"}[rep.OK])
	if *out != "" {
		b, _ := json.MarshalIndent(rep, "", "  ")
		fail(os.WriteFile(*out, b, 0o644))
	}
	if !rep.OK {
		os.Exit(3)
	}
}

func fail(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
