/**
 * pi-scribe — native Pi extension for the Scribe graph store.
 *
 * Three tools (scribe_artifact / scribe_graph / scribe_admin) mirror the
 * MCP surface over plain REST. A session-start widget shows work-graph
 * status; /scribe opens an interactive graph browser.
 *
 * Install: pi install git:github.com/DanyPops/pi-scribe
 * Env: SCRIBE_URL (default http://127.0.0.1:8080), SCRIBE_AUTH_TOKEN
 */
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { registerTools } from "./tools.ts";
import { showScribe } from "./tui.ts";
import { ops } from "./client.ts";

export default async function (pi: ExtensionAPI) {
	registerTools(pi);

	pi.registerCommand("scribe", {
		description: "Browse the Scribe work graph (interactive)",
		handler: async (_args, ctx) => {
			await showScribe(ctx);
		},
	});

	// Work-graph status widget above the editor — fires on session start.
	// Silent if scribe is unreachable (never blocks startup).
	pi.on("session_start", async (_event, ctx) => {
		if (!ctx.hasUI) return;
		try {
			const r = await ops("status");
			if (!r.ok || !r.text) return;
			ctx.ui.setWidget("pi-scribe", [
				ctx.ui.theme.bold("Scribe"),
				ctx.ui.theme.fg("dim", r.text.split("\n")[0]?.slice(0, 100) ?? ""),
			], { placement: "aboveEditor" });
		} catch {
			// scribe unavailable — stay silent
		}
	});
}
