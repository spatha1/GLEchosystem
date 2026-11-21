<script lang="ts">
	import type { ApiError } from '../lib/api';
	import type { Ctx } from '../lib/ctx';
	import type { EventView, PostingRule } from '../lib/studio';
	import { accountText, entityText, glossary, movementOf, movementText, movementWord, natureWord, ruleWhen, sideWord } from '../lib/explain';
	import { clone, newRule, uniqueCode, withRule, withoutRule, under } from '../lib/content';
	import Formula from './Formula.svelte';
	import RuleEditor from './RuleEditor.svelte';
	import Findings from '../components/Findings.svelte';
	import ReasonDialog from '../components/ReasonDialog.svelte';

	let { ctx, view }: { ctx: Ctx; view: EventView } = $props();

	const code = $derived(ctx.route.rule!);
	const rv = $derived(view.rules.find((r) => r.rule.code === code));
	const known = $derived(glossary(view));
	const base = $derived({ configId: ctx.version.configId, version: ctx.version.version, event: view.event.code });
	const taken = $derived(((ctx.content.postingRules ?? []) as PostingRule[]).map((r) => r.code));
	const others = $derived(view.rules.filter((r) => r.rule.code !== code));

	// The rule being edited: an existing rule, a copy, or a new one.
	let copyOf = $state<PostingRule | null>(null);
	const editTarget = $derived.by<{ rule: PostingRule; original?: string } | null>(() => {
		if (!ctx.route.edit) return null;
		if (code === '+new') {
			if (copyOf) return { rule: copyOf };
			const maxRank = Math.max(0, ...view.rules.map((r) => r.rule.rank));
			return { rule: newRule(view.event.code, taken, maxRank + 10) };
		}
		return rv ? { rule: clone(rv.rule), original: rv.rule.code } : null;
	});

	// Quick changes (deactivate, activate, delete) with a reason.
	let pending = $state<{ title: string; verb: string; detail: string; apply: () => any; after: () => void } | null>(null);
	let busy = $state(false);
	let error = $state('');

	function toggleActive() {
		if (!rv) return;
		const r = clone(rv.rule);
		r.active = !rv.isActive;
		pending = {
			title: `${rv.isActive ? 'Deactivate' : 'Activate'} ${rv.rule.code}`,
			verb: rv.isActive ? 'Deactivate' : 'Activate',
			detail: rv.isActive
				? 'The rule stays in the version for the record but is never applied; events it posted are refused unless another rule applies.'
				: 'The rule applies again from this draft.',
			apply: () => withRule(ctx.content, r, rv.rule.code),
			after: () => {}
		};
	}

	function remove() {
		if (!rv) return;
		pending = {
			title: `Delete ${rv.rule.code} from draft v${ctx.version.version}`,
			verb: 'Delete',
			detail: 'Earlier versions keep the rule; this draft will not have it. Deactivating keeps it for the record instead.',
			apply: () => withoutRule(ctx.content, rv.rule.code),
			after: () => ctx.go({ ...base, etab: 'rules' })
		};
	}

	function copy() {
		if (!rv) return;
		const r = clone(rv.rule);
		r.code = uniqueCode(`${rv.rule.code}_COPY`, taken);
		r.description = `Copy of ${rv.rule.code}${rv.rule.description ? `: ${rv.rule.description}` : ''}`;
		copyOf = r;
		ctx.go({ ...base, rule: '+new', edit: true });
	}

	async function confirm(reason: string) {
		if (!pending) return;
		busy = true;
		error = '';
		try {
			await ctx.save(pending.apply(), reason);
			const after = pending.after;
			pending = null;
			after();
		} catch (e) {
			const err = e as ApiError;
			error = `${err.code}: ${err.message}`;
			pending = null;
		} finally {
			busy = false;
		}
	}

	const sourcesOfRule = $derived(
		view.explanation.fields.filter((f) => f.kind === 'SOURCE' && f.reasons.some((r) => r.ref === code || r.ref.startsWith(code + '/')))
	);
	const debits = $derived(rv?.lines.filter((l) => l.line.side === 'D').map((l) => l.line.class) ?? []);
	const credits = $derived(rv?.lines.filter((l) => l.line.side === 'C').map((l) => l.line.class) ?? []);
</script>

{#if editTarget}
	{#key `${code}/${editTarget.original ?? editTarget.rule.code}`}
		<RuleEditor
			{ctx}
			{view}
			rule={editTarget.rule}
			original={editTarget.original}
			onclose={(saved) => {
				copyOf = null;
				ctx.go({ ...base, rule: saved ?? (code === '+new' ? undefined : code), etab: saved || code !== '+new' ? undefined : 'rules' });
			}}
		/>
	{/key}
{:else if !rv}
	<div class="notice warn">Rule {code} is not in version {ctx.version.version} of this event.</div>
{:else}
	<section class="rhead">
		<div>
			<div class="kind">Posting rule of <button class="btn link mono" onclick={() => ctx.go(base)}>{view.event.code}</button></div>
			<div class="row">
				<h2 class="mono">{rv.rule.code}</h2>
				{#if rv.isActive}<span class="badge ok">Active</span>{:else}<span class="badge off">Deactivated</span>{/if}
				<span class="chip">rank {rv.rule.rank}</span>
				<span class="chip">configuration {ctx.version.configId} v{ctx.version.version} · {ctx.version.status}</span>
			</div>
			{#if rv.rule.description}<p class="purpose">{rv.rule.description}</p>{/if}
		</div>
		<div class="row">
			{#if ctx.canEdit}
				<button class="btn primary" onclick={() => ctx.go({ ...base, rule: code, edit: true })}>Edit rule</button>
				<button class="btn" onclick={copy}>Copy</button>
				<button class="btn" onclick={toggleActive}>{rv.isActive ? 'Deactivate' : 'Activate'}</button>
				<button class="btn danger" onclick={remove}>Delete</button>
			{:else if ctx.isEditor}
				<button class="btn primary" onclick={() => ctx.startDraft({ ...base, rule: code, edit: true })}>Change this rule in a new draft</button>
			{/if}
			<button class="btn" onclick={() => ctx.go({ ...base, etab: 'simulate' })}>Simulate</button>
		</div>
	</section>
	{#if error}<div class="notice bad" role="alert">{error}</div>{/if}

	<div class="grid2">
		<section class="card">
			<h3>When it applies</h3>
			<p>This rule {ruleWhen(rv, view.event.code)}.</p>
			<dl class="kv">
				<dt>Filter</dt>
				<dd>{#if rv.rule.filter}<Formula src={rv.rule.filter} {known} />{:else}<span class="muted">none: every {view.event.code}</span>{/if}</dd>
				<dt>Effective</dt>
				<dd>
					{#if rv.rule.effectiveFrom || rv.rule.effectiveTo}{rv.rule.effectiveFrom ?? 'version start'} → {rv.rule.effectiveTo ?? 'version end'} (book date){:else}the
						version's window: {ctx.version.effectiveFrom} → {ctx.version.effectiveTo ?? 'open'}{/if}
				</dd>
				<dt>Other rules</dt>
				<dd>
					{#each others as o (o.rule.code)}
						<button class="btn link mono" onclick={() => ctx.go({ ...base, rule: o.rule.code })}>{o.rule.code}</button>
						<span class="small muted">rank {o.rule.rank}{o.isActive ? '' : ', off'}</span>
					{:else}<span class="muted">none for this event</span>{/each}
				</dd>
			</dl>
		</section>
		<section class="card">
			<h3>Balance</h3>
			<p>
				Debits <span class="mono side-D">{debits.join(', ') || '—'}</span> against credits <span class="mono side-C">{credits.join(', ') || '—'}</span>. The
				engine refuses a journal whose debits and credits differ for any legal entity and currency.
			</p>
			<h4>Source fields this rule reads</h4>
			<div class="row">
				{#each sourcesOfRule as f (f.node)}<span class="chip mono" title={f.label}>{f.listCode ? `${f.listCode}.` : ''}{f.fieldCode}</span>{/each}
			</div>
		</section>
	</div>

	<section class="card lines">
		<h3>Debit and credit lines <span class="muted">({rv.lines.length})</span></h3>
		{#each rv.lines as l, i (l.line.code)}
			<article class="line">
				<div class="lnum side-{l.line.side}">{i + 1}</div>
				<div class="lbody">
					<div class="row">
						<span class="badge {l.line.side === 'D' ? 'info' : 'warn'} sideb">{sideWord(l.line.side)}</span>
						<span class="mono"><b>{l.line.class}</b></span>
						<span>{l.class?.name ?? 'unknown class'}</span>
						<span class="muted small">· line {l.line.code}</span>
						{#if l.class}{@const m = movementOf(l.line.side, l.class.normalSide)}<span class="mv {m === 'INCREASE' ? 'inc' : 'dec'}">{movementWord(m)}</span>{/if}
					</div>
					<ul class="explain">
						<li>
							<span class="k">Class</span>
							<span>{l.class ? `${l.class.name}: ${natureWord(l.class.nature)}, normally a ${l.class.normalSide === 'D' ? 'debit' : 'credit'}, ${l.class.statement.replaceAll('_', ' ').toLowerCase()}` : 'not an accounting class'}</span>
						</li>
						{#if l.class}<li><span class="k">Movement</span> <span>{movementText(l.line.side, l.class)}</span></li>{/if}
						<li>
							<span class="k">Amount</span>
							<span><Formula src={l.line.amount} {known} />
							{#each view.derived.filter((d) => d.code === l.line.amount.trim()) as d (d.code)}
								<span class="muted"> where {d.code} = </span><Formula src={d.formula} {known} />
							{/each}
							<span class="muted small"> · a negative amount posts on the other side</span></span>
						</li>
						{#if l.line.expandBy}
							<li><span class="k">Per item</span> <span>one {sideWord(l.line.side).toLowerCase()} per <span class="mono">{l.line.expandBy}</span> item, reading that item's values</span></li>
						{/if}
						<li>
							<span class="k">Only when</span>
							<span>{#if l.line.when}<Formula src={l.line.when} {known} />{:else}always{/if}</span>
						</li>
						<li><span class="k">Legal entity</span> <span>{entityText(view, l)}</span></li>
						<li>
							<span class="k">Account</span>
							<ol class="cands">
								{#each accountText(l) as t, j (j)}<li>{t}</li>{/each}
							</ol>
						</li>
						{#if l.line.explanation}<li><span class="k">Note</span> <span>{l.line.explanation}</span></li>{/if}
					</ul>
					{#if under(view.findings, l.path).length}<Findings findings={under(view.findings, l.path)} />{/if}
				</div>
			</article>
		{/each}
	</section>
	{#if under(view.findings, rv.path).length}
		<section class="card"><h3>Findings for this rule</h3><Findings findings={under(view.findings, rv.path)} /></section>
	{/if}
{/if}

{#if pending}
	<ReasonDialog title={pending.title} verb={pending.verb} detail={pending.detail} {busy} onsubmit={confirm} oncancel={() => (pending = null)} />
{/if}

<style>
	.mv {
		font-weight: 600;
		font-size: 12.5px;
	}
	.mv.inc {
		color: var(--gl-ok);
	}
	.mv.dec {
		color: var(--gl-warn);
	}
	.rhead {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		align-items: flex-end;
		gap: 12px;
		margin: 18px 0 14px;
	}
	.kind {
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--gl-muted);
		font-weight: 600;
	}
	.kind .btn {
		font-size: 11px;
		text-transform: none;
	}
	h2 {
		margin: 2px 0;
		font-size: 22px;
	}
	.purpose {
		margin: 4px 0 0;
		font-size: 15px;
	}
	.lines {
		margin-top: 14px;
	}
	.line {
		display: grid;
		grid-template-columns: 34px 1fr;
		gap: 12px;
		padding: 12px 0;
		border-top: 1px solid var(--gl-border);
	}
	.lnum {
		display: grid;
		place-items: center;
		width: 30px;
		height: 30px;
		border-radius: 50%;
		border: 2px solid currentColor;
		font-weight: 700;
	}
	.sideb {
		min-width: 44px;
		justify-content: center;
	}
	.explain {
		list-style: none;
		margin: 8px 0 0;
		padding: 0;
		display: grid;
		gap: 5px;
	}
	.explain > li {
		display: grid;
		grid-template-columns: 96px 1fr;
		gap: 8px;
		align-items: baseline;
	}
	.explain .k {
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--gl-muted);
		font-weight: 600;
	}
	.cands {
		margin: 0;
		padding-left: 18px;
	}
</style>
