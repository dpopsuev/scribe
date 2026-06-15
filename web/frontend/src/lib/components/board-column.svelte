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

<div class="board-column">
	<div class="board-column-header">
		<span class="board-column-label">{label}</span>
		<span class="board-column-count">{items.length}</span>
	</div>
	<div
		class="board-column-body"
		use:dndzone={{ items, flipDurationMs: 200 }}
		onconsider={handleConsider}
		onfinalize={handleFinalize}
	>
		{#each items as card (card.id)}
			<BoardCard artifact={card} />
		{/each}
		{#if items.length === 0}
			<div class="board-column-empty">No items</div>
		{/if}
	</div>
</div>

<style>
	.board-column {
		display: flex;
		flex-direction: column;
		min-width: 250px;
		max-width: 280px;
		flex-shrink: 0;
		background: var(--bg-column);
		border: 1px solid var(--border);
		border-radius: 8px;
		max-height: calc(100vh - 120px);
	}
	.board-column-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		padding: 0.5rem 0.75rem;
		border-bottom: 1px solid var(--border);
	}
	.board-column-label {
		font-size: 0.75rem;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.03em;
		color: var(--text-muted);
	}
	.board-column-count {
		font-size: 0.75rem;
		background: var(--border);
		padding: 0.1em 0.5em;
		border-radius: 10px;
	}
	.board-column-body {
		display: flex;
		flex-direction: column;
		gap: 0.5rem;
		padding: 0.5rem;
		overflow-y: auto;
		flex: 1;
		min-height: 60px;
	}
	.board-column-empty {
		text-align: center;
		font-size: 0.75rem;
		color: var(--text-muted);
		padding: 1rem;
	}
</style>
