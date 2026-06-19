<script lang="ts">
  import { fetchLenses, createLens } from '$lib/api';
  import type { LensInfo } from '$lib/api';

  let {
    lenses = [] as LensInfo[],
    activeLens = null as string | null,
    lensStats = null as { traversed: number; edges: number } | null,
    onSelect = (_id: string) => {},
    onLensesChanged = (_l: LensInfo[]) => {},
  }: {
    lenses: LensInfo[];
    activeLens: string | null;
    lensStats: { traversed: number; edges: number } | null;
    onSelect?: (id: string) => void;
    onLensesChanged?: (l: LensInfo[]) => void;
  } = $props();

  let showForm = $state(false);
  let form = $state({ title: '', anchor: '', traverse: '', exclude: '', scoreBy: 'edges' });

  async function submit() {
    if (!form.title || !form.anchor) return;
    const anchors = form.anchor.split(',').map(s => s.trim()).filter(Boolean);
    const traverseRules = form.traverse.split(',').map(t => {
      const parts = t.trim().split(':');
      return { relation: parts[0] || '', direction: parts[1] || 'both', max_depth: parseInt(parts[2]) || 3 };
    }).filter(r => r.relation);
    const excludes = form.exclude ? form.exclude.split(',').map(s => s.trim()).filter(Boolean) : undefined;

    await createLens({
      title: form.title,
      anchor: anchors,
      traverse: traverseRules.length > 0 ? traverseRules : [{ relation: 'depends_on', direction: 'both', max_depth: 3 }],
      exclude: excludes,
      score_by: form.scoreBy,
    });

    const updated = await fetchLenses();
    onLensesChanged(updated);
    showForm = false;
    form = { title: '', anchor: '', traverse: '', exclude: '', scoreBy: 'edges' };
  }
</script>

{#if lenses.length > 0}
  <div class="lens-list">
    {#each lenses as lens}
      <button
        class="lens-btn"
        class:active={activeLens === lens.id}
        onclick={() => onSelect(lens.id)}
      >{lens.title}</button>
    {/each}
  </div>
  {#if lensStats}
    <div class="mode-detail">{lensStats.traversed} artifacts · {lensStats.edges} edges</div>
  {/if}
{:else if !showForm}
  <div class="mode-detail">No stored lenses</div>
{/if}

{#if showForm}
  <div class="lens-form">
    <input class="lens-input" placeholder="Lens name" bind:value={form.title} />
    <input class="lens-input" placeholder="Anchor labels (e.g. project:ptp)" bind:value={form.anchor} />
    <input class="lens-input" placeholder="Traverse (e.g. depends_on:both:3)" bind:value={form.traverse} />
    <input class="lens-input" placeholder="Exclude (e.g. status:archived)" bind:value={form.exclude} />
    <select class="lens-input" bind:value={form.scoreBy}>
      <option value="edges">Score: edges</option>
      <option value="pagerank">Score: pagerank</option>
      <option value="recency">Score: recency</option>
      <option value="weight">Score: weight</option>
    </select>
    <div class="lens-form-actions">
      <button class="lens-form-btn create" onclick={submit}>Create</button>
      <button class="lens-form-btn" onclick={() => { showForm = false; }}>Cancel</button>
    </div>
  </div>
{:else}
  <button class="lens-btn new-lens" onclick={() => { showForm = true; }}>+ New Lens</button>
{/if}

<style>
  .lens-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .lens-btn {
    text-align: left;
    font-size: 0.8125rem;
    padding: 8px 13px;
    min-height: 36px;
    border-radius: 6px;
    background: rgba(255,255,255,0.04);
    border: 1px solid rgba(255,255,255,0.08);
    color: #94a3b8;
    cursor: pointer;
  }
  .lens-btn:hover {
    border-color: #ec4899;
    color: #f9a8d4;
    background: rgba(236,72,153,0.08);
  }
  .lens-btn.active {
    background: rgba(236,72,153,0.2);
    border-color: #ec4899;
    color: #f9a8d4;
  }
  .lens-form {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-top: 8px;
  }
  .lens-input {
    font-size: 0.8125rem;
    padding: 8px 13px;
    min-height: 36px;
    border-radius: 6px;
    background: rgba(255,255,255,0.06);
    border: 1px solid rgba(255,255,255,0.12);
    color: #e2e8f0;
    width: 100%;
    box-sizing: border-box;
  }
  .lens-input::placeholder { color: #64748b; }
  .lens-input:focus {
    outline: none;
    border-color: #ec4899;
    box-shadow: 0 0 0 2px rgba(236,72,153,0.15);
  }
  .lens-form-actions {
    display: flex;
    gap: 8px;
    margin-top: 4px;
  }
  .lens-form-btn {
    flex: 1;
    font-size: 0.8125rem;
    padding: 8px 13px;
    min-height: 36px;
    border-radius: 6px;
    background: rgba(255,255,255,0.05);
    border: 1px solid rgba(255,255,255,0.12);
    color: #94a3b8;
    cursor: pointer;
  }
  .lens-form-btn.create {
    background: rgba(236,72,153,0.2);
    border-color: #ec4899;
    color: #f9a8d4;
  }
  .lens-form-btn:hover { border-color: rgba(255,255,255,0.3); }
  .new-lens {
    margin-top: 8px;
    border-style: dashed;
    text-align: center;
    color: #64748b;
  }
  .new-lens:hover {
    color: #f9a8d4;
    border-color: #ec4899;
  }
  .mode-detail {
    font-size: 0.8125rem;
    opacity: 0.6;
    line-height: 1.5;
    margin-top: 6px;
  }
</style>
