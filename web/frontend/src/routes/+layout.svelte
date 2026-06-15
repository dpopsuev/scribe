<script lang="ts">
	import '../app.css';
	import { page } from '$app/state';

	let { children } = $props();

	const nav = [
		{ href: '/app/board', label: 'Board' },
		{ href: '/app/graph', label: 'Graph' },
	];

	function isActive(href: string): boolean {
		return page.url.pathname.startsWith(href);
	}
</script>

<div class="shell">
	<nav class="topbar">
		<a href="/app" class="topbar-brand">Scribe</a>
		{#each nav as item}
			<a href={item.href} class="topbar-link" class:active={isActive(item.href)}>
				{item.label}
			</a>
		{/each}
	</nav>
	<main class="content">
		{@render children()}
	</main>
</div>

<style>
	.shell {
		display: flex;
		flex-direction: column;
		height: 100vh;
	}
	.topbar {
		display: flex;
		align-items: center;
		gap: 1.5rem;
		padding: 0.5rem 1rem;
		border-bottom: 1px solid var(--border);
		background: var(--bg-column);
	}
	.topbar-brand {
		font-weight: 700;
		font-size: 1.1rem;
		color: var(--accent);
		text-decoration: none;
	}
	.topbar-link {
		font-size: 0.875rem;
		color: var(--text-muted);
		text-decoration: none;
		transition: color 0.15s;
	}
	.topbar-link:hover, .topbar-link.active {
		color: white;
	}
	.content {
		flex: 1;
		overflow: hidden;
	}
</style>
