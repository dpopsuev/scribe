const BASE = '/api/v1';

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

export async function fetchArtifacts(params: Record<string, string> = {}): Promise<Artifact[]> {
	const qs = new URLSearchParams(params);
	const res = await fetch(`${BASE}/artifacts?${qs}`);
	if (!res.ok) throw new Error(await res.text());
	return res.json();
}

export async function fetchScopes(): Promise<Scope[]> {
	const res = await fetch(`${BASE}/scopes`);
	if (!res.ok) throw new Error(await res.text());
	return res.json();
}

export async function patchStatus(id: string, status: string): Promise<void> {
	const res = await fetch(`${BASE}/artifacts/${id}`, {
		method: 'PATCH',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ status })
	});
	if (!res.ok) throw new Error(await res.text());
}
