<script lang="ts">
	import { fetchArtifacts, fetchScopes, type Artifact, type Scope } from '$lib/api';

	let artifacts: Artifact[] = $state([]);
	let scopes: Scope[] = $state([]);
	let loading = $state(true);
	let scope = $state('');
	let kind = $state('');
	let groupBy = $state<'kind' | 'scope'>('kind');

	interface TimelineItem {
		art: Artifact;
		created: Date;
	}

	async function load() {
		loading = true;
		const params: Record<string, string> = {};
		if (scope) params.scope = scope;
		if (kind) params.kind_prefix = kind;
		const [arts, sc] = await Promise.all([fetchArtifacts(params), fetchScopes()]);
		artifacts = arts;
		scopes = sc;
		loading = false;
	}

	$effect(() => { void scope; void kind; load(); });

	let items = $derived.by(() => {
		return artifacts
			.map(a => ({ art: a, created: new Date((a as any).created_at ?? Date.now()) }))
			.sort((a, b) => b.created.getTime() - a.created.getTime());
	});

	let groups = $derived.by(() => {
		const map = new Map<string, TimelineItem[]>();
		for (const item of items) {
			const key = groupBy === 'kind' ? item.art.kind : item.art.scope;
			const list = map.get(key) ?? [];
			list.push(item);
			map.set(key, list);
		}
		return [...map.entries()].sort(([a], [b]) => a.localeCompare(b));
	});

	let timeRange = $derived.by(() => {
		if (items.length === 0) return { min: new Date(), max: new Date() };
		return { min: items[items.length - 1].created, max: items[0].created };
	});

	function position(date: Date): number {
		const range = timeRange.max.getTime() - timeRange.min.getTime();
		if (range === 0) return 50;
		return ((date.getTime() - timeRange.min.getTime()) / range) * 100;
	}

	function formatDate(d: Date): string {
		return d.toLocaleDateString('en-US', { month: 'short', day: 'numeric' });
	}

	const kindPrefixes = ['effort', 'intent', 'knowledge', 'support', 'code', 'agent', 'investigation'];
</script>

<div class="timeline-view">
	<div class="toolbar">
		<select bind:value={scope}>
			<option value="">All scopes</option>
			{#each scopes as s}<option value={s.scope}>{s.scope}</option>{/each}
		</select>
		<select bind:value={kind}>
			<option value="">All kinds</option>
			{#each kindPrefixes as k}<option value={k}>{k}.*</option>{/each}
		</select>
		<select bind:value={groupBy}>
			<option value="kind">Group by kind</option>
			<option value="scope">Group by scope</option>
		</select>
		<span class="count">{items.length} artifacts</span>
	</div>

	{#if loading}
		<div class="loading">Loading...</div>
	{:else if items.length === 0}
		<div class="loading">No artifacts found</div>
	{:else}
		<div class="lanes">
			<div class="time-axis">
				<span class="axis-label">{formatDate(timeRange.min)}</span>
				<div class="axis-line"></div>
				<span class="axis-label">{formatDate(timeRange.max)}</span>
			</div>
			{#each groups as [group, groupItems]}
				<div class="lane">
					<div class="lane-label">{group}</div>
					<div class="lane-track">
						{#each groupItems as item}
							<div
								class="dot"
								style="left: {position(item.created)}%"
								title="{item.art.title} ({formatDate(item.created)})"
							></div>
						{/each}
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>

<style>
	.timeline-view {
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
	}
	select {
		padding: var(--space-2) var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius);
		background: var(--bg-surface);
		color: var(--text);
		font-size: 13px;
	}
	.count { font-size: 12px; color: var(--text-muted); margin-left: auto; }
	.loading { display: flex; align-items: center; justify-content: center; height: 200px; color: var(--text-muted); }
	.lanes {
		flex: 1;
		overflow-y: auto;
		display: flex;
		flex-direction: column;
		gap: var(--space-1);
	}
	.time-axis {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		padding: 0 120px 0 120px;
		margin-bottom: var(--space-2);
	}
	.axis-label { font-size: 11px; color: var(--text-muted); white-space: nowrap; }
	.axis-line { flex: 1; height: 1px; background: var(--border); }
	.lane {
		display: flex;
		align-items: center;
		gap: var(--space-2);
	}
	.lane-label {
		width: 110px;
		font-size: 12px;
		color: var(--text-muted);
		text-align: right;
		flex-shrink: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.lane-track {
		flex: 1;
		position: relative;
		height: 20px;
		background: var(--bg-surface);
		border-radius: var(--radius-sm);
		border: 1px solid var(--border);
	}
	.dot {
		position: absolute;
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: var(--accent);
		top: 50%;
		transform: translate(-50%, -50%);
		cursor: pointer;
		transition: var(--transition);
	}
	.dot:hover {
		width: 12px;
		height: 12px;
		background: var(--accent-bright, var(--accent));
	}
</style>
