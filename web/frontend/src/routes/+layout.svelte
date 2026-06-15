<script lang="ts">
	import '../app.css';
	import { page } from '$app/state';

	let { children } = $props();

	const nav = [
		{ href: '/app/board', label: 'Board', icon: '▦' },
		{ href: '/app/graph', label: 'Graph', icon: '◉' },
	];

	function isActive(href: string): boolean {
		return page.url.pathname.startsWith(href);
	}

	let darkMode = $state(true);

	function toggleTheme() {
		darkMode = !darkMode;
		document.documentElement.setAttribute('data-theme', darkMode ? '' : 'light');
	}
</script>

<div class="shell">
	<nav class="topbar">
		<a href="/app" class="topbar-brand">
			<span class="brand-icon">◆</span>
			<span>Scribe</span>
		</a>
		<div class="topbar-nav">
			{#each nav as item}
				<a href={item.href} class="topbar-link" class:active={isActive(item.href)}>
					<span class="nav-icon">{item.icon}</span>
					{item.label}
				</a>
			{/each}
		</div>
		<div class="topbar-right">
			<button class="theme-toggle" onclick={toggleTheme} title="Toggle dark/light mode">
				{darkMode ? '☀' : '☾'}
			</button>
		</div>
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
		gap: var(--space-4);
		padding: var(--space-2) var(--space-4);
		border-bottom: 1px solid var(--border);
		background: var(--bg-surface);
		min-height: 44px;
	}
	.topbar-brand {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		font-weight: 600;
		font-size: 15px;
		color: var(--accent);
		text-decoration: none;
		transition: var(--transition);
	}
	.topbar-brand:hover { opacity: 0.85; }
	.brand-icon { font-size: 18px; }
	.topbar-nav {
		display: flex;
		gap: var(--space-1);
	}
	.topbar-link {
		display: flex;
		align-items: center;
		gap: var(--space-1);
		font-size: 13px;
		font-weight: 500;
		color: var(--text-muted);
		text-decoration: none;
		padding: var(--space-1) var(--space-3);
		border-radius: var(--radius);
		transition: var(--transition);
	}
	.topbar-link:hover {
		color: var(--text);
		background: var(--accent-subtle);
	}
	.topbar-link.active {
		color: var(--accent);
		background: var(--accent-subtle);
	}
	.nav-icon { font-size: 12px; opacity: 0.7; }
	.topbar-right {
		margin-left: auto;
	}
	.theme-toggle {
		background: none;
		border: 1px solid var(--border);
		color: var(--text-muted);
		font-size: 14px;
		width: 32px;
		height: 32px;
		border-radius: var(--radius);
		cursor: pointer;
		transition: var(--transition);
		display: flex;
		align-items: center;
		justify-content: center;
	}
	.theme-toggle:hover {
		background: var(--bg-raised);
		color: var(--text);
		border-color: var(--text-muted);
	}
	.content {
		flex: 1;
		overflow: hidden;
	}
</style>
