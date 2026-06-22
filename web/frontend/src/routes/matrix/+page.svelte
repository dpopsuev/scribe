<script lang="ts">
	import { fetchScopes, type Scope } from '$lib/api';

	interface GraphData {
		nodes: Array<{ id: string; name: string; kind: string; scope: string }>;
		links: Array<{ source: string; target: string; relation: string }>;
	}

	let data: GraphData = $state({ nodes: [], links: [] });
	let scopes: Scope[] = $state([]);
	let loading = $state(true);
	let scope = $state('');
	let axis = $state<'kind' | 'scope'>('kind');

	async function load() {
		loading = true;
		const params = new URLSearchParams();
		if (scope) params.set('scope', scope);
		params.set('max_nodes', '500');
		const [graphRes, scopeRes] = await Promise.all([
			fetch(`/api/v1/graph?${params}`).then(r => r.json()),
			fetchScopes(),
		]);
		data = graphRes;
		scopes = scopeRes;
		loading = false;
	}

	$effect(() => { void scope; load(); });

	interface Cell { count: number; relations: string[] }

	let matrix = $derived.by(() => {
		const nodeMap = new Map(data.nodes.map(n => [n.id, n]));
		const keys = new Set<string>();
		for (const n of data.nodes) {
			keys.add(axis === 'kind' ? n.kind : n.scope);
		}
		const labels = [...keys].sort();
		const cells = new Map<string, Cell>();

		for (const link of data.links) {
			const src = nodeMap.get(link.source);
			const tgt = nodeMap.get(link.target);
			if (!src || !tgt) continue;
			const rowKey = axis === 'kind' ? src.kind : src.scope;
			const colKey = axis === 'kind' ? tgt.kind : tgt.scope;
			const key = `${rowKey}|${colKey}`;
			const cell = cells.get(key) ?? { count: 0, relations: [] };
			cell.count++;
			if (!cell.relations.includes(link.relation)) cell.relations.push(link.relation);
			cells.set(key, cell);
		}

		return { labels, cells };
	});

	let maxCount = $derived(Math.max(1, ...[...matrix.cells.values()].map(c => c.count)));

	function cellColor(count: number): string {
		if (count === 0) return 'transparent';
		const intensity = Math.min(count / maxCount, 1);
		const alpha = 0.1 + intensity * 0.8;
		return `rgba(99, 102, 241, ${alpha})`;
	}
</script>

<div class="matrix-view">
	<div class="toolbar">
		<select bind:value={scope}>
			<option value="">All scopes</option>
			{#each scopes as s}<option value={s.scope}>{s.scope}</option>{/each}
		</select>
		<select bind:value={axis}>
			<option value="kind">Kind x Kind</option>
			<option value="scope">Scope x Scope</option>
		</select>
		<span class="count">{data.nodes.length} nodes, {data.links.length} edges</span>
	</div>

	{#if loading}
		<div class="loading">Loading...</div>
	{:else}
		<div class="matrix-wrap">
			<table class="dsm">
				<thead>
					<tr>
						<th class="corner"></th>
						{#each matrix.labels as col}
							<th class="col-header"><span>{col}</span></th>
						{/each}
					</tr>
				</thead>
				<tbody>
					{#each matrix.labels as row}
						<tr>
							<td class="row-header">{row}</td>
							{#each matrix.labels as col}
								{@const cell = matrix.cells.get(`${row}|${col}`)}
								<td
									class="cell"
									class:diagonal={row === col}
									style="background: {cellColor(cell?.count ?? 0)}"
									title="{row} → {col}: {cell?.count ?? 0} edges ({cell?.relations.join(', ') ?? 'none'})"
								>
									{#if cell && cell.count > 0}
										<span class="cell-count">{cell.count}</span>
									{/if}
								</td>
							{/each}
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</div>

<style>
	.matrix-view {
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
	.matrix-wrap { flex: 1; overflow: auto; }
	.dsm { border-collapse: collapse; font-size: 12px; }
	.corner { width: 120px; }
	.col-header { height: 120px; vertical-align: bottom; padding: var(--space-1); }
	.col-header span {
		writing-mode: vertical-rl;
		transform: rotate(180deg);
		white-space: nowrap;
		color: var(--text-muted);
		font-weight: 500;
	}
	.row-header {
		text-align: right;
		padding: var(--space-1) var(--space-2);
		color: var(--text-muted);
		font-weight: 500;
		white-space: nowrap;
	}
	.cell {
		width: 36px;
		height: 36px;
		text-align: center;
		vertical-align: middle;
		border: 1px solid var(--border);
		cursor: default;
		transition: var(--transition);
	}
	.cell:hover { outline: 2px solid var(--accent); outline-offset: -2px; }
	.cell.diagonal { background: var(--bg-raised) !important; }
	.cell-count { font-size: 10px; font-weight: 600; color: var(--text); }
</style>
