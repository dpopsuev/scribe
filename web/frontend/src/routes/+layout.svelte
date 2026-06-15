<script lang="ts">
	import '../app.css';
	import { page } from '$app/state';

	let { children } = $props();

	const nav = [
		{ href: '/app/board', label: 'Board' },
		{ href: '/app/graph', label: 'Graph' },
	];
</script>

<div class="flex flex-col h-screen">
	<nav class="flex items-center gap-6 px-4 py-2 border-b border-[var(--border)] bg-[var(--bg-column)]">
		<a href="/app" class="font-bold text-lg text-[var(--accent)]">Scribe</a>
		{#each nav as item}
			<a
				href={item.href}
				class="text-sm transition-colors {page.url.pathname.startsWith(item.href) ? 'text-white' : 'text-[var(--text-muted)] hover:text-white'}"
			>{item.label}</a>
		{/each}
	</nav>
	<main class="flex-1 overflow-hidden">
		{@render children()}
	</main>
</div>
