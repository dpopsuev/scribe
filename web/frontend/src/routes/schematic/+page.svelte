<script lang="ts">
	import { fetchScopes, type Scope } from '$lib/api';
	import SchematicCanvas from '$lib/components/graph/SchematicCanvas.svelte';
	import { computeSchematicLayout, type SchematicLayout } from '$lib/components/graph/elk-layout';
	import type { GraphNode } from '$lib/components/graph/GraphCanvas.svelte';
	import { goto } from '$app/navigation';

	interface GraphData {
		nodes: Array<{ id: string; name: string; kind: string; status: string; scope: string; val: number }>;
		links: Array<{ source: string; target: string; relation: string; weight?: number }>;
	}

	let scopes: Scope[] = $state([]);
	let scope = $state('');
	let loading = $state(true);
	let layout: SchematicLayout | null = $state(null);
	let graphData: GraphData = $state({ nodes: [], links: [] });

	async function load() {
		loading = true;
		const params = new URLSearchParams();
		if (scope) params.set('scope', scope);
		params.set('max_nodes', '200');
		const [data, sc] = await Promise.all([
			fetch(`/api/v1/graph?${params}`).then(r => r.json()),
			fetchScopes(),
		]);
		graphData = data;
		scopes = sc;

		if (data.nodes.length > 0) {
			layout = await computeSchematicLayout(data.nodes, data.links);
		} else {
			layout = null;
		}
		loading = false;
	}

	$effect(() => { void scope; load(); });

	function handleNodeClick(node: GraphNode) {
		goto(`/app/doc/${node.id}`);
	}
</script>

<div class="schematic-view">
	<div class="toolbar">
		<select bind:value={scope}>
			<option value="">All scopes</option>
			{#each scopes as s}<option value={s.scope}>{s.scope}</option>{/each}
		</select>
		<span class="count">{graphData.nodes.length} nodes, {graphData.links.length} edges</span>
	</div>

	{#if loading}
		<div class="loading">Computing layout...</div>
	{:else if layout}
		<div class="canvas-wrap">
			<SchematicCanvas {layout} onNodeClick={handleNodeClick} />
		</div>
	{:else}
		<div class="loading">No data</div>
	{/if}
</div>

<style>
	.schematic-view {
		display: flex;
		flex-direction: column;
		height: 100%;
		padding: var(--space-3);
		gap: var(--space-3);
	}
	.toolbar { display: flex; gap: var(--space-2); align-items: center; }
	select { padding: var(--space-2) var(--space-3); border: 1px solid var(--border); border-radius: var(--radius); background: var(--bg-surface); color: var(--text); font-size: 13px; }
	.count { font-size: 12px; color: var(--text-muted); margin-left: auto; }
	.loading { display: flex; align-items: center; justify-content: center; height: 200px; color: var(--text-muted); }
	.canvas-wrap { flex: 1; overflow: hidden; }
</style>
