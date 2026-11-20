// Sample events from the synthetic GL-TEST-001 scenario (policy P1), for the posting preview.

export interface Sample {
	id: string;
	label: string;
	event: Record<string, unknown>;
}

export const samples: Sample[] = [
	{
		id: 'E1',
		label: 'E1 · Premium 1,000 + tax 100 (motor)',
		event: {
			eventType: 'PREM_ADDED',
			currency: 'AUD',
			transactionDate: '2026-01-01',
			effectiveDate: '2026-01-01',
			keys: { POLICY_NO: 'P1', TERM_NO: '1' },
			measures: { PREMIUM: '1000.00', TAX: '100.00' },
			dimensions: { LOB: 'MOTOR', COMPANY: 'C01', PRODUCT: 'MOTOR_PRIVATE' }
		}
	},
	{
		id: 'E1H',
		label: 'E1 · Same premium, home (default AR0 account)',
		event: {
			eventType: 'PREM_ADDED',
			currency: 'AUD',
			transactionDate: '2026-01-01',
			effectiveDate: '2026-01-01',
			keys: { POLICY_NO: 'P1', TERM_NO: '1' },
			measures: { PREMIUM: '1000.00', TAX: '100.00' },
			dimensions: { LOB: 'HOME', COMPANY: 'C01', PRODUCT: 'HOME_BUILDING' }
		}
	},
	{
		id: 'E2',
		label: 'E2 · Two invoices of 550',
		event: {
			eventType: 'INVOICED',
			currency: 'AUD',
			transactionDate: '2026-01-05',
			effectiveDate: '2026-01-05',
			keys: { POLICY_NO: 'P1', TERM_NO: '1', INVOICE_RUN: 'RUN1' },
			dimensions: { COMPANY: 'C01', LOB: 'MOTOR' },
			lists: {
				INVOICE_LIST: [
					{ INVOICE_NO: 'INV1', INVOICE_AMT: '550.00' },
					{ INVOICE_NO: 'INV2', INVOICE_AMT: '550.00' }
				]
			}
		}
	},
	{
		id: 'E3',
		label: 'E3 · Receipt 1,099.50 into the C09 bank',
		event: {
			eventType: 'PAYMENT_RECEIVED',
			currency: 'AUD',
			transactionDate: '2026-01-10',
			effectiveDate: '2026-01-10',
			keys: { RECEIPT_NO: 'R1' },
			measures: { PAY_AMT: '1099.50' },
			dimensions: { BANK_ACCOUNT: 'BANK-C09', COMPANY: 'C01' },
			lists: {
				ALLOCATION_LIST: [
					{ ALLOCATION_ID: 'A1', INVOICE_NO: 'INV1', ALLOC_AMT: '550.00' },
					{ ALLOCATION_ID: 'A2', INVOICE_NO: 'INV2', ALLOC_AMT: '549.50' }
				]
			}
		}
	},
	{
		id: 'E3X',
		label: 'E3 · Same receipt into a C01 bank (refused: no C01 bank account)',
		event: {
			eventType: 'PAYMENT_RECEIVED',
			currency: 'AUD',
			transactionDate: '2026-01-10',
			effectiveDate: '2026-01-10',
			keys: { RECEIPT_NO: 'R1' },
			measures: { PAY_AMT: '1099.50' },
			dimensions: { BANK_ACCOUNT: 'BANK-C01', COMPANY: 'C01' },
			lists: {
				ALLOCATION_LIST: [
					{ ALLOCATION_ID: 'A1', INVOICE_NO: 'INV1', ALLOC_AMT: '550.00' },
					{ ALLOCATION_ID: 'A2', INVOICE_NO: 'INV2', ALLOC_AMT: '549.50' }
				]
			}
		}
	}
];
