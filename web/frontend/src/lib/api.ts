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

export function patchStatus(id: string, status: string): Promise<void> {
	return request(`/artifacts/${id}`, {
		method: 'PATCH',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ status })
	});
}
