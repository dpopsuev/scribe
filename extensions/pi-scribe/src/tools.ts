/**
 * tools.ts — three native tools mirroring Scribe's MCP surface.
 * Each takes an `action` verb + domain fields; the REST facade dispatches
 * through service.Find — same contract as MCP and CLI.
 */
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";
import { ops } from "./client.ts";

function text(t: string, details: Record<string, unknown> = {}) {
	return { content: [{ type: "text" as const, text: t }], details };
}

export function registerTools(pi: ExtensionAPI): void {
	pi.registerTool({
		name: "scribe_artifact",
		label: "Scribe Artifact",
		description:
			"Scribe artifact operations — create, get, query, set, update, delete on the typed graph store. " +
			"ACTIONS: create (needs kind+title), get (needs id), query (filter by kind/scope/labels/query/sort), " +
			"set (field update), update, delete, recent, brief, schema, help, claim, release, handoff, " +
			"comment_add, comment_list, message_add, message_list, export. " +
			"KINDS: effort.{campaign,goal,task}, intent.{bug,decision,need,spec}, knowledge.{note,concept,context}, " +
			"code.{file,function,method,struct,interface,test}, delivery.{commit,build,deployment}, " +
			"investigation.{case,cause,observation}, agent.{session,turn,memory}, test.{run,suite,result}, " +
			"support.{doc,config,template}. " +
			"STATUSES: work.draft, work.active, work.complete, status:archived, decision.accepted.",
		parameters: Type.Object({
			action: Type.String({
				description: "create|get|query|set|update|delete|recent|brief|schema|help|claim|release|handoff|comment_add|comment_list|export",
			}),
			id: Type.Optional(Type.String({ description: "artifact ID (slug)" })),
			kind: Type.Optional(Type.String({ description: "e.g. effort.task, intent.decision (short form ok)" })),
			title: Type.Optional(Type.String()),
			scope: Type.Optional(Type.String({ description: "project scope" })),
			status: Type.Optional(Type.String({ description: "work.draft, work.active, work.complete, status:archived" })),
			labels: Type.Optional(Type.Array(Type.String())),
			query: Type.Optional(Type.String({ description: "full-text search across title/goal/sections" })),
			title_contains: Type.Optional(Type.String()),
			sort: Type.Optional(Type.String({ description: "id|title|status|scope|kind|sprint|priority|topo" })),
			limit: Type.Optional(Type.Number({ description: "max results (default varies)" })),
			cursor: Type.Optional(Type.String({ description: "pagination cursor from previous query" })),
			parent: Type.Optional(Type.String({ description: "parent artifact ID" })),
			depends_on: Type.Optional(Type.Array(Type.String())),
			goal: Type.Optional(Type.String({ description: "goal statement (create)" })),
			priority: Type.Optional(Type.String({ description: "none|low|medium|high|critical" })),
			format: Type.Optional(Type.String({ description: "summary or full" })),
			fields: Type.Optional(Type.Array(Type.String())),
		}),
		async execute(_id, params, _signal, _onUpdate, _ctx) {
			const { action, ...input } = params;
			try {
				const r = await ops(action, input);
				if (!r.ok) return text(`scribe ${action} failed: ${r.error}`);
				return text(r.text ?? "(no output)", { data: r.data });
			} catch (e) {
				return text(`scribe_artifact error: ${e instanceof Error ? e.message : e}`);
			}
		},
	});

	pi.registerTool({
		name: "scribe_graph",
		label: "Scribe Graph",
		description:
			"Scribe graph operations — edges and analysis on the artifact DAG. " +
			"ACTIONS: link (add edges), unlink (remove), analyze (fan-in/out, pagerank, co_citation, paths, coupling), synonym. " +
			"RELATIONS: parent_of, depends_on, follows, justifies, governed_by, implements, documents, blocks, " +
			"duplicates, relates_to, mentions, discusses, tested_by, supersedes, cites, elaborates, causes, resolves.",
		parameters: Type.Object({
			action: Type.String({ description: "link|unlink|analyze|synonym" }),
			id: Type.Optional(Type.String({ description: "source artifact ID" })),
			relation: Type.Optional(Type.String({ description: "edge type: depends_on, parent_of, blocks, etc." })),
			targets: Type.Optional(Type.Array(Type.String(), { description: "target IDs for link" })),
			edges: Type.Optional(Type.Array(
				Type.Object({
					from: Type.String(),
					relation: Type.String(),
					to: Type.String(),
				}),
				{ description: "bulk: [{from, relation, to}]" },
			)),
			mode: Type.Optional(Type.String({ description: "link: unlink; analyze: fan|pagerank|co_citation|paths|coupling" })),
			from: Type.Optional(Type.String({ description: "analyze paths: source" })),
			to: Type.Optional(Type.String({ description: "analyze paths: target" })),
			depth: Type.Optional(Type.Number({ description: "tree/briefing max depth" })),
			direction: Type.Optional(Type.String({ description: "outbound (default) or inbound" })),
			alias: Type.Optional(Type.String({ description: "synonym: alias to register" })),
			term: Type.Optional(Type.String({ description: "synonym: term to resolve" })),
		}),
		async execute(_id, params, _signal, _onUpdate, _ctx) {
			const { action, ...input } = params;
			try {
				const r = await ops(action, input);
				if (!r.ok) return text(`scribe ${action} failed: ${r.error}`);
				return text(r.text ?? "(no output)", { data: r.data });
			} catch (e) {
				return text(`scribe_graph error: ${e instanceof Error ? e.message : e}`);
			}
		},
	});

	pi.registerTool({
		name: "scribe_admin",
		label: "Scribe Admin",
		description:
			"Scribe admin operations — ops and introspection on the work graph. " +
			"ACTIONS: status (server version, DB size, scopes), dashboard (campaign health), " +
			"triage (stale work, orphans), hygiene (zombie campaigns, stale tasks), " +
			"history (change log for artifact), changelog (field-level diffs), " +
			"lint (consistency checks), synthesize (auto-generate note with citations).",
		parameters: Type.Object({
			action: Type.String({ description: "status|dashboard|triage|hygiene|history|changelog|lint|synthesize" }),
			id: Type.Optional(Type.String({ description: "artifact ID (history, changelog, lint, synthesize)" })),
			scope: Type.Optional(Type.String({ description: "project scope (dashboard, triage, hygiene)" })),
			format: Type.Optional(Type.String({ description: "summary or full" })),
			limit: Type.Optional(Type.Number()),
			dry_run: Type.Optional(Type.Boolean()),
		}),
		async execute(_id, params, _signal, _onUpdate, _ctx) {
			const { action, ...input } = params;
			try {
				const r = await ops(action, input);
				if (!r.ok) return text(`scribe ${action} failed: ${r.error}`);
				return text(r.text ?? "(no output)", { data: r.data });
			} catch (e) {
				return text(`scribe_admin error: ${e instanceof Error ? e.message : e}`);
			}
		},
	});
}
