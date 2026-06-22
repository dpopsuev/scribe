<script lang="ts">
	import { fetchArtifacts, fetchScopes, type Artifact, type Scope } from '$lib/api';
	import { goto } from '$app/navigation';

	let artifacts: Artifact[] = $state([]);
	let scopes: Scope[] = $state([]);
	let loading = $state(true);

	let scope = $state('');
	let kind = $state('');
	let status = $state('');
	let sortCol = $state('id');
	let sortAsc = $state(true);
	let search = $state('');

	async function load() {
		loading = true;
		const params: Record<string, string> = {};
		if (scope) params.scope = scope;
		if (kind) params.kind_prefix = kind;
		if (status) params.status = status;
		const [arts, sc] = await Promise.all([fetchArtifacts(params), fetchScopes()]);
		artifacts = arts;
		scopes = sc;
		loading = false;
	}

	$effect(() => {
		void scope; void kind; void status;
		load();
	});

	let filtered = $derived.by(() => {
		let result = artifacts;
		if (search) {
			const q = search.toLowerCase();
			result = result.filter(a => a.title.toLowerCase().includes(q) || a.id.toLowerCase().includes(q));
		}
		result = [...result].sort((a, b) => {
			const va = (a as any)[sortCol] ?? '';
			const vb = (b as any)[sortCol] ?? '';
			return sortAsc ? String(va).localeCompare(String(vb)) : String(vb).localeCompare(String(va));
		});
		return result;
	});

	function toggleSort(col: string) {
		if (sortCol === col) {
			sortAsc = !sortAsc;
		} else {
			sortCol = col;
			sortAsc = true;
		}
	}

	function sortIndicator(col: string): string {
		if (sortCol !== col) return '';
		return sortAsc ? ' ▲' : ' ▼';
	}

	const columns = [
		{ key: 'id', label: 'ID' },
		{ key: 'title', label: 'Title' },
		{ key: 'kind', label: 'Kind' },
		{ key: 'status', label: 'Status' },
		{ key: 'scope', label: 'Scope' },
	];

	const kindPrefixes = ['effort', 'intent', 'knowledge', 'support', 'code', 'agent', 'investigation'];
</script>

<div class="table-view">
	<div class="toolbar">
		<input class="search" type="text" placeholder="Filter by title or ID..." bind:value={search} />
		<select bind:value={scope}>
			<option value="">All scopes</option>
			{#each scopes as s}
				<option value={s.scope}>{s.scope} ({s.count})</option>
			{/each}
		</select>
		<select bind:value={kind}>
			<option value="">All kinds</option>
			{#each kindPrefixes as k}
				<option value={k}>{k}.*</option>
			{/each}
		</select>
		<select bind:value={status}>
			<option value="">All statuses</option>
			<option value="work.draft">draft</option>
			<option value="work.active">active</option>
			<option value="work.blocked">blocked</option>
			<option value="work.complete">complete</option>
		</select>
		<span class="count">{filtered.length} artifacts</span>
	</div>

	{#if loading}
		<div class="loading">Loading...</div>
	{:else}
		<div class="table-wrap">
			<table>
				<thead>
					<tr>
						{#each columns as col}
							<th onclick={() => toggleSort(col.key)} class="sortable">
								{col.label}{sortIndicator(col.key)}
							</th>
						{/each}
					</tr>
				</thead>
				<tbody>
					{#each filtered as art}
						<tr onclick={() => goto(`/app/doc/${art.id}`)} class="clickable">
							<td class="mono">{art.id}</td>
							<td>{art.title}</td>
							<td class="kind">{art.kind}</td>
							<td class="status">{art.status}</td>
							<td>{art.scope}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>

<style>
	.table-view {
		display: flex;
		flex-direction: column;
		height: 100%;
		padding: var(--space-3);
		gap: var(--space-3);
	}
	.toolbar {
		display: flex;
		gap: var(--space-2);
		align-items: center;
		flex-wrap: wrap;
	}
	.search {
		flex: 1;
		min-width: 200px;
		padding: var(--space-2) var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--bg-surface);
		color: var(--text);
		font-size: 13px;
	}
	select {
		padding: var(--space-2) var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--bg-surface);
		color: var(--text);
		font-size: 13px;
	}
	.count {
		font-size: 12px;
		color: var(--text-muted);
		margin-left: auto;
	}
	.loading {
		display: flex;
		align-items: center;
		justify-content: center;
		height: 200px;
		color: var(--text-muted);
	}
	.table-wrap {
		flex: 1;
		overflow: auto;
	}
	table {
		width: 100%;
		border-collapse: collapse;
		font-size: 13px;
	}
	th {
		text-align: left;
		padding: var(--space-2) var(--space-3);
		border-bottom: 2px solid var(--border);
		color: var(--text-muted);
		font-weight: 600;
		font-size: 11px;
		text-transform: uppercase;
		letter-spacing: 0.5px;
		position: sticky;
		top: 0;
		background: var(--bg);
	}
	th.sortable {
		cursor: pointer;
		user-select: none;
	}
	th.sortable:hover {
		color: var(--accent);
	}
	td {
		padding: var(--space-2) var(--space-3);
		border-bottom: 1px solid var(--border);
	}
	tr.clickable {
		cursor: pointer;
		transition: var(--transition);
	}
	tr.clickable:hover {
		background: var(--accent-subtle);
	}
	.mono {
		font-family: var(--font-mono, monospace);
		font-size: 12px;
		color: var(--text-muted);
	}
	.kind {
		color: var(--accent);
	}
	.status {
		font-size: 12px;
	}
</style>
