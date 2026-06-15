<script lang="ts">
	import type { Artifact } from '$lib/api';

	let { artifact }: { artifact: Artifact } = $props();

	let kindShort = $derived(artifact.kind.split('.').pop() ?? artifact.kind);
	let scorePercent = $derived(Math.round(artifact.score * 100));
</script>

<a href="/app/doc/{artifact.id}" class="card">
	<div class="card-title">{artifact.title}</div>
	<div class="card-meta">
		<span class="card-kind">{kindShort}</span>
		<span class="card-scope">{artifact.scope}</span>
	</div>
	{#if artifact.score > 0}
		<div class="card-progress">
			<div class="card-progress-fill" style="width:{scorePercent}%"></div>
		</div>
	{/if}
</a>

<style>
	.card {
		display: block;
		padding: var(--space-3);
		background: var(--bg-surface);
		border: 1px solid var(--border-subtle);
		border-radius: var(--radius);
		text-decoration: none;
		color: inherit;
		transition: var(--transition);
	}
	.card:hover {
		border-color: var(--accent);
		box-shadow: 0 0 0 1px var(--accent-subtle);
		transform: translateY(-1px);
	}
	.card-title {
		font-size: 13px;
		font-weight: 500;
		line-height: 1.35;
		margin-bottom: var(--space-1);
		color: var(--text);
	}
	.card-meta {
		display: flex;
		justify-content: space-between;
		font-size: 11px;
		color: var(--text-muted);
	}
	.card-kind {
		background: var(--accent-subtle);
		color: var(--accent);
		padding: 1px 6px;
		border-radius: var(--radius-s);
		font-weight: 500;
	}
	.card-progress {
		margin-top: var(--space-2);
		height: 2px;
		background: var(--border);
		border-radius: 1px;
	}
	.card-progress-fill {
		height: 100%;
		background: var(--success);
		border-radius: 1px;
		transition: width 0.3s ease;
	}
</style>
