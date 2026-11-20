<script lang="ts">
	// A form for one configuration item, generated from its section's field spec. It edits
	// the item in place; the section editor checks and saves it.
	import ItemForm from './ItemForm.svelte';
	import type { Content } from '../lib/studio';
	import { getPath, setPath, type Catalog, type Field } from '../lib/sections';

	let {
		fields,
		item = $bindable(),
		content,
		catalog,
		editing = false,
		compact = false,
		idp = 'f'
	}: { fields: Field[]; item: any; content: Content; catalog: Catalog; editing?: boolean; compact?: boolean; idp?: string } = $props();

	let adding = $state<Record<string, string>>({});

	const val = (f: Field) => getPath(item, f.key);
	const set = (f: Field, v: any) => setPath(item, f.key, v);
	const opts = (f: Field) => f.options?.(content, catalog, item) ?? [];
	const arr = (f: Field): string[] => (Array.isArray(val(f)) ? val(f) : []);

	function addTo(f: Field) {
		const v = (adding[f.key] ?? '').trim().toUpperCase();
		if (!v || arr(f).includes(v)) return;
		set(f, [...arr(f), v]);
		adding = { ...adding, [f.key]: '' };
	}
	function pairs(f: Field): [string, string][] {
		return Object.entries((val(f) ?? {}) as Record<string, string>);
	}
	function setPairs(f: Field, ps: [string, string][]) {
		set(f, Object.fromEntries(ps));
	}
	function rows(f: Field): any[] {
		return Array.isArray(val(f)) ? val(f) : [];
	}
</script>

<div class="form" class:compact>
	{#each fields as f (f.key)}
		<div class="f" class:wide={f.wide && !compact} class:full={f.type === 'rows' || f.type === 'pairs'}>
			{#if f.type === 'bool'}
				<label class="check"><input type="checkbox" checked={!!val(f)} onchange={(e) => set(f, (e.currentTarget as HTMLInputElement).checked)} /> {f.label}</label>
			{:else}
				<label class="lbl" for="{idp}-{f.key}">{f.label}{f.required ? ' *' : ''}</label>
				{#if f.type === 'select'}
					<select id="{idp}-{f.key}" class="field" value={val(f) ?? ''} onchange={(e) => set(f, (e.currentTarget as HTMLSelectElement).value)} disabled={editing && f.fixedOnEdit}>
						{#if !opts(f).some((o) => o.value === '')}<option value="">{f.required ? 'choose…' : '—'}</option>{/if}
						{#each opts(f) as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
						{#if val(f) && !opts(f).some((o) => o.value === val(f))}<option value={val(f)}>{val(f)} (not defined)</option>{/if}
					</select>
				{:else if f.type === 'multi'}
					<div class="multi">
						{#each arr(f) as v (v)}
							<span class="chip mono">{v}<button class="x" aria-label="Remove {v}" onclick={() => set(f, arr(f).filter((x) => x !== v))}>×</button></span>
						{/each}
						<input
							id="{idp}-{f.key}"
							class="field mono add"
							list="{idp}-{f.key}-list"
							value={adding[f.key] ?? ''}
							oninput={(e) => (adding = { ...adding, [f.key]: (e.currentTarget as HTMLInputElement).value })}
							onchange={() => addTo(f)}
							onkeydown={(e) => e.key === 'Enter' && (e.preventDefault(), addTo(f))}
							placeholder="add…"
						/>
						<datalist id="{idp}-{f.key}-list">
							{#each opts(f).filter((o) => !arr(f).includes(o.value)) as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
						</datalist>
					</div>
				{:else if f.type === 'strings'}
					<input
						id="{idp}-{f.key}"
						class="field mono"
						value={arr(f).join(', ')}
						onchange={(e) =>
							set(
								f,
								(e.currentTarget as HTMLInputElement).value
									.split(',')
									.map((x) => x.trim())
									.filter(Boolean)
							)}
						placeholder={f.placeholder ?? 'comma-separated'}
					/>
				{:else if f.type === 'pairs'}
					<div class="pairs">
						{#each pairs(f) as [k, v], i (i)}
							<div class="pair">
								<input
									class="field mono"
									list="{idp}-{f.key}-keys"
									value={k}
									aria-label="{f.label} key {i + 1}"
									onchange={(e) => setPairs(f, pairs(f).map(([a, b], j) => (j === i ? [(e.currentTarget as HTMLInputElement).value.trim(), b] : [a, b])))}
								/>
								<span class="muted">=</span>
								<input
									class="field mono"
									value={v}
									aria-label="{f.label} value {i + 1}"
									onchange={(e) => setPairs(f, pairs(f).map(([a, b], j) => (j === i ? [a, (e.currentTarget as HTMLInputElement).value] : [a, b])))}
								/>
								<button class="btn link" onclick={() => setPairs(f, pairs(f).filter((_, j) => j !== i))}>Remove</button>
							</div>
						{/each}
						<datalist id="{idp}-{f.key}-keys">
							{#each f.pairKeys?.(content, catalog, item) ?? [] as o (o.value)}<option value={o.value}>{o.label}</option>{/each}
						</datalist>
						<button class="btn" onclick={() => setPairs(f, [...pairs(f).filter(([a]) => a !== ''), ['', '']])}>Add</button>
					</div>
				{:else if f.type === 'rows'}
					<div class="rows">
						{#each rows(f) as row, i (i)}
							<div class="rowbox">
								<ItemForm fields={f.fields ?? []} item={row} {content} {catalog} compact idp="{idp}-{f.key}-{i}" />
								<button class="btn link" onclick={() => set(f, rows(f).filter((_, j) => j !== i))}>Remove</button>
							</div>
						{/each}
						<button class="btn" onclick={() => set(f, [...rows(f), {}])}>Add {f.label.toLowerCase().replace(/s? \(.*$/, '').replace(/s$/, '')}</button>
					</div>
				{:else}
					<input
						id="{idp}-{f.key}"
						class="field"
						class:mono={f.type === 'code' || f.type === 'formula'}
						type={f.type === 'number' ? 'number' : f.type === 'date' ? 'date' : 'text'}
						value={val(f) ?? ''}
						disabled={editing && f.fixedOnEdit}
						placeholder={f.placeholder ?? ''}
						oninput={(e) => {
							const t = (e.currentTarget as HTMLInputElement).value;
							set(f, f.type === 'code' ? t.toUpperCase() : t);
						}}
					/>
				{/if}
			{/if}
			{#if f.help && !compact}<span class="help">{f.help}</span>{/if}
		</div>
	{/each}
</div>

<style>
	.form {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(230px, 1fr));
		gap: 10px 12px;
	}
	.form.compact {
		grid-template-columns: repeat(auto-fill, minmax(150px, 1fr));
		gap: 6px 8px;
	}
	.f {
		display: grid;
		gap: 3px;
		align-content: start;
		min-width: 0;
	}
	.f.wide {
		grid-column: span 2;
	}
	.f.full {
		grid-column: 1 / -1;
	}
	@media (max-width: 640px) {
		.f.wide {
			grid-column: auto;
		}
	}
	.check {
		display: flex;
		gap: 6px;
		align-items: center;
		font-size: 13px;
		margin-top: 18px;
	}
	.compact .check {
		margin-top: 16px;
	}
	.help {
		font-size: 11.5px;
		color: var(--gl-muted);
	}
	.multi {
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
		align-items: center;
		padding: 3px;
		border: 1px solid var(--gl-border-strong);
		border-radius: 6px;
		background: var(--gl-surface);
		min-height: 32px;
	}
	.multi .add {
		border: 0;
		flex: 1;
		min-width: 90px;
		padding: 3px 4px;
	}
	.x {
		border: 0;
		background: none;
		color: var(--gl-muted);
		cursor: pointer;
		padding: 0 0 0 2px;
		font-size: 14px;
		line-height: 1;
	}
	.pairs,
	.rows {
		display: grid;
		gap: 6px;
		justify-items: start;
	}
	.pair {
		display: flex;
		gap: 6px;
		align-items: center;
	}
	.rowbox {
		width: 100%;
		box-sizing: border-box;
		display: grid;
		grid-template-columns: 1fr auto;
		gap: 8px;
		align-items: end;
		padding: 8px;
		border: 1px dashed var(--gl-border-strong);
		border-radius: 8px;
	}
</style>
