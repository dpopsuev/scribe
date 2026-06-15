<script lang="ts">
	import type { Artifact } from '$lib/api';
	import BoardCard from './board-card.svelte';
	import { dndzone } from 'svelte-dnd-action';

	let { status, label, items, onupdate }: {
		status: string;
		label: string;
		items: Artifact[];
		onupdate: (status: string, items: Artifact[]) => void;
	} = $props();

	function handleConsider(e: CustomEvent) { onupdate(status, e.detail.items); }
	function handleFinalize(e: CustomEvent) { onupdate(status, e.detail.items); }
</script>

<div class="column">
	<div class="column-header">
		<span class="column-label">{label}</span>
		<span class="column-count">{items.length}</span>
	</div>
	<div
		class="column-body"
		use:dndzone={{ items, flipDurationMs: 200 }}
		onconsider={handleConsider}
		onfinalize={handleFinalize}
	>
		{#each items as card (card.id)}
			<BoardCard artifact={card} />
		{/each}
		{#if items.length === 0}
			<div class="column-empty">No items</div>
		{/if}
	</div>
</div>

<style>
	.column {
		display: flex;
		flex-direction: column;
		min-width: 260px;
		max-width: 290px;
		flex-shrink: 0;
		background: var(--bg-raised);
		border: 1px solid var(--border-subtle);
		border-radius: var(--radius-l);
		max-height: calc(100vh - 120px);
	}
	.column-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: var(--space-3) var(--space-4);
		border-bottom: 1px solid var(--border-subtle);
	}
	.column-label {
		font-size: 12px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.04em;
		color: var(--text-muted);
	}
	.column-count {
		font-size: 11px;
		font-weight: 600;
		background: var(--border);
		color: var(--text-secondary);
		padding: 1px 8px;
		border-radius: 10px;
	}
	.column-body {
		display: flex;
		flex-direction: column;
		gap: var(--space-2);
		padding: var(--space-2);
		overflow-y: auto;
		flex: 1;
		min-height: 60px;
	}
	.column-empty {
		text-align: center;
		font-size: 12px;
		color: var(--text-muted);
		padding: var(--space-5);
	}
</style>
