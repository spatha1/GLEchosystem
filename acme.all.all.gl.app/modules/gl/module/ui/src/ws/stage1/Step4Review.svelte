<script lang="ts">
	// Step 4 — everything in one place: what Stage 1 holds, what the server's validation finds,
	// whether every row of every imported sheet is accounted for, gaps, rule testing, and what was
	// accepted or rejected from the assistant. The checks are the engine's, not the AI's.
	import type { S1 } from './ctx';
	import type { Finding } from '../../lib/types';
	import type { Decision } from '../api';
	import { accounts, classes, coverage, overlaps, rules, servedBy, sourceClasses } from './model';
	import AIPanel from '../AIPanel.svelte';
	import RuleTester from './RuleTester.svelte';

	let { ctx }: { ctx: S1 } = $props();

	let findings = $state<Finding[] | null>(null);
	let decisions = $state<Decision[]>([]);
	let error = $state('');

	const STAGE1 = /^(accountingClasses|accounts|accountAssignmentRules|sourceClasses|sourceFiles|dimensions)\b/;
	$effect(() => {
		ctx.api.checkContent(ctx.version.configId, ctx.version.version, ctx.content).then(
			(r) => (findings = r.findings),
			(e) => (error = (e as Error).message)
		);
		ctx.api.decisions({ configId: ctx.version.configId }).then((d) => (decisions = d), () => {});
	});

	const c = $derived(ctx.content);
	const stage1 = $derived((findings ?? []).filter((f) => STAGE1.test(f.path) || !f.path));
	const others = $derived((findings ?? []).length - stage1.length);
	const errors = $derived(stage1.filter((f) => f.severity === 'error'));
	const warnings = $derived(stage1.filter((f) => f.severity !== 'error'));
	const cov = $derived(coverage(c));
	const srcs = $derived(sourceClasses(c));
	const unmapped = $derived(srcs.filter((s) => s.status !== 'NOT_USED' && !(s.mappings ?? []).length));
	const noRule = $derived(classes(c).filter((k) => !rules(c).some((r) => r.class === k.code)));
	const unusedAccounts = $derived(accounts(c).filter((a) => !servedBy(c, a).length));
	const badRefs = $derived(rules(c).filter((r) => !accounts(c).some((a) => a.code === r.account)));
	const dup = $derived(overlaps(c));
	const stdClasses = $derived(classes(c).filter((k) => k.standard));
	const unapproved = $derived(stdClasses.filter((k) => ctx.library.classes.find((x) => x.code === k.standard)?.status !== 'APPROVED'));
	const stepOf = (p: string) => (p.startsWith('accounts') ? 1 : p.startsWith('accountingClasses') || p.startsWith('sourceClasses') ? 2 : 3);
	const label = (f: Finding) => {
		const m = /^(\w+)\/(\d+)/.exec(f.path);
		if (!m) return f.path;
		const item = ((c[m[1]] ?? []) as any[])[Number(m[2])];
		return `${m[1]} ${item?.code ?? item?.account ?? ''}`.trim();
	};
</script>

<div class="with-ai">
	<div class="stack">
		{#if error}<p class="notice bad" role="alert">{error}</p>{/if}
		<div class="kpis">
			<div class="kpi"><b>{accounts(c).length}</b><span>accounts</span></div>
			<div class="kpi"><b>{classes(c).length}</b><span>classes ({stdClasses.length} standard)</span></div>
			<div class="kpi"><b>{srcs.length - unmapped.length}/{srcs.length}</b><span>source classes mapped</span></div>
			<div class="kpi"><b>{rules(c).length}</b><span>assignment rules</span></div>
			<div class="kpi"><b>{cov.length}</b><span>imported sheets</span></div>
		</div>

		<section class="card" aria-label="Validation findings">
			<div class="row">
				<h2 class="sec grow">Validation</h2>
				{#if findings === null}<span class="pill">checking…</span>{:else if errors.length}<span class="pill bad">{errors.length} to fix</span>{:else}<span class="pill ok">no blocking finding</span>{/if}
				{#if warnings.length}<span class="pill warn">{warnings.length} warnings</span>{/if}
			</div>
			<p class="small muted">The server’s own validation of the draft as saved{others ? `; ${others} findings outside Stage 1 (events, rules) are for Stage 2` : ''}.</p>
			{#if unapproved.length}<p class="notice warn small">{unapproved.length} standard class{unapproved.length === 1 ? '' : 'es'} used here {unapproved.length === 1 ? 'is' : 'are'} not approved in the library yet ({unapproved.map((k) => k.code).join(', ')}): the draft cannot be submitted until the product owner approves them.</p>{/if}
			{#if stage1.length}
				<div class="tw">
					<table class="grid">
						<thead><tr><th></th><th>Where</th><th>What</th><th></th></tr></thead>
						<tbody>
							{#each stage1.slice(0, 200) as f, i (i)}
								<tr>
									<td>{#if f.severity === 'error'}<span class="pill bad">error</span>{:else}<span class="pill warn">warning</span>{/if}</td>
									<td class="small mono">{label(f)}</td>
									<td class="small">{f.message}</td>
									<td><button class="btn ghost small" onclick={() => ctx.step(stepOf(f.path))}>Step {stepOf(f.path)} →</button></td>
								</tr>
							{/each}
						</tbody>
					</table>
				</div>
			{/if}
		</section>

		<section class="card" aria-label="Source coverage">
			<h2 class="sec">Every source row accounted for</h2>
			{#if !cov.length}<p class="muted small">No sheet was imported: nothing to check against.</p>{/if}
			{#each cov as x (x.file.id)}
				<div class="cov">
					<div class="row">
						<b>{x.file.name}{x.file.sheet ? ` › ${x.file.sheet}` : ''}</b>
						<span class="pill">{x.file.kind}</span>
						<span class="pill ok">{x.imported.length} imported</span>
						{#if x.skipped.length}<span class="pill warn">{x.skipped.length} not imported, with a reason</span>{/if}
						{#if x.missing.length}<span class="pill bad">{x.missing.length} unaccounted</span>{:else}<span class="pill ok">all {x.file.rows} rows accounted for</span>{/if}
					</div>
					{#if x.skipped.length}<details class="small"><summary>Rows not imported</summary>{#each x.skipped as k, ki (ki)}<div>row {k.row}: {k.reason}</div>{/each}</details>{/if}
					{#if x.missing.length}<p class="small bad">Rows {x.missing.slice(0, 30).join(', ')}{x.missing.length > 30 ? '…' : ''} are neither imported nor explained.</p>{/if}
				</div>
			{/each}
			<p class="small muted">Full coverage of the client’s original DCT assignment workbook is verified once that workbook is imported.</p>
		</section>

		<section class="card" aria-label="Gaps">
			<h2 class="sec">Gaps</h2>
			<ul class="gaps">
				<li class:okk={!unmapped.length}>{unmapped.length ? `${unmapped.length} source classes not mapped: ${unmapped.map((s) => s.code).join(', ')}` : 'Every source class is mapped or marked not used.'}</li>
				<li class:okk={!noRule.length}>{noRule.length ? `${noRule.length} classes with no assignment rule: ${noRule.map((k) => k.code).join(', ')}` : 'Every class has at least one assignment rule.'}</li>
				<li class:okk={!badRefs.length}>{badRefs.length ? `${badRefs.length} rules point to accounts not in the chart: ${badRefs.slice(0, 6).map((r) => `${r.code}→${r.account}`).join(', ')}` : 'Every rule points to an account of the chart.'}</li>
				<li class:okk={!dup.length}>{dup.length ? `${dup.length} pairs of rules with identical conditions` : 'No two rules of a class have identical conditions.'}</li>
				<li class="muted">{unusedAccounts.length} of {accounts(c).length} accounts receive no class yet (fine for accounts GL never posts to).</li>
			</ul>
		</section>

		<RuleTester {ctx} />

		<section class="card" aria-label="Assistant decisions">
			<h2 class="sec">What was accepted or rejected from the assistant</h2>
			{#if !decisions.length}<p class="muted small">No AI suggestion has been accepted or rejected for this configuration.</p>{:else}
				<div class="tw"><table class="grid"><thead><tr><th>When</th><th>Who</th><th>Step</th><th>Suggestion</th><th>Decision</th></tr></thead><tbody>
					{#each decisions as d (d.id)}<tr><td class="small">{d.at.replace('T', ' ').slice(0, 16)}</td><td class="small">{d.actor}</td><td class="small">{d.step}</td><td class="small">{d.title}</td><td><span class="pill {d.decision === 'REJECTED' ? 'warn' : 'ok'}">{d.decision.toLowerCase()}</span></td></tr>{/each}
				</tbody></table></div>
			{/if}
		</section>
	</div>
	<AIPanel
		api={ctx.api}
		area="stage1"
		step="review"
		configId={ctx.version.configId}
		version={ctx.version.version}
		documents={ctx.files}
		canAct={false}
		examples={['Review the complete Stage 1 configuration.', 'Which source rows are missing from the configuration?', 'Are any accounting classes mapped to conflicting accounts?', 'Which issues must be resolved before submitting?']}
	/>
</div>

<style>
	.grow {
		flex: 1;
	}
	.cov {
		padding: 8px 0;
		border-bottom: 1px solid var(--line);
	}
	.bad {
		color: var(--bad);
	}
	.gaps {
		margin: 0;
		padding-left: 18px;
		display: grid;
		gap: 4px;
	}
	.gaps li {
		color: var(--warn);
	}
	.gaps li.okk {
		color: var(--ok);
	}
	summary {
		cursor: pointer;
	}
</style>
