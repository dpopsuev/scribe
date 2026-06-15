<script lang="ts">
	import type { Edge } from '$lib/api';

	let { edges, artifactId }: { edges: Edge[]; artifactId: string } = $props();

	let outgoing = $derived(edges.filter(e => e.from === artifactId));
	let incoming = $derived(edges.filter(e => e.to === artifactId));
</script>

{#if incoming.length > 0}
	<div class="edge-section">
		<h4 class="edge-header">Incoming ({incoming.length})</h4>
		{#each incoming as e}
			<a href="/app/doc/{e.from}" class="edge-item">
				<span class="edge-relation">{e.relation}</span>
				<span class="edge-title">{e.title || e.from}</span>
				<span class="edge-kind">{e.kind?.split('.').pop()}</span>
			</a>
		{/each}
	</div>
{/if}

{#if outgoing.length > 0}
	<div class="edge-section">
		<h4 class="edge-header">Outgoing ({outgoing.length})</h4>
		{#each outgoing as e}
			<a href="/app/doc/{e.to}" class="edge-item">
				<span class="edge-relation">{e.relation}</span>
				<span class="edge-title">{e.title || e.to}</span>
				<span class="edge-kind">{e.kind?.split('.').pop()}</span>
			</a>
		{/each}
	</div>
{/if}

{#if edges.length === 0}
	<p class="edge-empty">No connections</p>
{/if}

<style>
	.edge-section { margin-bottom: 1rem; }
	.edge-header {
		font-size: 0.72rem;
		text-transform: uppercase;
		letter-spacing: 0.05em;
		color: var(--text-muted);
		margin-bottom: 0.4rem;
	}
	.edge-item {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		padding: 0.35rem 0;
		text-decoration: none;
		color: var(--text);
		font-size: 0.82rem;
		border-bottom: 1px solid var(--border);
	}
	.edge-item:hover { color: var(--accent); }
	.edge-relation {
		font-size: 0.7rem;
		color: var(--text-muted);
		min-width: 80px;
	}
	.edge-title { flex: 1; }
	.edge-kind {
		font-size: 0.7rem;
		color: var(--text-muted);
	}
	.edge-empty {
		font-size: 0.8rem;
		color: var(--text-muted);
	}
</style>
