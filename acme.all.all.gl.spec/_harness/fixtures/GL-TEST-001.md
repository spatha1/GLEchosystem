# GL-TEST-001 — Policy P1 synthetic acceptance test

One synthetic policy runs through source field requirements, agent mapping, data collection, event normalization, journal rules, lifecycle, period handling, intercompany and key-based correction. This is a **test scenario, not a complete universal accounting rulebook**.

| Event | When | Intended accounting behavior | Expected lines |
|---|---|---|---:|
| E1 | 01-Jan | Premium 1000 + tax 100; AR0 DR 1100, premium CR 1000, tax CR 100 | 3 |
| E2 | 05-Jan | INV1=550 and INV2=550; move from AR0 to AR1, separate invoice legs | 4 |
| E3 | 10-Jan | Receipt 1099.50 into C09 bank, covering C01 policy's INV1=550, INV2=549.50, IC due-to/from | 5 |
| E4 | 15-Jan | Write off 0.50 from INV2 based on posted OPEN_BALANCE state | 2 |
| E5 | 20-Jan | 200 endorsement effective 01-Feb; book as advance in January | 2 |
| E5R | 01-Feb | Reclass advance 200 to current (4 movement legs) | 4 |
| E6 | 05-Feb | Original endorsement corrected from 200 to 220; rebuild P1, book only +20 delta | 2 |
| **Total** | | **Seven linked accounting groups, company/currency balanced** | **22** |

P1 expected after all events: INV1 open=0.00, INV2 open=0.00, AR0 policy outstanding=220.00, matched IC=0, no additional E1–E5 posts on replay. The algorithm must not treat all positive cash as taxable GST, all invoices as cash received, or outstanding subledger balance as an unbalanced journal.

### Required field compile examples

- E1: PREMIUM, TAX, COMPANY, LOB, CURRENCY, POLICY_NO, transaction/effective dates.
- E2: INVOICE_LIST, invoice ID/amount, policy linkage, transaction/effective dates.
- E3: PAY_AMT, BANK_ACCOUNT, ALLOCATION_LIST, receipt ID, invoice linkage, dates.
- E4: INVOICE_NO, date, relevant policy/entity; write-off amount from **ledger state**, not source field.
- E5/E6: premium correction identity, policy link, date/effective-date and version.

Important nuance: `BOOKING_CLASS` and `BANKING_COMPANY` are derived; the mapping agent should seek only their *inputs*. Other test cases should cover alternative source authorities and conventions.
