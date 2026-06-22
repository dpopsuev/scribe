<script lang="ts">
	import { fetchArtifacts, type Artifact } from '$lib/api';
	import { kindColor } from '$lib/colors';
	import { onMount } from 'svelte';

	let artifacts: Artifact[] = $state([]);
	let loading = $state(true);
	let canvas: HTMLCanvasElement;
	let breadcrumb: string[] = $state([]);

	interface TreeNode {
		name: string;
		value: number;
		children: TreeNode[];
		color?: string;
		depth: number;
		startAngle?: number;
		endAngle?: number;
	}

	async function load() {
		loading = true;
		artifacts = await fetchArtifacts();
		loading = false;
	}

	$effect(() => { load(); });

	let tree = $derived.by((): TreeNode => {
		const root: TreeNode = { name: 'all', value: 0, children: [], depth: 0 };
		const scopeMap = new Map<string, TreeNode>();

		for (const a of artifacts) {
			const scope = a.scope || '(none)';
			let scopeNode = scopeMap.get(scope);
			if (!scopeNode) {
				scopeNode = { name: scope, value: 0, children: [], depth: 1 };
				scopeMap.set(scope, scopeNode);
				root.children.push(scopeNode);
			}

			const kind = a.kind || '(none)';
			let kindNode = scopeNode.children.find(c => c.name === kind);
			if (!kindNode) {
				kindNode = { name: kind, value: 0, children: [], depth: 2, color: kindColor(kind) };
				scopeNode.children.push(kindNode);
			}

			const status = a.status || '(none)';
			let statusNode = kindNode.children.find(c => c.name === status);
			if (!statusNode) {
				statusNode = { name: status, value: 0, children: [], depth: 3 };
				kindNode.children.push(statusNode);
			}
			statusNode.value++;
			kindNode.value++;
			scopeNode.value++;
			root.value++;
		}

		return root;
	});

	function layoutArcs(node: TreeNode, startAngle: number, endAngle: number) {
		node.startAngle = startAngle;
		node.endAngle = endAngle;
		if (node.children.length === 0 || node.value === 0) return;
		let angle = startAngle;
		for (const child of node.children) {
			const span = ((child.value / node.value) * (endAngle - startAngle));
			layoutArcs(child, angle, angle + span);
			angle += span;
		}
	}

	function collectArcs(node: TreeNode, arcs: TreeNode[]) {
		if (node.depth > 0) arcs.push(node);
		for (const child of node.children) collectArcs(child, arcs);
	}

	function draw() {
		if (!canvas || loading) return;
		const ctx = canvas.getContext('2d');
		if (!ctx) return;

		const w = canvas.width = canvas.clientWidth * devicePixelRatio;
		const h = canvas.height = canvas.clientHeight * devicePixelRatio;
		ctx.scale(devicePixelRatio, devicePixelRatio);
		const cw = canvas.clientWidth;
		const ch = canvas.clientHeight;

		ctx.clearRect(0, 0, cw, ch);
		const cx = cw / 2;
		const cy = ch / 2;
		const maxR = Math.min(cx, cy) - 20;
		const ringWidth = maxR / 4;

		layoutArcs(tree, 0, Math.PI * 2);
		const arcs: TreeNode[] = [];
		collectArcs(tree, arcs);

		for (const arc of arcs) {
			const innerR = (arc.depth - 1) * ringWidth + 2;
			const outerR = arc.depth * ringWidth;
			const start = arc.startAngle ?? 0;
			const end = arc.endAngle ?? 0;
			if (end - start < 0.001) continue;

			ctx.beginPath();
			ctx.arc(cx, cy, outerR, start, end);
			ctx.arc(cx, cy, innerR, end, start, true);
			ctx.closePath();

			const hue = (arc.name.charCodeAt(0) * 37 + arc.depth * 90) % 360;
			ctx.fillStyle = arc.color ?? `hsl(${hue}, 55%, ${45 + arc.depth * 8}%)`;
			ctx.fill();
			ctx.strokeStyle = 'rgba(0,0,0,0.3)';
			ctx.lineWidth = 0.5;
			ctx.stroke();

			const midAngle = (start + end) / 2;
			const midR = (innerR + outerR) / 2;
			const arcLen = (end - start) * midR;
			if (arcLen > 30) {
				ctx.save();
				ctx.translate(cx + Math.cos(midAngle) * midR, cy + Math.sin(midAngle) * midR);
				ctx.rotate(midAngle + (midAngle > Math.PI / 2 && midAngle < Math.PI * 1.5 ? Math.PI : 0));
				ctx.fillStyle = 'white';
				ctx.font = `${Math.min(11, arcLen / 8)}px sans-serif`;
				ctx.textAlign = 'center';
				ctx.textBaseline = 'middle';
				const label = arc.name.split('.').pop() ?? arc.name;
				ctx.fillText(label.length > 12 ? label.slice(0, 10) + '..' : label, 0, 0);
				ctx.restore();
			}
		}
	}

	$effect(() => { void tree; void loading; requestAnimationFrame(draw); });

	onMount(() => {
		const obs = new ResizeObserver(() => draw());
		obs.observe(canvas);
		return () => obs.disconnect();
	});
</script>

<div class="sunburst-view">
	<div class="toolbar">
		<span class="info">Scope → Kind → Status</span>
		<span class="count">{artifacts.length} artifacts</span>
	</div>
	{#if loading}
		<div class="loading">Loading...</div>
	{:else}
		<canvas bind:this={canvas} class="sunburst-canvas"></canvas>
	{/if}
</div>

<style>
	.sunburst-view {
		display: flex;
		flex-direction: column;
		height: 100%;
		padding: var(--space-3);
		gap: var(--space-3);
	}
	.toolbar { display: flex; gap: var(--space-2); align-items: center; }
	.info { font-size: 13px; color: var(--text-muted); }
	.count { font-size: 12px; color: var(--text-muted); margin-left: auto; }
	.loading { display: flex; align-items: center; justify-content: center; height: 200px; color: var(--text-muted); }
	.sunburst-canvas {
		flex: 1;
		width: 100%;
		cursor: pointer;
	}
</style>
