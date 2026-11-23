<script lang="ts">
	import { untrack } from 'svelte';
	// Test which account a line would get: a class (or a source class and its system) with
	// condition values, a company, a currency and a date. The server answers with the rule it
	// matched, why, every rule it considered, and whether the posting gate accepts the account.
	import type { S1 } from './ctx';
	import type { AssignResult } from '../api';
	import { classes, rules, sourceClasses } from './model';

	let { ctx }: { ctx: S1 } = $props();

	let by = $state<'class' | 'source'>('class');
	let cls = $state('');
	let src = $state('');
	let conds = $state<{ k: string; v: string }[]>([{ k: '', v: '' }]);
	let entity = $state('');
	let currency = $state('');
	let date = $state(untrack(() => ctx.version.effectiveFrom));
	let res = $state<AssignResult | null>(null);
	let error = $state('');
	let busy = $state(false);

	const keys = $derived([...new Set([...rules(ctx.content).flatMap((r) => Object.keys(r.match ?? {})), ...((ctx.content.dimensions ?? []) as { code: string }[]).map((d) => d.code)])].sort());

	async function run() {
		busy = true;
		error = '';
		res = null;
		try {
			const [system, code] = src.split('/');
			res = await ctx.api.assign(ctx.version.configId, ctx.version.version, {
				...(by === 'class' ? { class: cls } : { sourceSystem: system, sourceClass: code }),
				conditions: Object.fromEntries(conds.filter((c) => c.k && c.v).map((c) => [c.k, c.v])),
				entity: entity || undefined,
				currency: currency || undefined,
				bookDate: date
			});
		} catch (e) {
			error = (e as Error).message;
		} finally {
			busy = false;
		}
	}
</script>

<section class="card" aria-label="Test a rule">
	<h2 class="sec">Test which account a line gets</h2>
	<div class="row">
		<select class="field" bind:value={by} aria-label="Test by"><option value="class">Class</option><option value="source">Source class</option></select>
		{#if by === 'class'}
			<select class="field" bind:value={cls} aria-label="Class to test"><option value="">Choose…</option>{#each classes(ctx.content) as c, ci (ci)}<option value={c.code}>{c.code}</option>{/each}</select>
		{:else}
			<select class="field" bind:value={src} aria-label="Source class to test"><option value="">Choose…</option>{#each sourceClasses(ctx.content) as s, si (si)}<option value={`${s.system}/${s.code}`}>{s.code} ({s.system})</option>{/each}</select>
		{/if}
		<input class="field mono" style="width:120px" bind:value={entity} placeholder="Company" aria-label="Company" />
		<input class="field mono" style="width:80px" bind:value={currency} placeholder="CCY" aria-label="Currency" />
		<input class="field" type="date" bind:value={date} aria-label="Book date" />
	</div>
	{#each conds as c, i (i)}
		<div class="row cond">
			<input class="field mono" list="ws-cond-keys" bind:value={c.k} placeholder="CONDITION" aria-label="Condition" oninput={() => (c.k = c.k.toUpperCase())} />
			<span class="muted">=</span>
			<input class="field mono" bind:value={c.v} placeholder="value" aria-label="Condition value" />
			{#if i === conds.length - 1}<button class="btn ghost small" onclick={() => (conds = [...conds, { k: '', v: '' }])}>+ condition</button>{/if}
		</div>
	{/each}
	<datalist id="ws-cond-keys">{#each keys as k (k)}<option value={k}></option>{/each}</datalist>
	<div class="row"><button class="btn primary" disabled={busy || (by === 'class' ? !cls : !src) || !date} onclick={run}>{busy ? 'Testing…' : 'Test'}</button></div>
	{#if error}<p class="notice bad small" role="alert">{error}</p>{/if}
	{#if res}
		<div class="res" role="status">
			{#if res.mappedBy}<p class="small muted">Source class: {res.mappedBy}{res.class ? ` → ${res.class}` : ''}</p>{/if}
			{#if res.error}
				<p class="notice warn"><b>{res.error.code}</b> — {res.error.message}</p>
			{:else if res.assignment}
				<p class="big">Account <b class="mono">{res.assignment.account}</b> by rule <span class="mono">{res.assignment.rule}</span> {#if res.allowed}<span class="pill ok">posting allowed</span>{:else}<span class="pill bad">refused: {res.gate?.code}</span>{/if}</p>
				<p class="small">{res.assignment.why.join('; ')}</p>
				{#if res.gate}<p class="small muted">{res.gate.message}</p>{/if}
			{/if}
			{#if res.assignment?.considered?.length}
				<details class="small"><summary>{res.assignment.considered.length} rules considered</summary>
					{#each res.assignment.considered as k, ki (ki)}<div class:m={k.matched}><span class="mono">{k.rule}</span> → {k.account}: {k.matched ? '✓ ' : '✗ '}{k.reason}</div>{/each}
				</details>
			{/if}
		</div>
	{/if}
</section>

<style>
	.cond {
		margin-top: 8px;
	}
	.res {
		margin-top: 12px;
		border-top: 1px solid var(--line);
		padding-top: 10px;
	}
	.big {
		font-size: 15px;
		margin: 0 0 4px;
	}
	.m {
		color: var(--ok);
	}
	summary {
		cursor: pointer;
	}
	.row {
		margin-top: 8px;
	}
</style>
