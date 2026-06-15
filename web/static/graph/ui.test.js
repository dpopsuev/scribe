import { describe, it, expect, vi } from 'vitest';
import {
  setModeBadge, depthFromExpanded, setStats,
  renderExpandedTags, closeSidebar, showContextMenu,
} from './ui.js';

// ── depthFromExpanded ─────────────────────────────────────────────────────────

describe('depthFromExpanded', () => {
  it('no expansions → scope', () => expect(depthFromExpanded(0, 0)).toBe('project'));
  it('scope expanded → kind', () => expect(depthFromExpanded(1, 0)).toBe('kind'));
  it('kind expanded → artifact', () => expect(depthFromExpanded(0, 1)).toBe('artifact'));
  it('kind takes priority over scope', () => expect(depthFromExpanded(1, 1)).toBe('artifact'));
});

// ── setModeBadge ──────────────────────────────────────────────────────────────

describe('setModeBadge', () => {
  it('sets textContent to scope label', () => {
    const el = { textContent: '' };
    setModeBadge(el, 'project');
    expect(el.textContent).toBe('universe · project view');
  });
  it('sets kind label', () => {
    const el = { textContent: '' };
    setModeBadge(el, 'kind');
    expect(el.textContent).toBe('depth 1 · kind view');
  });
  it('sets artifact label', () => {
    const el = { textContent: '' };
    setModeBadge(el, 'artifact');
    expect(el.textContent).toBe('depth 2 · artifact view');
  });
  it('handles null element gracefully', () => {
    expect(() => setModeBadge(null, 'project')).not.toThrow();
  });
});

// ── setStats ──────────────────────────────────────────────────────────────────

describe('setStats', () => {
  it('formats node and link count', () => {
    const el = { textContent: '' };
    setStats(el, 85, 95);
    expect(el.textContent).toBe('85 nodes · 95 links');
  });
  it('handles null gracefully', () => {
    expect(() => setStats(null, 0, 0)).not.toThrow();
  });
});

// ── renderExpandedTags ────────────────────────────────────────────────────────

describe('renderExpandedTags', () => {
  function makeEl() {
    const children = [];
    return {
      innerHTML: '',
      style: { display: '' },
      appendChild: c => children.push(c),
      _children: children,
    };
  }

  it('hides wrap when nothing expanded', () => {
    const wrap = makeEl(), list = makeEl();
    renderExpandedTags(wrap, list, new Map(), new Map(), vi.fn(), vi.fn());
    expect(wrap.style.display).toBe('none');
  });

  it('shows wrap when scope is expanded', () => {
    const wrap = makeEl(), list = makeEl();
    const scopes = new Map([['alpha', {}]]);
    renderExpandedTags(wrap, list, scopes, new Map(), vi.fn(), vi.fn());
    expect(wrap.style.display).toBe('block');
  });

  it('creates one tag per expanded scope', () => {
    const wrap = makeEl(), list = makeEl();
    const scopes = new Map([['alpha', {}], ['beta', {}]]);
    renderExpandedTags(wrap, list, scopes, new Map(), vi.fn(), vi.fn());
    expect(list._children).toHaveLength(2);
  });

  it('scope tag onclick calls onCollapseScope', () => {
    const wrap = makeEl(), list = makeEl();
    const onCollapse = vi.fn();
    const scopes = new Map([['alpha', {}]]);
    renderExpandedTags(wrap, list, scopes, new Map(), onCollapse, vi.fn());
    list._children[0].onclick();
    expect(onCollapse).toHaveBeenCalledWith('alpha');
  });

  it('kind tag onclick calls onCollapseKind with scope+kind', () => {
    const wrap = makeEl(), list = makeEl();
    const onCollapseKind = vi.fn();
    const kinds = new Map([['scribe:task', {}]]);
    renderExpandedTags(wrap, list, new Map(), kinds, vi.fn(), onCollapseKind);
    list._children[0].onclick();
    expect(onCollapseKind).toHaveBeenCalledWith('scribe', 'task');
  });
});

// ── closeSidebar ──────────────────────────────────────────────────────────────

describe('closeSidebar', () => {
  it('removes open class', () => {
    const el = { classList: { remove: vi.fn() } };
    closeSidebar(el);
    expect(el.classList.remove).toHaveBeenCalledWith('open');
  });
  it('handles null gracefully', () => {
    expect(() => closeSidebar(null)).not.toThrow();
  });
});

// ── showContextMenu ───────────────────────────────────────────────────────────

describe('showContextMenu', () => {
  function makeMenu() {
    const children = [];
    return {
      innerHTML: '',
      style: { display: '', left: '', top: '' },
      appendChild: c => children.push(c),
      _children: children,
    };
  }

  it('sets display block and position', () => {
    const menu = makeMenu();
    showContextMenu(menu, 100, 200, []);
    expect(menu.style.display).toBe('block');
    expect(menu.style.left).toBe('100px');
    expect(menu.style.top).toBe('200px');
  });

  it('creates one element per non-sep item', () => {
    const menu = makeMenu();
    const action = vi.fn();
    showContextMenu(menu, 0, 0, [
      { label: 'Open', action },
      { sep: true },
      { label: 'Delete', action },
    ]);
    expect(menu._children).toHaveLength(3);
  });

  it('item onclick triggers action and hides menu', () => {
    const menu = makeMenu();
    const action = vi.fn();
    showContextMenu(menu, 0, 0, [{ label: 'Click me', action }]);
    menu._children[0].onclick();
    expect(action).toHaveBeenCalledOnce();
    expect(menu.style.display).toBe('none');
  });

  it('handles null menu gracefully', () => {
    expect(() => showContextMenu(null, 0, 0, [])).not.toThrow();
  });
});
