export interface Artifact {
	id: string;
	title: string;
	kind: string;
	status: string;
	scope: string;
}

export interface GraphData {
	nodes: Array<{ id: string; name: string; kind: string; status: string; scope: string; val: number }>;
	links: Array<{ source: string; target: string; relation: string }>;
}

export class ScribeClient {
	constructor(private baseUrl: string) {}

	private async request<T>(path: string): Promise<T> {
		const res = await fetch(`${this.baseUrl}/api/v1${path}`);
		if (!res.ok) throw new Error(`Scribe API error: ${res.status} ${await res.text()}`);
		return res.json();
	}

	fetchArtifacts(params: Record<string, string> = {}): Promise<Artifact[]> {
		const qs = new URLSearchParams(params).toString();
		return this.request(`/artifacts${qs ? '?' + qs : ''}`);
	}

	fetchGraph(params: Record<string, string> = {}): Promise<GraphData> {
		const qs = new URLSearchParams(params).toString();
		return this.request(`/graph${qs ? '?' + qs : ''}`);
	}

	fetchArtifact(id: string): Promise<Artifact> {
		return this.request(`/artifacts/${id}`);
	}
}
