<script lang="ts">
	import type { Client, ApiError } from '../lib/api';
	import type { Me } from '../lib/types';
	import type { Content, EventSummary, Stage, VersionFull } from '../lib/studio';
	import type { Finding } from '../lib/types';
	import type { Route, VersionTab } from '../lib/route';
	import { editable, type Ctx } from '../lib/ctx';
	import type { Catalog } from '../lib/sections';
	import { actionsFor, type ActionKind } from '../lib/permissions';
	import { digest, when } from '../lib/format';
	import StatusChip from '../components/StatusChip.svelte';
	import ReasonDialog from '../components/ReasonDialog.svelte';
	import Pipeline from './Pipeline.svelte';
	import NewDraftDialog from './NewDraftDialog.svelte';
	import EventsGrid from './EventsGrid.svelte';
	import EventPage from './EventPage.svelte';
	import FieldsPage from './FieldsPage.svelte';
	import MappingPage from './MappingPage.svelte';
	import Simulator from './Simulator.svelte';
	import ImpactPage from './ImpactPage.svelte';
	import AccountsPage from './AccountsPage.svelte';
	import HistoryPage from './HistoryPage.svelte';
	import SectionEditor from './SectionEditor.svelte';
	import { specs } from '../lib/sections';

	let { api, me, route, go }: { api: Client; me: Me; route: Route; go: (r: Route) => void } = $props();

	let version = $state<VersionFull | null>(null);
	let stages = $state<Stage[]>([]);
	let events = $state<EventSummary[]>([]);
	let findings = $state<Finding[]>([]);
	let failure = $state<ApiError | null>(null);
	let tick = $state(0);
	let catalog = $state<Catalog>({ measures: [], dimensions: [] });
	let stageNote = $state<Stage | null>(null);
	let acting = $state<ActionKind | null>(null);
	let actionError = $state('');
	let busy = $state(false);
	let drafting = $state<{ then?: Route } | null>(null);
	let draftError = $state('');

	const configId = $derived(route.configId!);
	const n = $derived(route.version!);

	async function load() {
		try {
			const [v, st, ev] = await Promise.all([api.version(configId, n), api.pipeline(configId, n), api.events(configId, n)]);
			version = v;
			stages = st;
			events = ev.events;
			findings = ev.findings ?? [];
			failure = null;
			tick++;
		} catch (e) {
			failure = e as ApiError;
		}
	}

	$effect(() => {
		load();
	});
	$effect(() => {
		Promise.all([api.catalogMeasures(), api.catalogDimensions()]).then(
			([measures, dimensions]) => (catalog = { measures, dimensions }),
			() => {}
		);
	});

	const tab = $derived<VersionTab>(route.tab ?? 'events');
	const tabs: { key: VersionTab; label: string }[] = [
		{ key: 'events', label: 'Business events' },
		{ key: 'fields', label: 'Required fields' },
		{ key: 'mapping', label: 'Data Workbench mapping' },
		{ key: 'simulate', label: 'Simulation' },
		{ key: 'impact', label: 'Change impact' },
		{ key: 'accounts', label: 'Chart of accounts' },
		{ key: 'history', label: 'Findings and history' }
	];

	const stageTab: Record<string, VersionTab> = {
		configured: 'events',
		validated: 'history',
		approved: 'history',
		fields: 'fields',
		mapped: 'mapping',
		collected: 'mapping',
		simulated: 'simulate'
	};

	function pickStage(s: Stage) {
		const t = stageTab[s.stage];
		if (t) {
			stageNote = null;
			go({ configId, version: n, tab: t });
		} else {
			stageNote = stageNote?.stage === s.stage ? null : s;
		}
	}

	async function save(content: Content, reason: string) {
		if (!version) return;
		await api.saveDraft(configId, n, version.contentDigest, content, reason);
		await load();
	}

	async function act(kind: ActionKind, reason?: string) {
		if (!version) return;
		busy = true;
		actionError = '';
		try {
			if (kind === 'validate') await api.validate(configId, n);
			if (kind === 'approve') await api.approve(configId, n, reason!);
			if (kind === 'retire') await api.retire(configId, n, reason!);
			acting = null;
			await load();
		} catch (e) {
			const err = e as ApiError;
			actionError = `${err.code}: ${err.message}`;
			if (kind === 'validate') acting = null;
		} finally {
			busy = false;
		}
	}

	async function createDraft(effectiveFrom: string, reason: string, _from?: number) {
		busy = true;
		draftError = '';
		try {
			const d = await api.newDraft(configId, n, effectiveFrom, reason);
			const then = drafting?.then ?? {};
			drafting = null;
			go({ ...then, configId, version: d.version });
		} catch (e) {
			const err = e as ApiError;
			draftError = `${err.code}: ${err.message}`;
		} finally {
			busy = false;
		}
	}

	const ctx = $derived<Ctx | null>(
		version
			? {
					api,
					me,
					version,
					content: (version.content ?? {}) as Content,
					canEdit: editable(version, me),
					isEditor: me.roles.includes('CONFIG_EDITOR'),
					route,
					go,
					save,
					startDraft: (then?: Route) => {
						draftError = '';
						drafting = { then };
					},
					tick,
					catalog
				}
			: null
	);
	const actions = $derived(version ? actionsFor(version, me) : []);
</script>

{#if failure}
	<div class="notice bad" role="alert"><strong>{failure.code}</strong> {failure.message}</div>
{:else if !version || !ctx}
	<p class="muted">Loading version {n}…</p>
{:else}
	<section class="card head">
		<div class="title">
			<div>
				<div class="row">
					<h2><span class="mono">{version.configId}</span> <span class="ver">v{version.version}</span></h2>
					<StatusChip status={version.status} />
					{#if version.basedOnVersion}<span class="chip">based on v{version.basedOnVersion}</span>{/if}
				</div>
				<div class="meta muted small">
					Effective {version.effectiveFrom} → {version.effectiveTo ?? 'open'} · digest <span class="mono">{digest(version.contentDigest)}</span> · changed by {version.editors.join(
						', '
					)}
					{#if version.approvedBy}· approved by {version.approvedBy} {when(version.approvedAt)}{/if}
				</div>
			</div>
			<div class="row acts">
				{#each actions as a (a.kind)}
					<button
						class="btn"
						class:primary={a.kind !== 'retire'}
						disabled={!a.enabled || busy}
						title={a.why ?? ''}
						onclick={() => a.enabled && (a.kind === 'validate' ? act('validate') : (acting = a.kind))}>{a.label}</button
					>
				{/each}
				{#if ctx.isEditor}
					<button class="btn" onclick={() => ctx.startDraft()}>New draft from v{version.version}</button>
				{/if}
			</div>
		</div>
		{#each actions.filter((a) => !a.enabled && a.why) as a (a.kind)}
			<p class="why small">{a.label}: {a.why}</p>
		{/each}
		{#if actionError}<div class="notice bad" role="alert">{actionError}</div>{/if}
		{#if version.status === 'approved' || version.status === 'retired'}
			<p class="small muted lock">This version is {version.status} and can no longer change. To change a rule or measure, start a new draft from it.</p>
		{/if}
		<div class="flowwrap">
			<Pipeline {stages} selected={stageNote?.stage ?? ''} onselect={pickStage} />
			{#if stageNote}
				<div class="notice {stageNote.status === 'NOT_IMPLEMENTED' ? 'info' : 'warn'}">
					<strong>{stageNote.label}:</strong>
					{stageNote.detail}{#if stageNote.story}
						<span class="muted"> · backlog {stageNote.story}</span>{/if}
				</div>
			{/if}
		</div>
	</section>

	{#if route.event}
		{#key route.event}
			<EventPage {ctx} />
		{/key}
	{:else}
		<div class="tabs" role="tablist" aria-label="Version views">
			{#each tabs as t (t.key)}
				<button role="tab" aria-selected={tab === t.key} class:active={tab === t.key} onclick={() => go({ configId, version: n, tab: t.key })}>
					{t.label}
					{#if t.key === 'events'}<span class="count">{events.length}</span>{/if}
					{#if t.key === 'history' && findings.length}<span class="count">{findings.length}</span>{/if}
				</button>
			{/each}
		</div>
		{#if tab === 'events'}
			<EventsGrid {ctx} {events} />
			<div class="addev">
				<SectionEditor
					{ctx}
					spec={specs.events}
					listing={false}
					title="Add a business event"
					help="Its code, purpose, source, economic key, measures, dimensions and lists. After saving, the event opens at step 4 of its flow."
					{findings}
					onsaved={(code) => code && go({ configId, version: n, event: code })}
				/>
			</div>
		{:else if tab === 'fields'}
			<FieldsPage {ctx} />
		{:else if tab === 'mapping'}
			<MappingPage {ctx} />
		{:else if tab === 'simulate'}
			<Simulator {ctx} />
		{:else if tab === 'impact'}
			<ImpactPage {ctx} />
		{:else if tab === 'accounts'}
			<AccountsPage {ctx} {findings} />
		{:else}
			<HistoryPage {ctx} {findings} />
		{/if}
	{/if}
{/if}

{#if acting && version}
	<ReasonDialog
		title={acting === 'approve' ? `Approve ${version.configId} v${version.version}` : `Retire ${version.configId} v${version.version}`}
		verb={acting === 'approve' ? 'Approve' : 'Retire'}
		detail={acting === 'approve' ? 'Approving publishes this version from its effective date. It can never change afterwards.' : 'Retiring stops this version from applying.'}
		{busy}
		onsubmit={(r) => act(acting!, r)}
		oncancel={() => (acting = null)}
	/>
{/if}
{#if drafting && version}
	<NewDraftDialog from={version.version} {busy} error={draftError} onsubmit={createDraft} oncancel={() => (drafting = null)} />
{/if}

<style>
	.head {
		padding-bottom: 12px;
	}
	.title {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		gap: 12px;
	}
	h2 {
		margin: 0;
		font-size: 18px;
	}
	.ver {
		font-family: var(--gl-mono);
		color: var(--gl-accent);
	}
	.meta {
		margin-top: 4px;
	}
	.acts {
		align-items: flex-start;
	}
	.why {
		margin: 8px 0 0;
		color: var(--gl-warn);
	}
	.lock {
		margin: 8px 0 0;
	}
	.addev {
		margin-top: 14px;
	}
	.flowwrap {
		margin-top: 12px;
		display: grid;
		gap: 8px;
	}
</style>
