<script lang="ts">
  import { marked } from 'marked';
  marked.setOptions({ breaks: true, gfm: true });

  interface ArtifactDetail {
    id: string;
    title: string;
    labels: string[];
    sections: Array<{ name: string; text: string }>;
    extra: Record<string, any>;
    created_at: string;
    updated_at: string;
  }
  interface EdgeRef {
    from: string;
    to: string;
    relation: string;
    title: string;
    kind: string;
  }
  interface SidebarState {
    art: ArtifactDetail;
    edges: EdgeRef[];
    history: string[];
  }

  let {
    state = null as SidebarState | null,
    onNavigate = (_id: string) => {},
    onClose = () => {},
    onEdgeHover = (_edge: { source: string; target: string } | null) => {},
  }: {
    state: SidebarState | null;
    onNavigate?: (id: string) => void;
    onClose?: () => void;
    onEdgeHover?: (edge: { source: string; target: string } | null) => void;
  } = $props();

  function back() {
    if (!state || state.history.length === 0) return;
    const prev = state.history[state.history.length - 1];
    onNavigate(prev);
  }
</script>

{#if state}
  <div class="sidebar">
    <div class="sidebar-header">
      <div class="sidebar-nav">
        {#if state.history.length > 0}
          <button class="sidebar-back" onclick={back}>←</button>
        {/if}
        <button class="sidebar-close" onclick={onClose}>×</button>
      </div>
      <h3>{state.art.title}</h3>
      <div class="sidebar-meta">
        {#each state.art.labels as label}
          {#if label.startsWith('kind:')}
            <span class="tag tag-kind">{label.replace('kind:', '')}</span>
          {:else if label.startsWith('project:')}
            <span class="tag tag-scope">{label.replace('project:', '')}</span>
          {:else if !label.startsWith('encoded:') && !label.startsWith('compliance:')}
            <span class="tag">{label}</span>
          {/if}
        {/each}
      </div>
    </div>

    <div class="sidebar-body">
      {#if state.art.extra?.description}
        <div class="sidebar-section">
          <div class="section-md">{@html marked.parse(state.art.extra.description)}</div>
        </div>
      {/if}

      {#each state.art.sections || [] as section}
        <div class="sidebar-section">
          <h4>{section.name}</h4>
          <div class="section-md">{@html marked.parse(section.text)}</div>
        </div>
      {/each}

      {#if state.edges.length > 0}
        <div class="sidebar-section">
          <h4>Linked References ({state.edges.length})</h4>
          {#each state.edges as edge}
            <button
              class="edge-link"
              onclick={() => onNavigate(edge.from === state?.art.id ? edge.to : edge.from)}
              onmouseenter={() => onEdgeHover({ source: edge.from, target: edge.to })}
              onmouseleave={() => onEdgeHover(null)}
            >
              <span class="edge-relation">{edge.relation}</span>
              <span class="edge-title">{edge.title || (edge.from === state?.art.id ? edge.to : edge.from)}</span>
              {#if edge.kind}
                <span class="edge-kind">{edge.kind.split('.').pop()}</span>
              {/if}
            </button>
          {/each}
        </div>
      {/if}
    </div>
  </div>
{/if}

<style>
  .sidebar {
    position: fixed;
    top: 0;
    left: 0;
    width: 360px;
    height: 100vh;
    background: rgba(16, 16, 32, 0.97);
    border-right: 1px solid rgba(255,255,255,0.08);
    z-index: 20;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .sidebar-header {
    padding: 1rem;
    border-bottom: 1px solid rgba(255,255,255,0.06);
    flex-shrink: 0;
  }
  .sidebar-header h3 {
    margin: 0.3rem 0 0.5rem;
    font-size: 0.95em;
    color: #e2e8f0;
    line-height: 1.3;
  }
  .sidebar-nav {
    display: flex;
    justify-content: space-between;
  }
  .sidebar-back, .sidebar-close {
    background: none;
    border: 1px solid rgba(255,255,255,0.12);
    color: #94a3b8;
    border-radius: 6px;
    padding: 8px 13px;
    min-width: 36px;
    min-height: 36px;
    cursor: pointer;
    font-size: 1rem;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .sidebar-back:hover, .sidebar-close:hover {
    color: #e2e8f0;
    border-color: rgba(255,255,255,0.3);
    background: rgba(255,255,255,0.05);
  }
  .sidebar-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 0.3rem;
  }
  .tag {
    font-size: 0.75rem;
    padding: 4px 8px;
    border-radius: 4px;
    background: rgba(255,255,255,0.06);
    color: #94a3b8;
    border: 1px solid rgba(255,255,255,0.08);
    line-height: 1.4;
  }
  .tag-kind {
    background: rgba(99,102,241,0.2);
    border-color: rgba(99,102,241,0.4);
    color: #a5b4fc;
  }
  .tag-scope {
    background: rgba(34,197,94,0.15);
    border-color: rgba(34,197,94,0.3);
    color: #86efac;
  }
  .sidebar-body {
    flex: 1;
    overflow-y: auto;
    padding: 0.8rem 1rem;
  }
  .sidebar-section {
    margin-bottom: 1rem;
  }
  .sidebar-section h4 {
    margin: 0 0 0.4rem;
    font-size: 0.78em;
    color: #64748b;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .section-md {
    font-size: 0.82em;
    color: #cbd5e1;
    line-height: 1.5;
    word-break: break-word;
  }
  .section-md :global(h1), .section-md :global(h2), .section-md :global(h3) {
    font-size: 0.95em;
    color: #e2e8f0;
    margin: 0.6rem 0 0.3rem;
  }
  .section-md :global(p) { margin: 0.3rem 0; }
  .section-md :global(ul), .section-md :global(ol) {
    padding-left: 1.2rem;
    margin: 0.3rem 0;
  }
  .section-md :global(li) { margin: 0.15rem 0; }
  .section-md :global(input[type="checkbox"]) {
    margin-right: 0.4rem;
    accent-color: #6366f1;
  }
  .section-md :global(code) {
    background: rgba(255,255,255,0.08);
    padding: 0.1rem 0.3rem;
    border-radius: 3px;
    font-size: 0.9em;
  }
  .section-md :global(pre) {
    background: rgba(0,0,0,0.3);
    padding: 0.5rem;
    border-radius: 4px;
    overflow-x: auto;
    margin: 0.4rem 0;
  }
  .section-md :global(a) {
    color: #818cf8;
    text-decoration: none;
  }
  .section-md :global(a:hover) { text-decoration: underline; }
  .edge-link {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 8px 13px;
    min-height: 40px;
    margin-bottom: 6px;
    background: rgba(255,255,255,0.03);
    border: 1px solid rgba(255,255,255,0.06);
    border-radius: 6px;
    color: #94a3b8;
    cursor: pointer;
    font-size: 0.8125rem;
    text-align: left;
  }
  .edge-link:hover {
    background: rgba(99,102,241,0.12);
    border-color: rgba(99,102,241,0.3);
    color: #c7d2fe;
  }
  .edge-relation {
    font-size: 0.75rem;
    padding: 3px 8px;
    border-radius: 4px;
    background: rgba(255,255,255,0.06);
    color: #64748b;
    flex-shrink: 0;
  }
  .edge-title {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .edge-kind {
    font-size: 0.75rem;
    opacity: 0.5;
    flex-shrink: 0;
  }
</style>
