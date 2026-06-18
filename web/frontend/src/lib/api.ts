const BASE = '/api/v1';

export class ApiError extends Error {
	constructor(public status: number, message: string) {
		super(message);
	}
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
	const res = await fetch(`${BASE}${path}`, init);
	if (!res.ok) throw new ApiError(res.status, await res.text());
	return res.json();
}

export interface Artifact {
	id: string;
	title: string;
	kind: string;
	status: string;
	scope: string;
	score: number;
}

export interface Scope {
	scope: string;
	count: number;
}

export function fetchArtifacts(params: Record<string, string> = {}): Promise<Artifact[]> {
	return request(`/artifacts?${new URLSearchParams(params)}`);
}

export function fetchScopes(): Promise<Scope[]> {
	return request('/scopes');
}

export interface ArtifactDetail {
	id: string;
	title: string;
	labels: string[];
	sections: { name: string; text: string }[];
	created_at: string;
	updated_at: string;
}

export interface Edge {
	from: string;
	to: string;
	relation: string;
	title: string;
	kind: string;
}

export function fetchArtifact(id: string): Promise<ArtifactDetail> {
	return request(`/artifacts/${id}`);
}

export function fetchEdges(id: string): Promise<Edge[]> {
	return request(`/artifacts/${id}/edges`);
}

export interface LensInfo {
	id: string;
	title: string;
}

export function fetchLenses(): Promise<LensInfo[]> {
	return request('/lenses');
}

export interface GraphData {
	nodes: Array<{
		id: string;
		name: string;
		kind: string;
		status: string;
		scope: string;
		val: number;
	}>;
	links: Array<{
		source: string;
		target: string;
		relation: string;
		weight?: number;
	}>;
}

export function fetchLensGraph(params: Record<string, string>): Promise<GraphData> {
	return request(`/graph/lens?${new URLSearchParams(params)}`);
}

export interface LensCreateInput {
	title: string;
	scope?: string;
	anchor?: string[];
	anchor_or?: string[];
	traverse?: Array<{ relation: string; direction: string; max_depth: number }>;
	exclude?: string[];
	include?: string[];
	max_depth?: number;
	score_by?: string;
}

export function createLens(input: LensCreateInput): Promise<{ result: string }> {
	return request('/lenses', {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(input),
	});
}

export function patchStatus(id: string, status: string): Promise<void> {
	return request(`/artifacts/${id}`, {
		method: 'PATCH',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ status })
	});
}
