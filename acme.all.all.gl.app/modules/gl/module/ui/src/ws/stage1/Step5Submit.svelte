<script lang="ts">
	// Step 5 — submit for independent review. The analyst submits (the server validates); a
	// reviewer who did not edit the version approves it or returns it to draft with a reason. An
	// approved version never changes; the next change is a new draft.
	import type { S1 } from './ctx';
	import type { Finding } from '../../lib/types';
	import { accounts, classes, coverage, rules, sourceClasses } from './model';
	import ReasonDialog from '../../components/ReasonDialog.svelte';
	import AIPanel from '../AIPanel.svelte';

	let { ctx }: { ctx: S1 } = $props();

	let findings = $state<Finding[] | null>(null);
	let busy = $state(false);
	let error = $state<string>('');
	let blocking = $state<Finding[]>([]);
	let asking = $state<'' | 'approve' | 'return'>('');
	let done = $state('');

	$effect(() => {
		ctx.api.checkContent(ctx.version.configId, ctx.version.version, ctx.content).then((r) => (findings = r.findings), () => {});
	});

	const c = $derived(ctx.content);
	const v = $derived(ctx.version);
	const errs = $derived((findings ?? []).filter((f) => f.severity === 'error'));
	const srcs = $derived(sourceClasses(c));
	const unmapped = $derived(srcs.filter((s) => s.status !== 'NOT_USED' && !(s.mappings ?? []).length));
	const cov = $derived(coverage(c));
	const missingRows = $derived(cov.reduce((n, x) => n + x.missing.length, 0));
	const unapproved = $derived(classes(c).filter((k) => k.standard && ctx.library.classes.find((x) => x.code === k.standard)?.status !== 'APPROVED'));
	const editedByMe = $derived(v.editors.includes(ctx.me.actor));
	const checks = $derived([
		{ ok: accounts(c).length > 0, text: `${accounts(c).length} accounts in the chart` },
		{ ok: srcs.length === 0 || unmapped.length === 0, text: unmapped.length ? `${unmapped.length} source classes still to map` : 'every source class mapped or marked not used' },
		{ ok: rules(c).length > 0 && classes(c).every((k) => rules(c).some((r) => r.class === k.code)), text: `${rules(c).length} assignment rules; every class has one` },
		{ ok: missingRows === 0, text: missingRows ? `${missingRows} source rows unaccounted for` : 'every source row accounted for' },
		{ ok: unapproved.length === 0, text: unapproved.length ? `${unapproved.length} standard classes awaiting library approval` : 'standard classes approved' },
		{ ok: findings !== null && errs.length === 0, text: findings === null ? 'checking…' : errs.length ? `${errs.length} blocking findings` : 'no blocking finding' }
	]);
	const ready = $derived(checks.every((x) => x.ok));

	async function submit() {
		busy = true;
		error = '';
		blocking = [];
		try {
			await ctx.api.validate(v.configId, v.version);
			done = `v${v.version} submitted: it is validated and waits for an independent reviewer.`;
			await ctx.reload();
		} catch (e) {
			const x = e as { code?: string; message: string; detail?: { findings?: Finding[] } };
			error = `${x.code ?? ''} ${x.message}`;
			blocking = (x.detail?.findings ?? []).filter((f) => f.severity === 'error');
		} finally {
			busy = false;
		}
	}
	async function decide(reason: string) {
		busy = true;
		error = '';
		try {
			if (asking === 'approve') {
				await ctx.api.approve(v.configId, v.version, reason);
				done = `v${v.version} approved. It can no longer change; Stage 2 can build on it.`;
			} else {
				await ctx.api.returnToDraft(v.configId, v.version, reason);
				done = `v${v.version} returned to draft: “${reason}”.`;
			}
			asking = '';
			await ctx.reload();
		} catch (e) {
			error = (e as Error).message;
			asking = '';
		} finally {
			busy = false;
		}
	}
</script>

<div class="with-ai">
	<div class="stack">
		{#if error}<p class="notice bad" role="alert">{error}</p>{/if}
		{#if blocking.length}<div class="notice bad small">{#each blocking.slice(0, 12) as f, i (i)}<div><span class="mono">{f.path}</span> {f.message}</div>{/each}</div>{/if}
		{#if done}<p class="notice ok" role="status">{done}</p>{/if}

		<section class="card" aria-label="Readiness">
			<h2 class="sec">Ready for review?</h2>
			<ul class="checks">{#each checks as x (x.text)}<li class:ok={x.ok}><span>{x.ok ? '✓' : '•'}</span> {x.text}</li>{/each}</ul>
			<p class="small muted">The server runs the same validation when you submit; nothing here can be skipped.</p>
		</section>

		<section class="card" aria-label="Summary">
			<h2 class="sec">Summary</h2>
			<p>
				{v.configId} v{v.version} holds <b>{accounts(c).length}</b> accounts, <b>{classes(c).length}</b> classes ({classes(c).filter((k) => k.standard).length} standard, {classes(c).filter((k) => k.custom).length} custom),
				<b>{srcs.length}</b> source classes ({srcs.length - unmapped.length} mapped) and <b>{rules(c).length}</b> assignment rules, from {cov.length} imported sheet{cov.length === 1 ? '' : 's'}.
				Edited by {v.editors.join(', ')}.
			</p>
		</section>

		<section class="card" aria-label="Submit">
			{#if v.status === 'draft'}
				<h2 class="sec">Submit for review</h2>
				{#if ctx.isEditor}
					<p class="small muted">Submitting validates the draft. A reviewer who did not edit it then approves it or returns it to you.</p>
					<button class="btn primary" disabled={busy || !ctx.canEdit} onclick={submit}>{busy ? 'Submitting…' : 'Submit for review'}</button>
					{#if !ready}<span class="small muted"> Some checks are open; the server decides what blocks.</span>{/if}
				{:else}<p class="muted">A business analyst submits the draft.</p>{/if}
			{:else if v.status === 'validated'}
				<h2 class="sec">Independent review</h2>
				{#if ctx.isApprover && !editedByMe}
					<p class="small muted">Approve it, or return it to the analysts with what to change.</p>
					<div class="row">
						<button class="btn primary" disabled={busy} onclick={() => (asking = 'approve')}>Approve…</button>
						<button class="btn" disabled={busy} onclick={() => (asking = 'return')}>Return to draft…</button>
					</div>
				{:else if editedByMe}<p class="muted">You edited this version, so someone else must review it.</p>
				{:else}<p class="muted">Waiting for a reviewer.</p>{/if}
			{:else if v.status === 'approved'}
				<h2 class="sec">Approved</h2>
				<p>Approved by {v.approvedBy}. It can no longer change; a change starts a new draft.</p>
			{/if}
		</section>
	</div>
	<AIPanel
		api={ctx.api}
		area="stage1"
		step="submit"
		configId={v.configId}
		version={v.version}
		canAct={false}
		examples={['Summarize Stage 1 and tell me whether it is ready for approval.', 'What changed since the last approved version?']}
	/>
</div>

{#if asking}
	<ReasonDialog
		title={asking === 'approve' ? `Approve ${v.configId} v${v.version}` : `Return ${v.configId} v${v.version} to draft`}
		verb={asking === 'approve' ? 'Approve' : 'Return to draft'}
		detail={asking === 'approve' ? 'Once approved, this version never changes.' : 'Say what the analysts should change.'}
		{busy}
		onsubmit={decide}
		oncancel={() => (asking = '')}
	/>
{/if}

<style>
	.checks {
		list-style: none;
		padding: 0;
		margin: 0 0 8px;
		display: grid;
		gap: 4px;
	}
	.checks li {
		color: var(--warn);
	}
	.checks li.ok {
		color: var(--ok);
	}
	.checks span {
		display: inline-block;
		width: 16px;
		font-weight: 700;
	}
</style>
