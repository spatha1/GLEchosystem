<script lang="ts">
	// A client's workspace: who it is, its configuration and versions, and the stages: Stage 1
	// (chart of accounts) now, Stage 2 (events and posting rules) next.
	import type { WsClient, ClientProfile } from './api';
	import type { WsRoute } from './route';
	import type { Me, ConfigSummary } from '../lib/types';
	import type { Content, VersionFull } from '../lib/studio';
	import { accounts, classes, rules, sourceClasses } from './stage1/model';

	let { api, me, clientLabel, go, studioUrl }: { api: WsClient; me: Me; clientLabel: string; go: (r: WsRoute) => void; studioUrl: string } = $props();

	let profile = $state<ClientProfile | null>(null);
	let configs = $state<ConfigSummary[]>([]);
	let versions = $state<VersionFull[]>([]);
	let latest = $state<VersionFull | null>(null);
	let loaded = $state(false);

	$effect(() => {
		(async () => {
			profile = await api.getClient().catch(() => null);
			configs = await api.configs().catch(() => []);
			const id = profile?.configId && configs.some((c) => c.configId === profile!.configId) ? profile.configId : configs[0]?.configId;
			if (id) {
				versions = await api.versions(id);
				latest = await api.version(id, versions.at(-1)!.version);
			}
			loaded = true;
		})();
	});

	const c = $derived((latest?.content ?? {}) as Content);
	const srcs = $derived(sourceClasses(c));
	const mapped = $derived(srcs.filter((s) => s.status === 'NOT_USED' || (s.mappings ?? []).length).length);
	const stage1 = $derived(
		!latest ? 'Not started' : latest.status === 'approved' ? 'Approved' : latest.status === 'validated' ? 'Submitted for review' : accounts(c).length ? 'In progress' : 'Not started'
	);
	const statusLabel: Record<string, string> = { draft: 'Draft', validated: 'Submitted', approved: 'Approved', retired: 'Retired' };
</script>

<h1 class="ws-h1">{profile?.name ?? clientLabel}</h1>
<p class="ws-lead">
	{#if profile}{profile.profile.description ?? 'Client workspace'}{:else}This client has not been onboarded in GL yet{me.roles.includes('GL_ADMIN') ? ': start with Client onboarding.' : '; an administrator onboards it.'}{/if}
</p>

{#if !loaded}
	<p class="muted">Loading…</p>
{:else}
	<div class="kpis top">
		<div class="kpi"><b>{profile ? (profile.status === 'ACTIVE' ? 'Active' : 'Onboarding') : '—'}</b><span>client status</span></div>
		<div class="kpi"><b class="mono">{latest?.configId ?? '—'}</b><span>configuration</span></div>
		<div class="kpi"><b>{latest ? `v${latest.version}` : '—'}</b><span>{latest ? statusLabel[latest.status] : 'no version'}</span></div>
		<div class="kpi"><b>{(profile?.profile.sourceSystems ?? []).join(', ') || '—'}</b><span>source systems</span></div>
	</div>

	<div class="stages">
		<section class="card stage">
			<div class="row"><span class="num">1</span><h2 class="sec grow">Chart of accounts</h2><span class="pill {stage1 === 'Approved' ? 'ok' : stage1 === 'Not started' ? '' : 'info'}">{stage1}</span></div>
			<p class="small muted">The client’s accounts, its accounting classes mapped to the standard library, and the rules choosing an account for each class.</p>
			<ul class="facts small">
				<li><b>{accounts(c).length}</b> accounts</li>
				<li><b>{mapped}/{srcs.length}</b> source classes mapped</li>
				<li><b>{classes(c).length}</b> classes</li>
				<li><b>{rules(c).length}</b> assignment rules</li>
			</ul>
			<button class="btn primary" disabled={!latest} onclick={() => go({ page: 'stage1', client: api.tenant, step: 1 })}>Open Stage 1</button>
		</section>
		<section class="card stage soon">
			<div class="row"><span class="num">2</span><h2 class="sec grow">Business events and posting rules</h2><span class="pill">Next</span></div>
			<p class="small muted">Events from the client’s documents, their measures, and the debit and credit lines, using the classes of Stage 1. Not in this release.</p>
		</section>
	</div>

	{#if versions.length}
		<section class="card">
			<h2 class="sec">Versions of {latest?.configId}</h2>
			<div class="tw">
				<table class="grid">
					<thead><tr><th>Version</th><th>Status</th><th>Applies from</th><th>Edited by</th><th>Approved by</th></tr></thead>
					<tbody>
						{#each [...versions].reverse() as v (v.version)}<tr><td>v{v.version}</td><td><span class="pill {v.status === 'approved' ? 'ok' : v.status === 'draft' ? 'warn' : ''}">{statusLabel[v.status]}</span></td><td>{v.effectiveFrom}</td><td class="small">{v.editors.join(', ')}</td><td class="small">{v.approvedBy ?? ''}</td></tr>{/each}
					</tbody>
				</table>
			</div>
			<p class="small muted">The classic studio still shows every detail: <a href={studioUrl}>open it</a>.</p>
		</section>
	{/if}
{/if}

<style>
	.top {
		margin-bottom: 18px;
	}
	.stages {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(300px, 1fr));
		gap: 16px;
		margin-bottom: 18px;
	}
	.stage {
		display: grid;
		gap: 10px;
		align-content: start;
	}
	.stage.soon {
		opacity: 0.7;
	}
	.num {
		width: 28px;
		height: 28px;
		border-radius: 8px;
		display: grid;
		place-items: center;
		background: var(--accent);
		color: #fff;
		font-weight: 700;
	}
	.soon .num {
		background: var(--faint);
	}
	.grow {
		flex: 1;
	}
	.sec {
		margin: 0;
	}
	.facts {
		list-style: none;
		padding: 0;
		margin: 0;
		display: grid;
		grid-template-columns: 1fr 1fr;
		gap: 4px;
	}
</style>
