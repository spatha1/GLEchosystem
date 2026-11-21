# Business Requirements Document: Motor renewals and cancellations

Synthetic sample for the GL studio's AI-assisted configuration. It names no client, person or real policy.

## 1. Purpose

The general ledger must record motor policy renewals and mid-term cancellations from the policy administration system, so that finance can report written premium, taxes collected, duties owed and broker commission each month.

## 2. Scope

- Line of business: motor (private cars). Home policies renewed in the same system follow the same rules.
- Source: the policy administration system sends one event per renewal and one per cancellation.
- Legal entity: each policy belongs to one company; every journal balances per company and currency.
- Currency: all amounts are in Australian dollars (AUD).
- Out of scope: claims, reinsurance and the mapping of source fields to the policy system (handled separately in the Data Workbench).

## 3. Business events

3.1 Renewal. When a policy is renewed for a new term, the policy system sends a renewal event identified by the policy number and the term number. If the policy was sold through a broker, the event also carries the broker identifier.

3.2 Mid-term cancellation. When a policy is cancelled during its term, the policy system sends a cancellation event identified by the policy number and the term number. The unused premium is returned to the policyholder.

## 4. Amounts sent by the source

4.1 For a renewal the policy system sends the premium for the new term, the GST on that premium, the stamp duty, and the broker commission (zero when there is no broker).

4.2 For a cancellation the policy system sends the return premium and the GST on the return premium.

## 5. Calculations

5.1 The amount the policyholder owes for a renewal is the premium plus GST plus stamp duty.

5.2 The amount refunded on a cancellation is the return premium plus the GST on it.

## 6. Accounting

6.1 Renewal: the amount the policyholder owes is a receivable. The premium is income (written premium). GST collected is owed to the tax office. Stamp duty collected is owed to the state revenue office.

6.2 Broker commission is an expense of the insurer and is owed to the broker; it is booked only when the policy was sold through a broker (commission greater than zero).

6.3 Cancellation: the written premium is reduced by the return premium, the GST owed to the tax office is reduced by the GST on it, and the amount refunded is owed to the policyholder.

6.4 Debits must equal credits for every journal, per company and currency.

## 7. Reporting dimensions

Every journal line carries the company, the line of business and the product.

## 8. Acceptance examples

- A motor renewal through a broker with premium 1,000.00, GST 100.00, stamp duty 90.00 and commission 150.00 posts a receivable of 1,190.00 and a commission of 150.00.
- A home renewal sold directly with premium 800.00, GST 80.00, stamp duty 40.00 and no commission posts a receivable of 920.00 and no commission.
- A cancellation with return premium 200.00 and GST 20.00 refunds 220.00.
