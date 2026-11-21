<script lang="ts">
	import type { Client, ApiError, Origin } from '../lib/api';
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
	import LifecyclePanel from './LifecyclePanel.svelte';
	import ImpactPage from './ImpactPage.svelte';
	import AccountsPage from './AccountsPage.svelte';
	import HistoryPage from './HistoryPage.svelte';
	import SectionEditor from './SectionEditor.svelte';
	import AIWizard from './AIWizard.svelte';
	import { specs } from '../lib/sections';
	import { viewOf } from '../lib/views';

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
		Promise.all([api.catalogMeasures(), api.catalogDimensions(), api.catalogEvents()]).then(
			([m, dimensions, events]) =>
				(catalog = {
					measures: m.measures.map((x) => ({ value: x.code, label: `${x.code} · ${x.name} (${x.category.toLowerCase()})` })),
					measureDefs: m.measures,
					dimensions,
					events
				}),
			() => {}
		);
	});

	const tab = $derived<VersionTab>(route.tab ?? 'events');
	const view = $derived(viewOf(tab));

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

	async function save(content: Content, reason: string, origin?: Origin) {
		if (!version) return;
		await api.saveDraft(configId, n, version.contentDigest, content, reason, origin);
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
	{#snippet versionCard()}
		<section class="card vcard" aria-label="Version {version!.configId} v{version!.version}">
			<header class="vhead">
				<div class="vtitle">
					<div class="row">
						<h2><span class="mono">{version!.configId}</span> <span class="ver">v{version!.version}</span></h2>
						<StatusChip status={version!.status} />
						{#if version!.basedOnVersion}<span class="chip">based on v{version!.basedOnVersion}</span>{/if}
					</div>
					<div class="meta">
						Effective {version!.effectiveFrom} → {version!.effectiveTo ?? 'open'} · digest <span class="mono">{digest(version!.contentDigest)}</span> · changed by {version!.editors.join(
							', '
						)}
						{#if version!.approvedBy}· approved by {version!.approvedBy} {when(version!.approvedAt)}{/if}
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
					{#if ctx!.isEditor}
						<button class="btn" onclick={() => ctx!.startDraft()}>New draft from v{version!.version}</button>
					{/if}
				</div>
			</header>
			<div class="vbody">
				{#each actions.filter((a) => !a.enabled && a.why) as a (a.kind)}
					<p class="why small">{a.label}: {a.why}</p>
				{/each}
				{#if actionError}<div class="notice bad" role="alert">{actionError}</div>{/if}
				{#if version!.status === 'approved' || version!.status === 'retired'}
					<p class="small muted lock">This version is {version!.status} and can no longer change. To change a rule or measure, start a new draft from it.</p>
				{/if}
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
	{/snippet}

	{#if route.event}
		{#key route.event}
			<EventPage {ctx} {versionCard} />
		{/key}
	{:else}
		<div class="pagehead">
			<div class="titles">
				<span class="eyebrow">{version.configId} · Version {version.version}</span>
				<h1>
					{view.label}
					{#if tab === 'events'}<span class="badge off">{events.length}</span>{/if}
					{#if tab === 'history' && findings.length}<span class="badge warn">{findings.length} finding{findings.length === 1 ? '' : 's'}</span>{/if}
				</h1>
				<p class="purpose">{view.purpose}</p>
			</div>
		</div>
		{@render versionCard()}
		<div class="viewbody">
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
			{:else if tab === 'ai'}
				<AIWizard {ctx} />
			{:else if tab === 'fields'}
				<FieldsPage {ctx} />
			{:else if tab === 'mapping'}
				<MappingPage {ctx} />
			{:else if tab === 'simulate'}
				<Simulator {ctx} />
				<div class="life-gap"><LifecyclePanel {ctx} /></div>
			{:else if tab === 'impact'}
				<ImpactPage {ctx} />
			{:else if tab === 'accounts'}
				<AccountsPage {ctx} {findings} />
			{:else}
				<HistoryPage {ctx} {findings} />
			{/if}
		</div>
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
	.life-gap {
		margin-top: 14px;
	}
	.vcard {
		padding: 0;
	}
	.vhead {
		display: flex;
		flex-wrap: wrap;
		justify-content: space-between;
		align-items: flex-start;
		gap: 12px;
		padding: 16px 20px 14px;
		border-bottom: 1px solid var(--gl-border);
	}
	.vtitle {
		min-width: 0;
	}
	h2 {
		margin: 0;
		font-size: 16px;
		font-weight: 600;
	}
	.ver {
		font-family: var(--gl-mono);
		color: var(--gl-accent);
	}
	.meta {
		margin-top: 4px;
		font-size: 12px;
		color: var(--gl-muted);
	}
	.acts {
		align-items: flex-start;
	}
	.vbody {
		display: grid;
		gap: 10px;
		padding: 14px 20px 16px;
	}
	.why,
	.lock {
		margin: 0;
	}
	.why {
		color: var(--gl-warn);
	}
	h1 .badge {
		vertical-align: middle;
		margin-left: 4px;
	}
	.viewbody {
		margin-top: 16px;
	}
	.addev {
		margin-top: 16px;
	}
</style>
