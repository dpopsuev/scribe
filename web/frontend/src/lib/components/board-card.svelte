<script lang="ts">
	import type { Artifact } from '$lib/api';

	let { artifact }: { artifact: Artifact } = $props();

	let kindShort = $derived(artifact.kind.split('.').pop() ?? artifact.kind);
	let scorePercent = $derived(Math.round(artifact.score * 100));
</script>

<a
	href="/app/doc/{artifact.id}"
	class="board-card"
>
	<div class="board-card-title">{artifact.title}</div>
	<div class="board-card-meta">
		<span>{kindShort}</span>
		<span>{artifact.scope}</span>
	</div>
	{#if artifact.score > 0}
		<div class="board-card-progress">
			<div class="board-card-progress-fill" style="width:{scorePercent}%"></div>
		</div>
	{/if}
</a>

<style>
	.board-card {
		display: block;
		padding: 0.75rem;
		background: var(--bg-card);
		border: 1px solid var(--border);
		border-radius: 6px;
		text-decoration: none;
		color: inherit;
		transition: border-color 0.15s;
	}
	.board-card:hover {
		border-color: var(--accent);
	}
	.board-card-title {
		font-size: 0.875rem;
		font-weight: 500;
		line-height: 1.3;
		margin-bottom: 0.25rem;
	}
	.board-card-meta {
		display: flex;
		justify-content: space-between;
		font-size: 0.72rem;
		color: var(--text-muted);
	}
	.board-card-progress {
		margin-top: 0.5rem;
		height: 2px;
		background: var(--border);
		border-radius: 1px;
	}
	.board-card-progress-fill {
		height: 100%;
		background: #22c55e;
		border-radius: 1px;
	}
</style>
