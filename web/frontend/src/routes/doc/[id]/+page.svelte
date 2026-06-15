<script lang="ts">
	import { page } from '$app/state';
	import { onMount } from 'svelte';
	import { fetchArtifact, fetchEdges, type ArtifactDetail, type Edge } from '$lib/api';
	import EdgeList from '$lib/components/edge-list.svelte';

	let id = $derived(page.params.id);
	let artifact: ArtifactDetail | null = $state(null);
	let edges: Edge[] = $state([]);
	let loading = $state(true);
	let error: string | null = $state(null);

	let kind = $derived(artifact?.labels?.find(l => l.startsWith('kind:'))?.slice(5) ?? '');
	let status = $derived(artifact?.labels?.find(l => l.includes('.') && !l.includes(':'))
		?? artifact?.labels?.find(l => l.startsWith('status:'))?.slice(7) ?? '');
	let project = $derived(artifact?.labels?.find(l => l.startsWith('project:'))?.slice(8) ?? '');

	async function load() {
		loading = true;
		error = null;
		try {
			[artifact, edges] = await Promise.all([fetchArtifact(id), fetchEdges(id)]);
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to load';
		} finally {
			loading = false;
		}
	}

	onMount(load);

	$effect(() => {
		if (id) load();
	});
</script>

{#if error}
	<div class="doc-error">{error}</div>
{:else if loading}
	<div class="doc-loading">Loading...</div>
{:else if artifact}
	<div class="doc-layout">
		<main class="doc-main">
			<h1 class="doc-title">{artifact.title}</h1>

			<div class="doc-meta">
				{#if kind}<span class="doc-badge">{kind}</span>{/if}
				{#if status}<span class="doc-badge doc-badge-status">{status.split('.').pop()}</span>{/if}
				{#if project}<span class="doc-badge doc-badge-project">{project}</span>{/if}
				<span class="doc-date">Updated {new Date(artifact.updated_at).toLocaleDateString()}</span>
			</div>

			{#each artifact.sections as section}
				<div class="doc-section">
					<h3 class="doc-section-name">{section.name.replace(/_/g, ' ')}</h3>
					<div class="doc-section-text">{@html renderMarkdown(section.text)}</div>
				</div>
			{/each}

			{#if artifact.sections.length === 0}
				<p class="doc-empty">No sections</p>
			{/if}
		</main>

		<aside class="doc-sidebar">
			<h3 class="doc-sidebar-title">Connections</h3>
			<EdgeList {edges} artifactId={id} />

			<h3 class="doc-sidebar-title">Details</h3>
			<div class="doc-detail">
				<div class="doc-detail-row"><span>ID</span><code>{artifact.id}</code></div>
				<div class="doc-detail-row"><span>Created</span><span>{new Date(artifact.created_at).toLocaleDateString()}</span></div>
				{#each artifact.labels as label}
					<span class="doc-label">{label}</span>
				{/each}
			</div>
		</aside>
	</div>
{/if}

<script lang="ts" context="module">
	function renderMarkdown(text: string): string {
		return text
			.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
			.replace(/^### (.+)$/gm, '<h4>$1</h4>')
			.replace(/^## (.+)$/gm, '<h3>$1</h3>')
			.replace(/^# (.+)$/gm, '<h2>$1</h2>')
			.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>')
			.replace(/\*(.+?)\*/g, '<em>$1</em>')
			.replace(/`(.+?)`/g, '<code>$1</code>')
			.replace(/\[\[([^\]]+)\]\]/g, '<a href="/app/doc/$1" class="wikilink">$1</a>')
			.replace(/^- (.+)$/gm, '<li>$1</li>')
			.replace(/(<li>.*<\/li>\n?)+/g, '<ul>$&</ul>')
			.replace(/\n\n/g, '<br><br>')
			.replace(/\n/g, '<br>');
	}
</script>

<style>
	.doc-layout {
		display: grid;
		grid-template-columns: 1fr 300px;
		gap: 1.5rem;
		padding: 1.5rem;
		height: 100%;
		overflow-y: auto;
	}
	.doc-main { min-width: 0; }
	.doc-title {
		font-size: 1.4rem;
		font-weight: 700;
		margin-bottom: 0.5rem;
	}
	.doc-meta {
		display: flex;
		gap: 0.4rem;
		align-items: center;
		flex-wrap: wrap;
		margin-bottom: 1.5rem;
	}
	.doc-badge {
		font-size: 0.72rem;
		padding: 0.15em 0.5em;
		border-radius: 4px;
		background: var(--border);
		color: var(--text);
	}
	.doc-badge-status { background: rgba(34,197,94,0.2); color: #4ade80; }
	.doc-badge-project { background: rgba(99,102,241,0.2); color: #a5b4fc; }
	.doc-date { font-size: 0.72rem; color: var(--text-muted); }
	.doc-section {
		margin-bottom: 1.5rem;
		border-top: 1px solid var(--border);
		padding-top: 1rem;
	}
	.doc-section-name {
		font-size: 0.85rem;
		font-weight: 600;
		color: var(--text-muted);
		margin-bottom: 0.5rem;
		text-transform: capitalize;
	}
	.doc-section-text {
		font-size: 0.9rem;
		line-height: 1.6;
	}
	.doc-section-text :global(h2) { font-size: 1.1rem; margin-top: 1rem; }
	.doc-section-text :global(h3) { font-size: 1rem; margin-top: 0.8rem; }
	.doc-section-text :global(h4) { font-size: 0.9rem; margin-top: 0.6rem; }
	.doc-section-text :global(code) {
		background: var(--border);
		padding: 0.1em 0.3em;
		border-radius: 3px;
		font-size: 0.85em;
	}
	.doc-section-text :global(ul) { padding-left: 1.2rem; }
	.doc-section-text :global(li) { margin-bottom: 0.2rem; }
	.doc-section-text :global(.wikilink) { color: var(--accent); }
	.doc-empty { color: var(--text-muted); font-size: 0.85rem; }

	.doc-sidebar {
		border-left: 1px solid var(--border);
		padding-left: 1.5rem;
		overflow-y: auto;
	}
	.doc-sidebar-title {
		font-size: 0.75rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--text-muted);
		margin-bottom: 0.5rem;
		margin-top: 1rem;
	}
	.doc-sidebar-title:first-child { margin-top: 0; }
	.doc-detail-row {
		display: flex;
		justify-content: space-between;
		font-size: 0.78rem;
		padding: 0.25rem 0;
		border-bottom: 1px solid var(--border);
	}
	.doc-detail-row span:first-child { color: var(--text-muted); }
	.doc-detail-row code { font-size: 0.7rem; color: var(--text-muted); }
	.doc-label {
		display: inline-block;
		font-size: 0.65rem;
		padding: 0.1em 0.4em;
		margin: 0.15rem 0.1rem;
		background: var(--border);
		border-radius: 3px;
		color: var(--text-muted);
	}

	.doc-loading, .doc-error {
		display: flex;
		align-items: center;
		justify-content: center;
		height: 100%;
		color: var(--text-muted);
	}
	.doc-error { color: #ef4444; }
</style>
