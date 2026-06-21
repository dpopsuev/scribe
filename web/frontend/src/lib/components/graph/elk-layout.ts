import ELK, { type ElkNode, type ElkExtendedEdge, type ElkPort } from 'elkjs/lib/elk.bundled.js';
import { kindColor } from '$lib/colors';

export interface SchematicNode {
	id: string;
	label: string;
	x: number;
	y: number;
	width: number;
	height: number;
	color: string;
	kind: string;
	isInterface: boolean;
	layer: number;
	ports: SchematicPort[];
}

export interface SchematicPort {
	id: string;
	side: 'WEST' | 'EAST';
	x: number;
	y: number;
}

export interface SchematicEdge {
	id: string;
	source: string;
	target: string;
	relation: string;
	points: { x: number; y: number }[];
	sameLayer: boolean;
	label?: string;
}

export interface ContainmentBox {
	id: string;
	label: string;
	x: number;
	y: number;
	width: number;
	height: number;
}

export interface SchematicLayout {
	nodes: SchematicNode[];
	edges: SchematicEdge[];
	containers: ContainmentBox[];
	width: number;
	height: number;
}

interface RawNode {
	id: string;
	name: string;
	kind: string;
	scope?: string;
	val?: number;
}

interface RawLink {
	source: string;
	target: string;
	relation?: string;
	weight?: number;
}

const PORT_SPACING = 14;
const NODE_PAD_X = 20;
const MIN_NODE_W = 110;
const BASE_NODE_H = 32;

export async function computeSchematicLayout(
	rawNodes: RawNode[],
	rawLinks: RawLink[],
): Promise<SchematicLayout> {
	const elk = new ELK();

	const nodeIdSet = new Set(rawNodes.map(n => n.id));
	const validLinks = rawLinks.filter(l => nodeIdSet.has(l.source) && nodeIdSet.has(l.target));

	// Count incoming/outgoing edges per node to build ports
	const inEdges = new Map<string, string[]>();
	const outEdges = new Map<string, string[]>();
	for (const link of validLinks) {
		if (!inEdges.has(link.target)) inEdges.set(link.target, []);
		inEdges.get(link.target)!.push(link.source);
		if (!outEdges.has(link.source)) outEdges.set(link.source, []);
		outEdges.get(link.source)!.push(link.target);
	}

	// Group nodes by scope for containment
	const byScope = new Map<string, RawNode[]>();
	for (const node of rawNodes) {
		const scope = node.scope || '';
		if (!byScope.has(scope)) byScope.set(scope, []);
		byScope.get(scope)!.push(node);
	}
	const useContainers = byScope.size > 1 || (byScope.size === 1 && !byScope.has(''));

	function makeElkNode(node: RawNode): ElkNode {
		const inCount = inEdges.get(node.id)?.length || 0;
		const outCount = outEdges.get(node.id)?.length || 0;
		const maxPorts = Math.max(inCount, outCount);

		const label = node.name || node.id;
		const width = Math.max(MIN_NODE_W, label.length * 7.5 + NODE_PAD_X * 2);
		const height = Math.max(BASE_NODE_H, maxPorts * PORT_SPACING + 8);

		const ports: ElkPort[] = [];
		const ins = inEdges.get(node.id) || [];
		for (let i = 0; i < ins.length; i++) {
			ports.push({
				id: `${node.id}:in:${i}`,
				width: 6,
				height: 6,
				layoutOptions: { 'port.side': 'WEST', 'port.index': `${i}` },
			});
		}
		const outs = outEdges.get(node.id) || [];
		for (let i = 0; i < outs.length; i++) {
			ports.push({
				id: `${node.id}:out:${i}`,
				width: 6,
				height: 6,
				layoutOptions: { 'port.side': 'EAST', 'port.index': `${i}` },
			});
		}

		return {
			id: node.id,
			labels: [{ text: label, width: label.length * 7.5, height: 14 }],
			width,
			height,
			ports,
			layoutOptions: {
				'elk.portConstraints': 'FIXED_SIDE',
			},
		};
	}

	// Build top-level children
	const children: ElkNode[] = [];
	for (const [scope, scopeNodes] of byScope) {
		if (!useContainers || !scope) {
			for (const node of scopeNodes) {
				children.push(makeElkNode(node));
			}
		} else {
			children.push({
				id: `scope:${scope}`,
				labels: [{ text: scope, width: scope.length * 7.5, height: 14 }],
				layoutOptions: {
					'elk.padding': '[top=28,left=12,bottom=12,right=12]',
					'elk.algorithm': 'layered',
					'elk.direction': 'DOWN',
					'elk.edgeRouting': 'ORTHOGONAL',
					'elk.layered.crossingMinimization.strategy': 'LAYER_SWEEP',
					'elk.portConstraints': 'FIXED_SIDE',
				},
				children: scopeNodes.map(makeElkNode),
			});
		}
	}

	// Map edge endpoints to correct port IDs
	const sourcePortCounter = new Map<string, number>();
	const targetPortCounter = new Map<string, number>();

	const edges: ElkExtendedEdge[] = validLinks.map((l, i) => {
		const si = sourcePortCounter.get(l.source) || 0;
		sourcePortCounter.set(l.source, si + 1);
		const ti = targetPortCounter.get(l.target) || 0;
		targetPortCounter.set(l.target, ti + 1);

		const edge: ElkExtendedEdge = {
			id: `e${i}`,
			sources: [`${l.source}:out:${si}`],
			targets: [`${l.target}:in:${ti}`],
		};
		if (l.relation === 'field_ref') {
			edge.labels = [{ text: 'field_ref', width: 44, height: 10 }];
		}
		return edge;
	});

	const graph: ElkNode = {
		id: 'root',
		layoutOptions: {
			'elk.algorithm': 'layered',
			'elk.direction': 'DOWN',
			'elk.layered.crossingMinimization.strategy': 'LAYER_SWEEP',
			'elk.edgeRouting': 'ORTHOGONAL',
			'elk.spacing.nodeNode': '22',
			'elk.layered.spacing.nodeNodeBetweenLayers': '40',
			'elk.spacing.edgeNode': '12',
			'elk.spacing.edgeEdge': '8',
			'elk.hierarchyHandling': 'INCLUDE_CHILDREN',
			'elk.portConstraints': 'FIXED_SIDE',
			'elk.layered.nodePlacement.strategy': 'NETWORK_SIMPLEX',
		},
		children,
		edges,
	};

	const result = await elk.layout(graph);
	return extractLayout(result, rawNodes, validLinks);
}

function extractLayout(
	elkResult: ElkNode,
	rawNodes: RawNode[],
	rawLinks: RawLink[],
): SchematicLayout {
	const nodes: SchematicNode[] = [];
	const containers: ContainmentBox[] = [];
	const rawMap = new Map(rawNodes.map(n => [n.id, n]));
	const yByNode = new Map<string, number>();

	function visit(elkNode: ElkNode, ox = 0, oy = 0) {
		for (const child of elkNode.children || []) {
			const cx = (child.x || 0) + ox;
			const cy = (child.y || 0) + oy;

			if (child.id.startsWith('scope:')) {
				containers.push({
					id: child.id,
					label: child.labels?.[0]?.text || child.id.replace('scope:', ''),
					x: cx,
					y: cy,
					width: child.width || 200,
					height: child.height || 100,
				});
				visit(child, cx, cy);
				continue;
			}

			const raw = rawMap.get(child.id);
			const kind = raw?.kind || '';
			const shortKind = kind.split('.').pop() || kind;

			const ports: SchematicPort[] = (child.ports || []).map(p => ({
				id: p.id || '',
				side: (p.id?.includes(':in:') ? 'WEST' : 'EAST') as 'WEST' | 'EAST',
				x: cx + (p.x || 0),
				y: cy + (p.y || 0),
			}));

			yByNode.set(child.id, cy);
			nodes.push({
				id: child.id,
				label: child.labels?.[0]?.text || child.id,
				x: cx,
				y: cy,
				width: child.width || MIN_NODE_W,
				height: child.height || BASE_NODE_H,
				color: kindColor(kind),
				kind,
				isInterface: shortKind === 'interface',
				layer: Math.round(cy),
				ports,
			});
		}
	}
	visit(elkResult);

	const edges: SchematicEdge[] = [];

	function visitEdges(elkNode: ElkNode, ox = 0, oy = 0) {
		for (const edge of elkNode.edges || []) {
			const points: { x: number; y: number }[] = [];
			for (const section of edge.sections || []) {
				points.push({
					x: section.startPoint.x + ox,
					y: section.startPoint.y + oy,
				});
				for (const bp of section.bendPoints || []) {
					points.push({ x: bp.x + ox, y: bp.y + oy });
				}
				points.push({
					x: section.endPoint.x + ox,
					y: section.endPoint.y + oy,
				});
			}

			// Resolve source/target from port IDs back to node IDs
			const srcPort = edge.sources[0] || '';
			const tgtPort = edge.targets[0] || '';
			const source = srcPort.replace(/:out:\d+$/, '');
			const target = tgtPort.replace(/:in:\d+$/, '');

			const rawLink = rawLinks.find(l => l.source === source && l.target === target);
			const relation = rawLink?.relation || '';

			const sy = yByNode.get(source) || 0;
			const ty = yByNode.get(target) || 0;
			const sameLayer = Math.abs(sy - ty) < 5;

			edges.push({
				id: edge.id || `e-${source}-${target}`,
				source,
				target,
				relation,
				points: points.length > 0 ? points : fallbackLine(source, target, nodes),
				sameLayer,
				label: relation === 'field_ref' ? 'field_ref' : undefined,
			});
		}

		for (const child of elkNode.children || []) {
			if (child.id.startsWith('scope:')) {
				visitEdges(child, (child.x || 0) + ox, (child.y || 0) + oy);
			}
		}
	}
	visitEdges(elkResult);

	return {
		nodes,
		edges,
		containers,
		width: elkResult.width || 800,
		height: elkResult.height || 600,
	};
}

function fallbackLine(
	sourceId: string,
	targetId: string,
	nodes: SchematicNode[],
): { x: number; y: number }[] {
	const s = nodes.find(n => n.id === sourceId);
	const t = nodes.find(n => n.id === targetId);
	if (!s || !t) return [];
	return [
		{ x: s.x + s.width / 2, y: s.y + s.height / 2 },
		{ x: t.x + t.width / 2, y: t.y + t.height / 2 },
	];
}
