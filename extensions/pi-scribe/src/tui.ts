/**
 * tui.ts — /scribe interactive graph browser.
 * Lists artifacts (query), type-to-filter, Enter → show details.
 * Follows the pi-extension-manager / pi-packed TUI idiom.
 */
import type { ExtensionCommandContext } from "@earendil-works/pi-coding-agent";
import { DynamicBorder, rawKeyHint } from "@earendil-works/pi-coding-agent";
import { Container, Input, Spacer, truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";
import { ops, opsText } from "./client.ts";

interface ArtifactRow {
	id: string;
	kind: string;
	title: string;
	status: string;
}

async function loadArtifacts(): Promise<{ rows: ArtifactRow[]; error?: string }> {
	try {
		const text = await opsText("query", { limit: 50 });
		const rows: ArtifactRow[] = [];
		for (const line of text.split("\n").slice(2)) {
			// table format: ID  KIND  SCOPE  STATUS  TITLE
			const parts = line.split(/\s{2,}/).filter(Boolean);
			if (parts.length >= 5) {
				rows.push({
					id: parts[0]!,
					kind: parts[1]!,
					title: parts.slice(4).join(" "),
					status: parts[3]!,
				});
			}
		}
		return { rows };
	} catch (e) {
		return { rows: [], error: e instanceof Error ? e.message : String(e) };
	}
}

export async function showScribe(ctx: ExtensionCommandContext): Promise<void> {
	if (!ctx.hasUI) {
		ctx.ui.notify("/scribe requires interactive mode", "warning");
		return;
	}

	let { rows, error } = await loadArtifacts();
	if (error) {
		ctx.ui.notify(`scribe unavailable: ${error}`, "error");
		return;
	}
	if (rows.length === 0) {
		ctx.ui.notify("No artifacts in the work graph", "info");
		return;
	}

	for (;;) {
		const action = await renderPanel(ctx, rows);
		if (!action) return;

		if (action.type === "show" && action.row) {
			try {
				const detail = await opsText("get", { id: action.row.id, format: "full" });
				ctx.ui.notify(detail.slice(0, 500), "info");
			} catch (e) {
				ctx.ui.notify(`get failed: ${e instanceof Error ? e.message : e}`, "error");
			}
		}
	}
}

interface PanelAction {
	type: "show";
	row?: ArtifactRow;
}

function renderPanel(ctx: ExtensionCommandContext, rows: ArtifactRow[]): Promise<PanelAction | undefined> {
	return ctx.ui.custom<PanelAction | undefined>((tui, theme, _kb, done) => {
		const searchInput = new Input();
		let searchActive = false;
		let filtered = [...rows];
		let selectedIndex = 0;
		const maxVisible = 20;

		function applyFilter(): void {
			const q = searchInput.getValue().trim().toLowerCase();
			filtered = q
				? rows.filter((r) => r.id.toLowerCase().includes(q) || r.title.toLowerCase().includes(q) || r.kind.includes(q))
				: [...rows];
			selectedIndex = 0;
		}

		const header = {
			invalidate() {},
			render(width: number): string[] {
				const title = theme.bold("Scribe · Work Graph");
				const hint = searchActive
					? rawKeyHint("esc", "clear")
					: rawKeyHint("enter", "show") + theme.fg("muted", " · ") + rawKeyHint("/", "filter") + theme.fg("muted", " · ") + rawKeyHint("esc", "close");
				const spacing = Math.max(1, width - visibleWidth(title) - visibleWidth(hint));
				return [
					truncateToWidth(`${title}${" ".repeat(spacing)}${hint}`, width, ""),
					truncateToWidth(theme.fg("muted", `${filtered.length} artifacts`), width, ""),
				];
			},
		};

		const list = {
			invalidate() {},
			render(width: number): string[] {
				const lines: string[] = [];
				if (searchActive) lines.push(...searchInput.render(width));
				lines.push("");
				if (filtered.length === 0) {
					lines.push(theme.fg("muted", "  No results"));
					return lines;
				}
				const start = Math.max(0, Math.min(selectedIndex - Math.floor(maxVisible / 2), filtered.length - maxVisible));
				const end = Math.min(start + maxVisible, filtered.length);
				for (let i = start; i < end; i++) {
					const row = filtered[i]!;
					const selected = i === selectedIndex;
					const cursor = selected ? theme.fg("accent", "❯") : " ";
					const name = selected ? theme.bold(row.id) : row.id;
					const kind = theme.fg("dim", ` [${row.kind}]`);
					const title = theme.fg("muted", ` ${row.title}`);
					lines.push(truncateToWidth(`${cursor} ${name}${kind}${title}`, width, ""));
				}
				return lines;
			},
		};

		const container = new Container();
		container.addChild(new Spacer(1));
		container.addChild(new DynamicBorder());
		container.addChild(new Spacer(1));
		container.addChild(header);
		container.addChild(new Spacer(1));
		container.addChild(list);
		container.addChild(new Spacer(1));
		container.addChild(new DynamicBorder());

		return {
			render: (width: number) => container.render(width),
			invalidate: () => container.invalidate(),
			handleInput(data: string) {
				if (searchActive) {
					if (data === "\x1b") {
						searchActive = false;
						applyFilter();
					} else if (data === "\r") {
						searchActive = false;
					} else {
						searchInput.handleInput(data);
						applyFilter();
					}
					tui.requestRender();
					return;
				}
				switch (data) {
					case "\x1b[A":
						selectedIndex = (selectedIndex - 1 + filtered.length) % Math.max(filtered.length, 1);
						break;
					case "\x1b[B":
						selectedIndex = (selectedIndex + 1) % Math.max(filtered.length, 1);
						break;
	case "/":
						searchActive = true;
						break;
					case "\r": {
						const row = filtered[selectedIndex];
						if (row) done({ type: "show", row });
						return;
					}
					case "\x1b":
						done(undefined);
						return;
					default:
						return;
				}
				tui.requestRender();
			},
		};
	});
}
