<script lang="ts">
	import { fetchArtifacts, fetchScopes, type Artifact, type Scope } from '$lib/api';
	import { kindColor } from '$lib/colors';

	let artifacts: Artifact[] = $state([]);
	let scopes: Scope[] = $state([]);
	let loading = $state(true);
	let mode = $state<'scope-kind' | 'activity'>('scope-kind');

	async function load() {
		loading = true;
		const [arts, sc] = await Promise.all([fetchArtifacts(), fetchScopes()]);
		artifacts = arts;
		scopes = sc;
		loading = false;
	}

	$effect(() => { load(); });

	let heatmapData = $derived.by(() => {
		if (mode === 'scope-kind') {
			const scopeSet = new Set<string>();
			const kindSet = new Set<string>();
			const counts = new Map<string, number>();
			for (const a of artifacts) {
				scopeSet.add(a.scope || '(none)');
				kindSet.add(a.kind || '(none)');
				const key = `${a.scope || '(none)'}|${a.kind || '(none)'}`;
				counts.set(key, (counts.get(key) ?? 0) + 1);
			}
			return {
				rows: [...scopeSet].sort(),
				cols: [...kindSet].sort(),
				counts,
				max: Math.max(1, ...counts.values()),
			};
		}
		const statusSet = new Set<string>();
		const kindSet = new Set<string>();
		const counts = new Map<string, number>();
		for (const a of artifacts) {
			statusSet.add(a.status || '(none)');
			kindSet.add(a.kind || '(none)');
			const key = `${a.status || '(none)'}|${a.kind || '(none)'}`;
			counts.set(key, (counts.get(key) ?? 0) + 1);
		}
		return {
			rows: [...statusSet].sort(),
			cols: [...kindSet].sort(),
			counts,
			max: Math.max(1, ...counts.values()),
		};
	});

	function heatColor(count: number, max: number): string {
		if (count === 0) return 'transparent';
		const t = count / max;
		const h = 240 - t * 240;
		return `hsl(${h}, 70%, ${50 - t * 15}%)`;
	}
</script>

<div class="heatmap-view">
	<div class="toolbar">
		<select bind:value={mode}>
			<option value="scope-kind">Scope x Kind</option>
			<option value="activity">Status x Kind</option>
		</select>
		<span class="count">{artifacts.length} artifacts</span>
	</div>

	{#if loading}
		<div class="loading">Loading...</div>
	{:else}
		<div class="heatmap-wrap">
			<table class="heatmap">
				<thead>
					<tr>
						<th class="corner">{mode === 'scope-kind' ? 'Scope' : 'Status'}</th>
						{#each heatmapData.cols as col}
							<th class="col-header"><span>{col.split('.').pop()}</span></th>
						{/each}
					</tr>
				</thead>
				<tbody>
					{#each heatmapData.rows as row}
						<tr>
							<td class="row-header">{row}</td>
							{#each heatmapData.cols as col}
								{@const count = heatmapData.counts.get(`${row}|${col}`) ?? 0}
								<td
									class="cell"
									style="background: {heatColor(count, heatmapData.max)}"
									title="{row} / {col}: {count}"
								>
									{#if count > 0}
										<span class="cell-count">{count}</span>
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
	.heatmap-view {
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
	.heatmap-wrap { flex: 1; overflow: auto; }
	.heatmap { border-collapse: collapse; font-size: 12px; }
	.corner { padding: var(--space-2); color: var(--text-muted); font-weight: 600; text-transform: uppercase; font-size: 10px; }
	.col-header { height: 100px; vertical-align: bottom; padding: var(--space-1); }
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
		width: 44px;
		height: 44px;
		text-align: center;
		vertical-align: middle;
		border: 1px solid var(--border);
		transition: var(--transition);
	}
	.cell:hover { outline: 2px solid var(--text); outline-offset: -2px; }
	.cell-count { font-size: 11px; font-weight: 600; color: white; text-shadow: 0 0 3px rgba(0,0,0,0.5); }
</style>
