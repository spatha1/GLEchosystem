<script lang="ts">
	// Lists one configuration section's items and, in a draft, adds, edits and removes them.
	// Every edit is applied to a copy of the stored content, checked by the server as you
	// type (the same validation that gates approval), and saved to the draft with a reason.
	import type { ApiError } from '../lib/api';
	import type { Ctx } from '../lib/ctx';
	import type { Finding } from '../lib/types';
	import type { ItemChange } from '../lib/studio';
	import { clone, under } from '../lib/content';
	import { itemPath, withItem, type SectionSpec } from '../lib/sections';
	import ItemForm from './ItemForm.svelte';
	import Findings from '../components/Findings.svelte';
	import ReasonDialog from '../components/ReasonDialog.svelte';

	let {
		ctx,
		spec,
		filter,
		fixed = {},
		title,
		help,
		findings = [],
		listing = true,
		canAdd = true,
		onsaved
	}: {
		ctx: Ctx;
		spec: SectionSpec;
		filter?: (item: any) => boolean;
		fixed?: Record<string, any>;
		title?: string;
		help?: string;
		findings?: Finding[];
		listing?: boolean;
		canAdd?: boolean;
		onsaved?: (key: string | null) => void;
	} = $props();

	const all = $derived(((ctx.content[spec.section] ?? []) as any[]).map((x, i) => ({ x, i })));
	const items = $derived(filter ? all.filter(({ x }) => filter(x)) : all);

	let editing = $state<{ original?: string; item: any } | null>(null);
	let check = $state<Finding[]>([]);
	let changes = $state<ItemChange[]>([]);
	let checking = $state(false);
	let pending = $state<{ kind: 'save' | 'remove'; key?: string } | null>(null);
	let busy = $state(false);
	let error = $state('');
	let timer: ReturnType<typeof setTimeout> | undefined;

	function add() {
		error = '';
		editing = { item: { ...spec.blank(ctx.content), ...clone(fixed) } };
	}
	function edit(x: any) {
		error = '';
		const item = clone(x);
		editing = { original: spec.key(x), item: spec.load ? spec.load(ctx.content, item) : item };
	}

	const candidate = $derived(editing ? withItem(ctx.content, spec, editing.item, editing.original) : null);

	$effect(() => {
		const c = candidate;
		if (!c || !editing) return;
		const path = itemPath(c, spec, spec.key(editing.item));
		clearTimeout(timer);
		checking = true;
		timer = setTimeout(() => {
			ctx.api.check(ctx.version.configId, ctx.version.version, c).then(
				(r) => {
					check = under(r.findings, path);
					changes = r.changes;
					checking = false;
				},
				(e: ApiError) => {
					check = [{ path, code: e.code, severity: 'error', message: e.message }];
					checking = false;
				}
			);
		}, 350);
	});

	async function confirm(reason: string) {
		if (!pending) return;
		busy = true;
		error = '';
		try {
			if (pending.kind === 'save' && candidate && editing) {
				await ctx.save(candidate, reason);
				const key = spec.key(editing.item);
				editing = null;
				onsaved?.(key);
			} else if (pending.kind === 'remove' && pending.key) {
				await ctx.save(withItem(ctx.content, spec, null, pending.key), reason);
				onsaved?.(null);
			}
			pending = null;
		} catch (e) {
			const err = e as ApiError;
			error = err.code === 'STALE_VERSION' ? 'Someone else saved this draft meanwhile. Reload and reapply your change.' : `${err.code}: ${err.message}`;
			pending = null;
		} finally {
			busy = false;
		}
	}

	const errors = $derived(check.filter((f) => f.severity === 'error').length);
</script>

<section class="card sec">
	<div class="row head">
		<div class="grow">
			<h3>{title ?? spec.title}{#if listing}&nbsp;<span class="muted">({items.length})</span>{/if}</h3>
			{#if help ?? spec.help}<p class="small muted">{help ?? spec.help}</p>{/if}
		</div>
		{#if ctx.canEdit && canAdd}
			<button class="btn" onclick={add} disabled={!!editing}>+ Add {spec.noun}</button>
		{/if}
	</div>

	{#if !listing}
		<!-- add-only: the items are listed elsewhere -->
	{:else if items.length}
		<div class="scroll">
			<table class="data">
				<thead>
					<tr>
						{#each spec.columns as col (col.label)}<th>{col.label}</th>{/each}
						<th></th>
					</tr>
				</thead>
				<tbody>
					{#each items as { x, i } (spec.key(x))}
						{@const fs = under(findings, `${spec.section}/${i}`)}
						<tr class:editing={editing?.original === spec.key(x)}>
							{#each spec.columns as col (col.label)}<td class:mono={col.mono}>{col.value(x, ctx.content) || '—'}</td>{/each}
							<td class="acts">
								{#if fs.some((f) => f.severity === 'error')}<span class="badge bad">{fs.filter((f) => f.severity === 'error').length} error(s)</span>{/if}
								{#if ctx.canEdit}
									<button class="btn" onclick={() => edit(x)} disabled={!!editing}>Edit</button>
									<button class="btn danger" onclick={() => ((error = ''), (pending = { kind: 'remove', key: spec.key(x) }))} disabled={!!editing}>Remove</button>
								{/if}
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{:else}
		<p class="small muted none">None yet.</p>
	{/if}

	{#if !ctx.canEdit && ctx.isEditor}
		<p class="small muted lock">
			This version is {ctx.version.status}: to change it, <button class="btn link" onclick={() => ctx.startDraft({ ...ctx.route })}>start a new draft from v{ctx.version.version}</button>.
		</p>
	{/if}

	{#if editing}
		<div class="editor">
			<div class="row">
				<b class="grow">{editing.original ? `Edit ${spec.noun} ${editing.original}` : `New ${spec.noun}`}</b>
				<span class="small muted">{checking ? 'Checking…' : errors ? `${errors} problem${errors === 1 ? '' : 's'}` : 'Checks out'}</span>
			</div>
			<ItemForm fields={spec.fields} bind:item={editing.item} content={ctx.content} catalog={ctx.catalog} editing={!!editing.original} idp={spec.section} />
			{#if check.length}<Findings findings={check} />{/if}
			{#if changes.length}
				<p class="small muted">Will change: {changes.map((c) => `${c.change.toLowerCase()} ${c.section} ${c.key}`).join('; ')}</p>
			{/if}
			<div class="row">
				<span class="spacer"></span>
				<button class="btn" onclick={() => (editing = null)}>Cancel</button>
				<button class="btn primary" onclick={() => (pending = { kind: 'save' })} disabled={busy || !spec.key(editing.item)}>Save to draft v{ctx.version.version}</button>
			</div>
		</div>
	{/if}
	{#if error}<div class="notice bad" role="alert">{error}</div>{/if}
</section>

{#if pending}
	<ReasonDialog
		title={pending.kind === 'save' ? `Save ${spec.noun} ${editing ? spec.key(editing.item) : ''}` : `Remove ${spec.noun} ${pending.key}`}
		verb={pending.kind === 'save' ? 'Save' : 'Remove'}
		detail={pending.kind === 'save' && errors
			? `It still has ${errors} problem(s): the draft can be saved, but not validated until they are fixed.`
			: 'Saved to the draft with your reason; it applies only after validation and approval by someone else.'}
		{busy}
		onsubmit={confirm}
		oncancel={() => (pending = null)}
	/>
{/if}

<style>
	.sec + :global(.sec) {
		margin-top: 14px;
	}
	.head {
		align-items: flex-start;
	}
	.grow {
		flex: 1;
		min-width: 0;
	}
	.grow p {
		margin: 0 0 8px;
	}
	.acts {
		text-align: right;
		white-space: nowrap;
	}
	.acts .btn + .btn {
		margin-left: 4px;
	}
	tr.editing td {
		background: var(--gl-accent-soft);
	}
	.none,
	.lock {
		margin: 6px 0 0;
	}
	.editor {
		display: grid;
		gap: 10px;
		margin-top: 12px;
		padding: 12px;
		border-radius: 8px;
		border: 1px solid var(--gl-accent);
		background: var(--gl-surface-2);
	}
</style>
