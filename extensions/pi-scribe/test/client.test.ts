import { describe, it, expect, beforeAll, afterAll } from "bun:test";
import { ops, opsText, scribeUrl, scribeToken } from "../src/client.ts";

// Fake scribe facade: validates that the client sends the right shape
// and returns the facade's response.
let server: ReturnType<typeof Bun.serve>;
const received: { action: string; auth?: string; body: Record<string, unknown> }[] = [];

beforeAll(() => {
	server = Bun.serve({
		port: 0,
		async fetch(req) {
			const auth = req.headers.get("authorization") ?? undefined;
			const url = new URL(req.url);
			if (url.pathname === "/api/v1/ops" && req.method === "POST") {
				const body = (await req.json()) as Record<string, unknown>;
				received.push({ action: body["action"] as string, auth, body });
				return Response.json({ ok: true, text: "created fake-task", data: { id: "fake-task" } });
			}
			return new Response("nf", { status: 404 });
		},
	});
	process.env["SCRIBE_URL"] = `http://127.0.0.1:${server.port}`;
});

afterAll(() => server.stop(true));

describe("scribe client", () => {
	it("ops sends action + input, returns response", async () => {
		const r = await ops("create", { kind: "effort.task", title: "Test" });
		expect(r.ok).toBe(true);
		expect(r.text).toBe("created fake-task");
		expect(r.data).toMatchObject({ id: "fake-task" });
		expect(received.at(-1)?.action).toBe("create");
		expect(received.at(-1)?.body["kind"]).toBe("effort.task");
	});

	it("ops sends auth header when configured", async () => {
		process.env["SCRIBE_AUTH_TOKEN"] = "secret-token";
		await ops("status");
		expect(received.at(-1)?.auth).toBe("Bearer secret-token");
		delete process.env["SCRIBE_AUTH_TOKEN"];
	});

	it("opsText throws on failure", async () => {
		const old = process.env["SCRIBE_URL"];
		process.env["SCRIBE_URL"] = "http://127.0.0.1:1";
		await expect(opsText("status")).rejects.toThrow();
		process.env["SCRIBE_URL"] = old;
	});

	it("ops returns ok:false on HTTP error", async () => {
		const bad = Bun.serve({
			port: 0,
			fetch: () => new Response("err", { status: 500 }),
		});
		const old = process.env["SCRIBE_URL"];
		process.env["SCRIBE_URL"] = `http://127.0.0.1:${bad.port}`;
		const r = await ops("status");
		expect(r.ok).toBe(false);
		expect(r.error).toContain("500");
		process.env["SCRIBE_URL"] = old;
		bad.stop(true);
	});
});
