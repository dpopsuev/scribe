<script lang="ts">
	import { fetchArtifacts, fetchScopes, type Artifact } from '$lib/api';
	import { onMount } from 'svelte';
	import { dndzone } from 'svelte-dnd-action';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';

	const EFFORT_STATUSES = ['work.draft', 'work.active', 'work.blocked', 'work.complete', 'cancelled'];
	const INTENT_STATUSES = ['work.draft', 'decision.proposed', 'decision.accepted', 'decision.rejected', 'archived'];
	const KNOWLEDGE_STATUSES = ['note.fleeting', 'note.mature', 'note.evergreen', 'archived'];

	let scopes: string[] = $state([]);
	let selectedScope = $state('');
	let selectedKindPrefix = $state('effort');
	let columns: Record<string, Artifact[]> = $state({});
	let loading = $state(true);

	function statusesFor(prefix: string): string[] {
		if (prefix === 'effort') return EFFORT_STATUSES;
		if (prefix === 'intent') return INTENT_STATUSES;
		if (prefix === 'knowledge') return KNOWLEDGE_STATUSES;
		return EFFORT_STATUSES;
	}

	function statusLabel(s: string): string {
		return s.split('.').pop()?.replace(/_/g, ' ') ?? s;
	}

	async function loadBoard() {
		loading = true;
		const params: Record<string, string> = { kind_prefix: selectedKindPrefix };
		if (selectedScope) params.scope = selectedScope;

		const arts = await fetchArtifacts(params);
		const statuses = statusesFor(selectedKindPrefix);
		const cols: Record<string, Artifact[]> = {};
		statuses.forEach(s => cols[s] = []);
		arts.forEach(a => {
			if (cols[a.status]) cols[a.status].push(a);
		});
		columns = cols;
		loading = false;
	}

	function handleDndConsider(status: string, e: CustomEvent) {
		columns[status] = e.detail.items;
	}

	function handleDndFinalize(status: string, e: CustomEvent) {
		columns[status] = e.detail.items;
	}

	function selectScope(scope: string) {
		selectedScope = scope;
		const url = new URL(page.url);
		if (scope) url.searchParams.set('scope', scope);
		else url.searchParams.delete('scope');
		goto(url.toString(), { replaceState: true });
		loadBoard();
	}

	onMount(async () => {
		const scopeData = await fetchScopes();
		scopes = scopeData.map(s => s.scope).filter(s => s && s !== '_schema');
		selectedScope = page.url.searchParams.get('scope') ?? '';
		await loadBoard();
	});
</script>

<div class="flex flex-col h-full">
	<div class="flex items-center gap-3 px-4 py-3 border-b border-[var(--border)]">
		<select
			class="bg-[var(--bg-column)] border border-[var(--border)] text-sm rounded px-2 py-1 text-[var(--text)]"
			bind:value={selectedScope}
			onchange={() => selectScope(selectedScope)}
		>
			<option value="">All scopes</option>
			{#each scopes as scope}
				<option value={scope}>{scope}</option>
			{/each}
		</select>
		<select
			class="bg-[var(--bg-column)] border border-[var(--border)] text-sm rounded px-2 py-1 text-[var(--text)]"
			bind:value={selectedKindPrefix}
			onchange={() => loadBoard()}
		>
			<option value="effort">effort.*</option>
			<option value="intent">intent.*</option>
			<option value="knowledge">knowledge.*</option>
		</select>
		{#if selectedScope}
			<span class="text-xs text-[var(--text-muted)]">
				{Object.values(columns).flat().length} artifacts
			</span>
		{/if}
	</div>

	{#if loading}
		<div class="flex items-center justify-center h-full text-[var(--text-muted)]">Loading...</div>
	{:else}
		<div class="flex gap-3 p-4 overflow-x-auto h-full items-start">
			{#each Object.entries(columns) as [status, items]}
				<div class="flex flex-col min-w-[250px] max-w-[280px] bg-[var(--bg-column)] border border-[var(--border)] rounded-lg shrink-0 max-h-[calc(100vh-120px)]">
					<div class="flex items-center justify-between px-3 py-2 border-b border-[var(--border)]">
						<span class="text-xs font-semibold uppercase tracking-wide text-[var(--text-muted)]">{statusLabel(status)}</span>
						<span class="text-xs bg-[var(--border)] px-2 py-0.5 rounded-full">{items.length}</span>
					</div>
					<div
						class="flex flex-col gap-2 p-2 overflow-y-auto flex-1 min-h-[60px]"
						use:dndzone={{ items, flipDurationMs: 200 }}
						onconsider={(e: CustomEvent) => handleDndConsider(status, e)}
						onfinalize={(e: CustomEvent) => handleDndFinalize(status, e)}
					>
						{#each items as card (card.id)}
							<a
								href="/app/doc/{card.id}"
								class="block p-3 bg-[var(--bg-card)] border border-[var(--border)] rounded-md hover:border-[var(--accent)] transition-colors no-underline"
							>
								<div class="text-sm font-medium text-[var(--text)] leading-tight mb-1">{card.title}</div>
								<div class="flex items-center justify-between text-xs text-[var(--text-muted)]">
									<span>{card.kind.split('.').pop()}</span>
									<span>{card.scope}</span>
								</div>
								{#if card.score > 0}
									<div class="mt-2 h-0.5 bg-[var(--border)] rounded-full">
										<div class="h-full bg-emerald-500 rounded-full" style="width:{Math.round(card.score * 100)}%"></div>
									</div>
								{/if}
							</a>
						{/each}
						{#if items.length === 0}
							<div class="text-center text-xs text-[var(--text-muted)] py-4">No items</div>
						{/if}
					</div>
				</div>
			{/each}
		</div>
	{/if}
</div>
