import type { GraphNode } from './GraphCanvas.svelte';
import { kindColor } from '$lib/colors';

interface RawNode {
	id: string;
	name: string;
	kind: string;
	val?: number;
	status?: string;
	scope?: string;
}

interface RawLink {
	source: string;
	target: string;
	relation?: string;
	weight?: number;
}

export interface GraphEdgeWithRelation {
	source: string;
	target: string;
	relation?: string;
	color: string;
}

export function layoutNodes(
	rawNodes: RawNode[],
	opts: { minSize?: number; maxSize?: number; spread?: number } = {},
): GraphNode[] {
	const n = rawNodes.length || 1;
	const minSize = opts.minSize ?? 4;
	const maxSize = opts.maxSize ?? 18;
	const spread = opts.spread ?? 12;

	const vals = rawNodes.map(r => r.val || 1);
	const minCbrt = Math.cbrt(Math.min(...vals));
	const maxCbrt = Math.cbrt(Math.max(...vals));
	const range = maxCbrt - minCbrt || 1;
	const goldenAngle = Math.PI * (3 - Math.sqrt(5));
	const spreadRadius = Math.sqrt(n) * spread;

	return rawNodes.map((raw, i) => {
		const t = (Math.cbrt(raw.val || 1) - minCbrt) / range;
		const size = minSize + (maxSize - minSize) * t;
		const angle = i * goldenAngle;
		const r = spreadRadius * Math.sqrt((i + 0.5) / n);
		return {
			id: raw.id,
			label: raw.name,
			x: r * Math.cos(angle),
			y: r * Math.sin(angle),
			size,
			color: kindColor(raw.kind),
			kind: raw.kind,
			depth: 0,
		} as GraphNode;
	});
}

export function layoutEdges(rawLinks: RawLink[]): GraphEdgeWithRelation[] {
	return rawLinks.map(raw => ({
		source: raw.source,
		target: raw.target,
		relation: raw.relation,
		color: '#5a5a7a',
	}));
}
