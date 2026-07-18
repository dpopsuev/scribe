/**
 * client.ts — REST client for the Scribe service.
 * One endpoint: POST /api/v1/ops {action, ...input} → {ok, text, data, error}
 * The facade dispatches every verb through service.Find — zero per-verb
 * client logic, same lock-step contract as MCP/CLI.
 *
 * Env read at CALL time (not import time) so tests can reconfigure.
 */
export function scribeUrl(): string {
	return process.env["SCRIBE_URL"] ?? "http://127.0.0.1:8080";
}
export function scribeToken(): string {
	return process.env["SCRIBE_AUTH_TOKEN"] ?? "";
}
export function scribeTimeoutMs(): number {
	return Number(process.env["SCRIBE_TIMEOUT_MS"] ?? 15_000);
}

export interface OpsResponse {
	ok: boolean;
	text?: string;
	data?: unknown;
	error?: string;
}

export async function ops(action: string, input: Record<string, unknown> = {}): Promise<OpsResponse> {
	try {
		const res = await fetch(`${scribeUrl()}/api/v1/ops`, {
			method: "POST",
			headers: {
				"content-type": "application/json",
				...(scribeToken() ? { authorization: `Bearer ${scribeToken()}` } : {}),
			},
			body: JSON.stringify({ action, ...input }),
			signal: AbortSignal.timeout(scribeTimeoutMs()),
		});
		if (!res.ok) return { ok: false, error: `scribe HTTP ${res.status}` };
		return (await res.json()) as OpsResponse;
	} catch (e) {
		return { ok: false, error: e instanceof Error ? e.message : String(e) };
	}
}

export async function opsText(action: string, input: Record<string, unknown> = {}): Promise<string> {
	const r = await ops(action, input);
	if (!r.ok) throw new Error(r.error ?? "scribe operation failed");
	return r.text ?? "";
}
