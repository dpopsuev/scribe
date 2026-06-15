<script lang="ts">
	import { page } from '$app/state';
	let id = $derived(page.params.id);
</script>

<div class="p-6">
	<h1 class="text-xl font-bold mb-4">Document: {id}</h1>
	<p class="text-[var(--text-muted)]">Block editor — coming soon</p>
</div>
