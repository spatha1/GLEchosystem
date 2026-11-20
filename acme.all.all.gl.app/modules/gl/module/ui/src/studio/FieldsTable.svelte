<script lang="ts">
	// The required-field manifest (contract C2) of one event, with every reason. Derived from
	// the configuration by the server (US-014); nothing here is typed in by hand.
	import type { EventExplanation, Graph, ManifestEvent } from '../lib/studio';

	let {
		explanation,
		manifest,
		known = {},
		graph,
		mappingColumn = true
	}: { explanation: EventExplanation; manifest: ManifestEvent; known?: Record<string, string>; graph?: Graph; mappingColumn?: boolean } = $props();

	let open = $state<Record<string, boolean>>({});
	const key = (f: { listCode?: string; fieldCode: string }) => `${f.listCode ?? ''}.${f.fieldCode}`;
	const mf = (f: { listCode?: string; fieldCode: string }) => manifest.fields.find((m) => key(m) === key(f));
	const reqLabel: Record<string, string> = {
		POSTING_RULE: 'rule line',
		FORMULA: 'formula',
		FILTER: 'rule filter',
		KEY_PROFILE: 'business key',
		DATE_POLICY: 'date policy',
		ACCOUNT_ASSIGNMENT: 'account choice',
		LOOKUP: 'lookup',
		EVENT_RECOGNITION: 'event identity',
		RECONCILIATION: 'reconciliation'
	};
	const sources = $derived(explanation.fields.filter((f) => f.kind === 'SOURCE'));
	const calculated = $derived(explanation.fields.filter((f) => f.kind === 'DERIVED'));
</script>

<div class="stack">
	<section class="card">
		<h3>Fields to collect for {explanation.eventType} <span class="muted">({sources.length}, of which {sources.filter((f) => f.mandatory).length} mandatory)</span></h3>
		<p class="small muted">
			Compiled from the configuration: a field is listed only because something configured reads it. Open a field to see every rule, formula, key or policy
			that needs it.
		</p>
		<div class="scroll">
			<table class="data">
				<thead>
					<tr>
						<th>Field</th>
						<th>Type</th>
						<th>Needed</th>
						<th>Source</th>
						<th>Canonical event field</th>
						<th>Needed by</th>
						{#if mappingColumn}<th>Source mapping</th>{/if}
						<th></th>
					</tr>
				</thead>
				<tbody>
					{#each sources as f (key(f))}
						{@const m = mf(f)}
						<tr>
							<td>
								<span class="mono"><b>{f.listCode ? `${f.listCode}[ ].` : ''}{f.fieldCode}</b></span>
								<div class="small muted">{f.label}</div>
							</td>
							<td class="small">{m?.dataType.toLowerCase()}{m?.cardinality === 'LIST' ? ' · per item' : ''}{m?.keyRole ? ` · ${m.keyRole.replace('_', ' ').toLowerCase()}` : ''}</td>
							<td>{#if f.mandatory}<span class="badge bad">mandatory</span>{:else}<span class="badge info">optional</span>{/if}</td>
							<td class="small">{f.sourceSystems?.join(', ') || '—'}</td>
							<td class="mono small">{f.path}</td>
							<td class="small">
								{#each [...new Set((m?.requiredBy ?? []).map((r) => reqLabel[r.kind] ?? r.kind))] as k (k)}<span class="chip">{k}</span> {/each}
							</td>
							{#if mappingColumn}<td class="small muted">not mapped yet</td>{/if}
							<td><button class="btn" aria-expanded={!!open[key(f)]} onclick={() => (open = { ...open, [key(f)]: !open[key(f)] })}>{open[key(f)] ? 'Hide' : 'Why'}</button></td>
						</tr>
						{#if open[key(f)]}
							<tr class="why">
								<td colspan={mappingColumn ? 8 : 7}>
									<ul>
										{#each f.reasons as r, i (i)}
											<li>
												{#if r.mandatory}<span class="badge bad">mandatory</span>{:else}<span class="badge info">optional</span>{/if}
												{r.detail}
												{#if r.via?.length}<span class="via">via {r.via.join(' → ')}</span>{/if}
												<span class="muted small"> · {reqLabel[r.kind] ?? r.kind} <span class="mono">{r.ref}</span></span>
											</li>
										{/each}
									</ul>
									{#if f.constraints?.length}<p class="small muted">Constraints: {f.constraints.join(' · ')}</p>{/if}
								</td>
							</tr>
						{/if}
					{/each}
				</tbody>
			</table>
		</div>
	</section>

	<section class="card">
		<h3>Calculated by GL — never requested from a source <span class="muted">({calculated.length})</span></h3>
		{#each calculated as f (f.fieldCode)}
			<div class="calc">
				<span class="mono"><b>{f.fieldCode}</b></span>
				<span class="badge info">{f.category === 'FORMULA' ? 'formula' : f.category === 'LOOKUP' ? 'lookup' : 'date policy'}</span>
				<span class="mono small">{f.detail}</span>
				<div class="small muted">
					from {f.derivedFrom?.join(', ')} (requested instead) · used by {f.reasons.map((r) => r.ref).filter((x, i, a) => a.indexOf(x) === i).join(', ')}
				</div>
			</div>
		{:else}
			<p class="muted small">Nothing is calculated for this event.</p>
		{/each}
	</section>

	{#if explanation.lists.length || explanation.unused.length}
		<div class="grid2">
			{#if explanation.lists.length}
				<section class="card">
					<h3>Lists</h3>
					{#each explanation.lists as l (l.fieldCode)}
						<p class="small"><span class="mono"><b>{l.fieldCode}</b></span> — {l.detail}</p>
						<ul class="small">
							{#each l.reasons as r, i (i)}<li>{r.detail}</li>{:else}<li class="muted">nothing expands by this list</li>{/each}
						</ul>
					{/each}
				</section>
			{/if}
			{#if explanation.unused.length}
				<section class="card">
					<h3>Declared but not used</h3>
					<p class="small muted">The event can carry these, but nothing configured reads them, so they are not requested.</p>
					{#each explanation.unused as u (u.node)}
						<span class="chip mono" title={u.label}>{u.listCode ? `${u.listCode}.` : ''}{u.fieldCode}</span>
					{/each}
				</section>
			{/if}
		</div>
	{/if}
</div>

<style>
	.why td {
		background: var(--gl-surface-2);
	}
	.why ul {
		margin: 0;
		padding-left: 0;
		list-style: none;
		display: grid;
		gap: 6px;
	}
	.why li {
		font-size: 13px;
	}
	.via {
		font-family: var(--gl-mono);
		font-size: 12px;
		color: var(--gl-accent);
		margin-left: 4px;
	}
	.calc {
		padding: 8px 0;
		border-top: 1px solid var(--gl-border);
	}
	.calc:first-of-type {
		border-top: 0;
	}
</style>
