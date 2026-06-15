<script lang="ts">
	import { fetchArtifacts, fetchScopes, type Artifact } from '$lib/api';
	import { statusesFor, statusLabel } from '$lib/statuses';
	import BoardColumn from '$lib/components/board-column.svelte';
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';

	let scopes: string[] = $state([]);
	let selectedScope = $state('');
	let selectedKindPrefix = $state('effort');
	let columns: Record<string, Artifact[]> = $state({});
	let loading = $state(true);
	let error: string | null = $state(null);

	async function loadBoard() {
		loading = true;
		error = null;
		try {
			const params: Record<string, string> = { kind_prefix: selectedKindPrefix };
			if (selectedScope) params.scope = selectedScope;

			const arts = await fetchArtifacts(params);
			const cols: Record<string, Artifact[]> = {};
			for (const s of statusesFor(selectedKindPrefix)) cols[s] = [];
			for (const a of arts) if (cols[a.status]) cols[a.status].push(a);
			columns = cols;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Unknown error';
		} finally {
			loading = false;
		}
	}

	function handleColumnUpdate(status: string, items: Artifact[]) {
		columns[status] = items;
	}

	function selectScope(scope: string) {
		selectedScope = scope;
		const url = new URL(page.url);
		if (scope) url.searchParams.set('scope', scope);
		else url.searchParams.delete('scope');
		goto(url.toString(), { replaceState: true });
		loadBoard();
	}

	let totalCount = $derived(Object.values(columns).flat().length);

	onMount(async () => {
		try {
			const scopeData = await fetchScopes();
			scopes = scopeData.map(s => s.scope).filter(s => s && s !== '_schema');
		} catch { /* scopes dropdown empty on error — non-fatal */ }
		selectedScope = page.url.searchParams.get('scope') ?? '';
		await loadBoard();
	});
</script>

<div class="board-toolbar">
	<select class="board-select" bind:value={selectedScope} onchange={() => selectScope(selectedScope)}>
		<option value="">All scopes</option>
		{#each scopes as scope}
			<option value={scope}>{scope}</option>
		{/each}
	</select>
	<select class="board-select" bind:value={selectedKindPrefix} onchange={() => loadBoard()}>
		<option value="effort">effort.*</option>
		<option value="intent">intent.*</option>
		<option value="knowledge">knowledge.*</option>
	</select>
	{#if selectedScope}
		<span class="board-count">{totalCount} artifacts</span>
	{/if}
</div>

{#if error}
	<div class="board-error">{error}</div>
{:else if loading}
	<div class="board-loading">Loading...</div>
{:else}
	<div class="board-columns">
		{#each Object.entries(columns) as [status, items]}
			<BoardColumn
				{status}
				label={statusLabel(status)}
				{items}
				onupdate={handleColumnUpdate}
			/>
		{/each}
	</div>
{/if}

<style>
	.board-toolbar {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		padding: var(--space-3) var(--space-4);
		border-bottom: 1px solid var(--border);
		background: var(--bg-surface);
	}
	.board-select {
		background: var(--bg-raised);
		border: 1px solid var(--border);
		color: var(--text);
		font-size: 13px;
		border-radius: var(--radius);
		padding: var(--space-1) var(--space-3);
		transition: var(--transition);
	}
	.board-select:focus {
		outline: none;
		border-color: var(--accent);
		box-shadow: 0 0 0 2px var(--accent-subtle);
	}
	.board-count {
		font-size: 11px;
		color: var(--text-muted);
	}
	.board-columns {
		display: flex;
		gap: var(--space-3);
		padding: var(--space-4);
		overflow-x: auto;
		height: calc(100vh - 120px);
		align-items: flex-start;
	}
	.board-loading, .board-error {
		display: flex;
		align-items: center;
		justify-content: center;
		height: 100%;
		color: var(--text-muted);
	}
	.board-error {
		color: #ef4444;
	}
</style>
