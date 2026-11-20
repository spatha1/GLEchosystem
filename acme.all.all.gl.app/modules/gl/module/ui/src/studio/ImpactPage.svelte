<script lang="ts">
	import type { ApiError } from '../lib/api';
	import type { Ctx } from '../lib/ctx';
	import type { Impact } from '../lib/studio';

	let { ctx }: { ctx: Ctx } = $props();

	const defaultAgainst = $derived(ctx.version.basedOnVersion ?? (ctx.version.version > 1 ? ctx.version.version - 1 : 0));
	let against = $state(0);
	let impact = $state<Impact | null>(null);
	let failure = $state<ApiError | null>(null);
	let open = $state<Record<string, boolean>>({});

	$effect(() => {
		if (!against) against = defaultAgainst;
	});
	$effect(() => {
		ctx.tick;
		const a = against;
		if (!a) return;
		impact = null;
		ctx.api.impact(ctx.version.configId, ctx.version.version, a).then(
			(i) => {
				impact = i;
				failure = null;
			},
			(e) => (failure = e)
		);
	});

	const go = (event: string) => ctx.go({ configId: ctx.version.configId, version: ctx.version.version, event });
	const sectionWord: Record<string, string> = {
		postingRules: 'posting rule',
		derivedMeasures: 'formula',
		accounts: 'account',
		accountAssignmentRules: 'assignment rule',
		accountingClasses: 'accounting class',
		events: 'event',
		dimensions: 'dimension',
		lookups: 'lookup',
		keyProfiles: 'business key',
		datePolicies: 'date policy',
		calendars: 'calendar',
		sourceAuthority: 'source authority',
		sourceSystems: 'source system',
		baseMeasures: 'measure',
		scenarios: 'scenario'
	};
	const remap = $derived(impact?.events.reduce((n, e) => n + e.remap, 0) ?? 0);
	const changedScenarios = $derived(impact?.scenarios.filter((s) => s.changed) ?? []);
</script>

{#if !defaultAgainst && !against}
	<div class="card"><p class="muted">Version 1 has no earlier version to compare with.</p></div>
{:else}
	<section class="card head">
		<div class="row">
			<h3 class="grow">What changes from v{against} to v{ctx.version.version}</h3>
			<label class="lbl">Compare with
				<select class="field" bind:value={against}>
					{#each Array.from({ length: ctx.version.version - 1 }, (_, i) => i + 1) as v (v)}<option value={v}>v{v}{v === ctx.version.basedOnVersion ? ' (based on)' : ''}</option>{/each}
				</select>
			</label>
		</div>
		{#if impact}
			<div class="row stats">
				<div class="stat"><b>{impact.changes.length}</b><span>configuration items changed</span></div>
				<div class="stat"><b>{impact.events.length}</b><span>events affected</span></div>
				<div class="stat"><b>{impact.manifest.events.reduce((n, e) => n + e.fields.length, 0)}</b><span>required-field changes</span></div>
				<div class="stat"><b>{remap}</b><span>fields to (re)map</span></div>
				<div class="stat"><b>{changedScenarios.length}</b><span>scenario journals change</span></div>
			</div>
		{/if}
	</section>

	{#if failure}
		<div class="notice bad" role="alert"><strong>{failure.code}</strong> {failure.message}</div>
	{:else if !impact}
		<p class="muted">Comparing…</p>
	{:else}
		<div class="grid2">
			<section class="card">
				<h3>Changed configuration</h3>
				{#if impact.changes.length}
					<table class="data">
						<thead><tr><th>Item</th><th>Change</th><th>What</th></tr></thead>
						<tbody>
							{#each impact.changes as c (c.section + c.key)}
								<tr>
									<td><span class="small muted">{sectionWord[c.section] ?? c.section}</span> <span class="mono"><b>{c.key}</b></span></td>
									<td><span class="badge {c.change === 'ADDED' ? 'ok' : c.change === 'REMOVED' ? 'bad' : 'warn'}">{c.change.toLowerCase()}</span></td>
									<td class="small mono">{c.fields.join(', ') || '—'}</td>
								</tr>
							{/each}
						</tbody>
					</table>
				{:else}
					<p class="muted">The two versions have the same content.</p>
				{/if}
			</section>
			<section class="card">
				<h3>Events affected</h3>
				{#each impact.events as e (e.eventType)}
					<div class="ev">
						<button class="btn link mono" onclick={() => go(e.eventType)}><b>{e.eventType}</b></button>
						<span class="small muted">because of</span>
						{#each e.because ?? [] as b (b)}<span class="chip mono">{b.replace(':', ' ')}</span>{/each}
						{#if e.fieldsChanged}<span class="small muted"> · {e.fieldsChanged} field change(s), {e.remap} to remap</span>{/if}
					</div>
				{:else}
					<p class="muted small">No event is affected.</p>
				{/each}
			</section>
		</div>

		<section class="card">
			<h3>Required fields and source mappings</h3>
			<p class="small muted">
				Manifest digest {impact.manifest.fromDigest.slice(7, 15)} → {impact.manifest.toDigest.slice(7, 15)}{impact.manifest.changed ? '' : ' (unchanged: mappings stay valid)'}.
			</p>
			{#each impact.manifest.events as e (e.eventType)}
				<h4>{e.eventType} <span class="muted">({e.change.toLowerCase()})</span></h4>
				<table class="data">
					<thead><tr><th>Field</th><th>Change</th><th>Details</th><th>Mapping</th></tr></thead>
					<tbody>
						{#each e.fields as f (f.listCode + f.fieldCode)}
							<tr>
								<td class="mono"><b>{f.listCode ? `${f.listCode}.` : ''}{f.fieldCode}</b> <span class="small muted">{f.kind.toLowerCase()}</span></td>
								<td><span class="badge {f.change === 'ADDED' ? 'ok' : f.change === 'REMOVED' ? 'bad' : 'warn'}">{f.change.toLowerCase()}</span></td>
								<td class="small">{(f.details ?? []).join('; ')}</td>
								<td>{#if f.remap}<span class="badge warn">needs mapping review</span>{:else}<span class="badge off">no remap</span>{/if}</td>
							</tr>
						{/each}
					</tbody>
				</table>
			{:else}
				<p class="muted small">No required field changes.</p>
			{/each}
		</section>

		<section class="card">
			<h3>Simulated journals before and after</h3>
			<table class="data">
				<thead><tr><th>Scenario</th><th>Event</th><th>v{against}</th><th>v{ctx.version.version}</th><th>Differences</th><th></th></tr></thead>
				<tbody>
					{#each impact.scenarios as s (s.code)}
						<tr class:changed={s.changed}>
							<td class="mono"><b>{s.code}</b></td>
							<td class="mono small">{s.eventType}</td>
							<td>{#if s.before}<span class="badge {s.before.status === 'PASSED' ? 'ok' : s.before.status === 'RAN' ? 'info' : 'bad'}">{s.before.status.toLowerCase()}</span>{:else}—{/if}</td>
							<td>{#if s.after}<span class="badge {s.after.status === 'PASSED' ? 'ok' : s.after.status === 'RAN' ? 'info' : 'bad'}">{s.after.status.toLowerCase()}</span>{:else}—{/if}</td>
							<td class="small">{(s.differences ?? []).join('; ') || 'same journal'}</td>
							<td><button class="btn" onclick={() => (open = { ...open, [s.code]: !open[s.code] })}>{open[s.code] ? 'Hide' : 'Legs'}</button></td>
						</tr>
						{#if open[s.code]}
							<tr class="legs">
								<td colspan="6">
									<div class="grid2">
										<div><h4>v{against}</h4><ul class="mono small">{#each s.beforeLegs as l, i (i)}<li class:gone={!s.afterLegs.includes(l)}>{l}</li>{:else}<li class="muted">{s.before?.error?.code ?? 'no legs'}</li>{/each}</ul></div>
										<div><h4>v{ctx.version.version}</h4><ul class="mono small">{#each s.afterLegs as l, i (i)}<li class:new={!s.beforeLegs.includes(l)}>{l}</li>{:else}<li class="muted">{s.after?.error?.code ?? 'no legs'}</li>{/each}</ul></div>
									</div>
									{#if s.after?.mismatches?.length}<p class="small notice warn">Against its expected journal: {s.after.mismatches.join('; ')}</p>{/if}
								</td>
							</tr>
						{/if}
					{/each}
				</tbody>
			</table>
		</section>

		<section class="card">
			<h3>Downstream</h3>
			{#each impact.downstream as d (d.stage)}
				<div class="ds"><span class="badge off">{d.label}: not built yet</span> <span class="small">{d.detail}</span> <span class="small muted">· {d.story}</span></div>
			{/each}
		</section>
	{/if}
{/if}

<style>
	.head {
		margin-bottom: 14px;
	}
	.grow {
		flex: 1;
	}
	.stats {
		gap: 10px;
		margin-top: 8px;
	}
	.stat {
		display: grid;
		padding: 8px 14px;
		border-radius: 8px;
		background: var(--gl-surface-2);
		border: 1px solid var(--gl-border);
	}
	.stat b {
		font-size: 20px;
	}
	.stat span {
		font-size: 11.5px;
		color: var(--gl-muted);
	}
	.ev {
		padding: 6px 0;
		border-top: 1px solid var(--gl-border);
		display: flex;
		flex-wrap: wrap;
		gap: 6px;
		align-items: center;
	}
	.ev:first-of-type {
		border-top: 0;
	}
	tr.changed td {
		background: var(--gl-warn-soft);
	}
	.legs td {
		background: var(--gl-surface-2);
	}
	.legs ul {
		margin: 0;
		padding-left: 16px;
	}
	.gone {
		color: var(--gl-bad);
		text-decoration: line-through;
	}
	.new {
		color: var(--gl-ok);
		font-weight: 650;
	}
	.ds {
		padding: 4px 0;
	}
</style>
