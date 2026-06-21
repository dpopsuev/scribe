// shapes.ts — Shape ID mapping from artifact kind to geometric shape.
//
// Psychology-backed mapping (verified via deep-research workflow):
//   Circle (0)     — approachable, complete, unity → knowledge
//   Square (1)     — stability, structure, foundation → code
//   Diamond (2)    — decision, evaluation, branching → intent (decisions)
//   Triangle (3)   — attention, urgency, warning → intent (bugs)
//   Hexagon (4)    — preparation, setup, interconnection → support
//   Pentagon (5)   — authority, institutional → effort
//   Star (6)       — significance, investigation → investigation
//   Rounded rect (7) — container, terminal → meta (project, kind-group)

export const SHAPE_CIRCLE = 0;
export const SHAPE_SQUARE = 1;
export const SHAPE_DIAMOND = 2;
export const SHAPE_TRIANGLE = 3;
export const SHAPE_HEXAGON = 4;
export const SHAPE_PENTAGON = 5;
export const SHAPE_STAR = 6;
export const SHAPE_ROUNDED_RECT = 7;

const kindShapeMap: Record<string, number> = {
	// Knowledge domain — circle (approachable, complete)
	'note': SHAPE_CIRCLE,
	'concept': SHAPE_CIRCLE,
	'source': SHAPE_CIRCLE,
	'journal': SHAPE_CIRCLE,
	'context': SHAPE_CIRCLE,

	// Code domain — square (stable, structural)
	'file': SHAPE_SQUARE,
	'struct': SHAPE_SQUARE,
	'interface': SHAPE_SQUARE,
	'function': SHAPE_SQUARE,
	'method': SHAPE_SQUARE,
	'test': SHAPE_SQUARE,

	// Intent domain — diamond (decision/evaluation) or triangle (bug/urgency)
	'decision': SHAPE_DIAMOND,
	'spec': SHAPE_DIAMOND,
	'need': SHAPE_DIAMOND,
	'bug': SHAPE_TRIANGLE,

	// Support domain — hexagon (preparation, setup)
	'doc': SHAPE_HEXAGON,
	'config': SHAPE_HEXAGON,
	'template': SHAPE_HEXAGON,
	'rule': SHAPE_HEXAGON,
	'ref': SHAPE_HEXAGON,
	'section': SHAPE_HEXAGON,

	// Effort domain — pentagon (authority, hierarchy)
	'campaign': SHAPE_PENTAGON,
	'goal': SHAPE_PENTAGON,
	'task': SHAPE_PENTAGON,

	// Investigation domain — star (significance, spotlight)
	'case': SHAPE_STAR,
	'observation': SHAPE_STAR,
	'cause': SHAPE_STAR,
	'investigation': SHAPE_STAR,

	// Meta — rounded rect (container)
	'project': SHAPE_ROUNDED_RECT,
	'kind-group': SHAPE_ROUNDED_RECT,
	'session': SHAPE_ROUNDED_RECT,
	'turn': SHAPE_CIRCLE,
	'tool-call': SHAPE_CIRCLE,
	'ghost': SHAPE_CIRCLE,
};

export function kindShape(kind: string): number {
	const short = kind?.split('.').pop() || kind || 'unknown';
	return kindShapeMap[short] ?? SHAPE_CIRCLE;
}
