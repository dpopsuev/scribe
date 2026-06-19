const GOLDEN_ANGLE = 137.508;

const KIND_ORDER = [
	'project', 'campaign', 'goal', 'task', 'bug', 'note', 'concept',
	'decision', 'spec', 'need', 'source', 'doc', 'ref', 'context',
	'journal', 'kind-group', 'ghost', 'session', 'turn', 'tool-call',
	'interface', 'test', 'config', 'template', 'rule', 'section',
];
const kindIndexMap = new Map(KIND_ORDER.map((k, i) => [k, i]));
let nextKindIndex = KIND_ORDER.length;

export function kindColor(kind: string): string {
	const short = kind?.split('.').pop() || kind || 'unknown';
	let idx = kindIndexMap.get(short);
	if (idx === undefined) {
		idx = nextKindIndex++;
		kindIndexMap.set(short, idx);
	}
	const hue = (idx * GOLDEN_ANGLE + 60) % 360;
	return oklchToHex(0.72, 0.14, hue);
}

export function oklchToHex(L: number, C: number, h: number): string {
	const hRad = h * Math.PI / 180;
	const a = C * Math.cos(hRad);
	const b = C * Math.sin(hRad);
	const l_ = L + 0.3963377774 * a + 0.2158037573 * b;
	const m_ = L - 0.1055613458 * a - 0.0638541728 * b;
	const s_ = L - 0.0894841775 * a - 1.2914855480 * b;
	const l3 = l_ * l_ * l_;
	const m3 = m_ * m_ * m_;
	const s3 = s_ * s_ * s_;
	let r = +4.0767416621 * l3 - 3.3077115913 * m3 + 0.2309699292 * s3;
	let g = -1.2684380046 * l3 + 2.6097574011 * m3 - 0.3413193965 * s3;
	let bl = -0.0041960863 * l3 - 0.7034186147 * m3 + 1.7076147010 * s3;
	const gamma = (x: number) => x <= 0.0031308 ? 12.92 * x : 1.055 * Math.pow(x, 1/2.4) - 0.055;
	r = Math.round(Math.max(0, Math.min(1, gamma(r))) * 255);
	g = Math.round(Math.max(0, Math.min(1, gamma(g))) * 255);
	bl = Math.round(Math.max(0, Math.min(1, gamma(bl))) * 255);
	return '#' + [r, g, bl].map(v => v.toString(16).padStart(2, '0')).join('');
}
