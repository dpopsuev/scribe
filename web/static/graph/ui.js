/**
 * ui.js — DOM interactions for the Scribe graph UI.
 *
 * Functions receive DOM element IDs or references explicitly so they can be
 * tested with jsdom. No direct window/document access at module load time.
 */

// ── Mode badge ────────────────────────────────────────────────────────────────

const DEPTH_LABELS = {
  scope:    'universe · scope view',
  kind:     'depth 1 · kind view',
  artifact: 'depth 2 · artifact view',
};

/**
 * Update the mode badge text based on current depth.
 * @param {Element} el
 * @param {'scope'|'kind'|'artifact'} depth
 */
export function setModeBadge(el, depth) {
  if (el) el.textContent = DEPTH_LABELS[depth] || depth;
}

/**
 * Derive depth label from expanded scope/kind counts.
 */
export function depthFromExpanded(expandedScopeCount, expandedKindCount) {
  if (expandedKindCount > 0) return 'artifact';
  if (expandedScopeCount > 0) return 'kind';
  return 'scope';
}

// ── Stats ─────────────────────────────────────────────────────────────────────

/**
 * Update the stats line (node count · link count).
 */
export function setStats(el, nodeCount, linkCount) {
  if (el) el.textContent = `${nodeCount} nodes · ${linkCount} links`;
}

// ── Expanded tags ─────────────────────────────────────────────────────────────

/**
 * Render the list of expanded scope/kind tags in the control panel.
 * Each tag has a click handler to collapse it.
 *
 * @param {Element} wrapEl   — #expanded-wrap
 * @param {Element} listEl   — #expanded-list
 * @param {Map} expandedScopes
 * @param {Map} expandedKinds
 * @param {function} onCollapseScope   — (scopeName) => void
 * @param {function} onCollapseKind    — (scopeName, kindName) => void
 */
export function renderExpandedTags(wrapEl, listEl, expandedScopes, expandedKinds, onCollapseScope, onCollapseKind) {
  if (!listEl || !wrapEl) return;
  listEl.innerHTML = '';
  const total = expandedScopes.size + expandedKinds.size;
  wrapEl.style.display = total === 0 ? 'none' : 'block';

  for (const [sc] of expandedScopes) {
    const tag = document.createElement('span');
    tag.className = 'expanded-tag';
    tag.innerHTML = `${sc} <span style="opacity:0.6">✕</span>`;
    tag.onclick = () => onCollapseScope(sc);
    listEl.appendChild(tag);
  }

  for (const [key] of expandedKinds) {
    const [sc, kind] = key.split(':');
    const tag = document.createElement('span');
    tag.className = 'expanded-tag';
    tag.style.cssText = 'background:rgba(99,102,241,0.2);border-color:rgba(99,102,241,0.4);color:#c7d2fe';
    tag.innerHTML = `${sc}/${kind} <span style="opacity:0.6">✕</span>`;
    tag.onclick = () => onCollapseKind(sc, kind);
    listEl.appendChild(tag);
  }
}

// ── Sidebar ───────────────────────────────────────────────────────────────────

/**
 * Open the HTMX sidebar for a given artifact ID.
 * Fetches the fragment and injects it; calls htmx.process if available.
 */
export async function openSidebar(sidebarEl, contentEl, fetch, htmx, id, baseURL = '') {
  if (!sidebarEl || !contentEl) return;
  contentEl.innerHTML = '<p class="htmx-indicator">Loading…</p>';
  sidebarEl.classList.add('open');
  try {
    const res = await fetch(`${baseURL}/fragments/artifacts/${encodeURIComponent(id)}`);
    const html = await res.text();
    contentEl.innerHTML = html;
    if (htmx) htmx.process(contentEl);
  } catch {
    contentEl.innerHTML = `<p>Could not load ${id}</p>`;
  }
}

export function closeSidebar(sidebarEl) {
  sidebarEl?.classList.remove('open');
}

// ── Context menu ──────────────────────────────────────────────────────────────

/**
 * Show a context menu at (x, y) with the given items.
 * @param {Element} menuEl
 * @param {number} x
 * @param {number} y
 * @param {Array<{label:string, action:function}|{sep:true}>} items
 */
export function showContextMenu(menuEl, x, y, items) {
  if (!menuEl) return;
  menuEl.innerHTML = '';
  for (const item of items) {
    if (item.sep) {
      const sep = document.createElement('div');
      sep.className = 'ctx-sep';
      menuEl.appendChild(sep);
    } else {
      const el = document.createElement('div');
      el.className = 'ctx-item';
      el.textContent = item.label;
      el.onclick = () => { item.action(); menuEl.style.display = 'none'; };
      menuEl.appendChild(el);
    }
  }
  menuEl.style.display = 'block';
  menuEl.style.left = `${x}px`;
  menuEl.style.top = `${y}px`;
  setTimeout(() => {
    document.addEventListener('click', () => { menuEl.style.display = 'none'; }, { once: true });
  }, 10);
}

// ── Controls wiring ───────────────────────────────────────────────────────────

/**
 * Wire relation toggle buttons — clicking one toggles its active state.
 * Returns a getter function that returns the currently active relations.
 */
export function wireRelationToggles(containerEl) {
  const toggles = containerEl?.querySelectorAll?.('.rel-toggle') ?? [];
  for (const el of toggles) {
    el.addEventListener('click', () => el.classList.toggle('active'));
  }
  return () => [...toggles].filter(el => el.classList.contains('active')).map(el => el.dataset.rel);
}
