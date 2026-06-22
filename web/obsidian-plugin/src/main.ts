import { Plugin, ItemView, WorkspaceLeaf, PluginSettingTab, Setting, MarkdownPostProcessorFunction, App } from 'obsidian';
import { ScribeClient, type GraphData } from './api';

const VIEW_TYPE_SCRIBE = 'scribe-graph-view';

interface ScribePluginSettings {
	serverUrl: string;
	autoSync: boolean;
	defaultScope: string;
}

const DEFAULT_SETTINGS: ScribePluginSettings = {
	serverUrl: 'http://localhost:8080',
	autoSync: false,
	defaultScope: '',
};

export default class ScribePlugin extends Plugin {
	settings!: ScribePluginSettings;
	client!: ScribeClient;

	async onload() {
		await this.loadSettings();
		this.client = new ScribeClient(this.settings.serverUrl);

		this.registerView(VIEW_TYPE_SCRIBE, (leaf) => new ScribeView(leaf, this.client));

		this.addRibbonIcon('network', 'Open Scribe Graph', () => this.activateView());

		this.addCommand({
			id: 'open-scribe-graph',
			name: 'Open Scribe Graph',
			callback: () => this.activateView(),
		});

		this.addCommand({
			id: 'sync-vault-to-scribe',
			name: 'Sync vault to Scribe',
			callback: () => this.syncVault(),
		});

		this.registerMarkdownCodeBlockProcessor('scribe-graph', this.renderGraphBlock.bind(this));
		this.registerMarkdownCodeBlockProcessor('scribe-board', this.renderBoardBlock.bind(this));

		this.addSettingTab(new ScribeSettingTab(this.app, this));
	}

	async activateView() {
		const existing = this.app.workspace.getLeavesOfType(VIEW_TYPE_SCRIBE);
		if (existing.length > 0) {
			this.app.workspace.revealLeaf(existing[0]);
			return;
		}
		const leaf = this.app.workspace.getRightLeaf(false);
		if (leaf) {
			await leaf.setViewState({ type: VIEW_TYPE_SCRIBE, active: true });
			this.app.workspace.revealLeaf(leaf);
		}
	}

	async syncVault() {
		// Placeholder — will POST vault path to Scribe's vault_sync endpoint
		console.log('Sync vault to Scribe:', this.app.vault.getRoot().path);
	}

	renderGraphBlock: MarkdownPostProcessorFunction = async (source, el) => {
		const params = parseBlockParams(source);
		try {
			const data = await this.client.fetchGraph(params);
			renderMiniGraph(el, data);
		} catch (e) {
			el.createEl('p', { text: `Scribe error: ${e}`, cls: 'scribe-error' });
		}
	};

	renderBoardBlock: MarkdownPostProcessorFunction = async (source, el) => {
		const params = parseBlockParams(source);
		try {
			const arts = await this.client.fetchArtifacts(params);
			renderMiniBoard(el, arts);
		} catch (e) {
			el.createEl('p', { text: `Scribe error: ${e}`, cls: 'scribe-error' });
		}
	};

	async loadSettings() {
		this.settings = Object.assign({}, DEFAULT_SETTINGS, await this.loadData());
	}

	async saveSettings() {
		await this.saveData(this.settings);
		this.client = new ScribeClient(this.settings.serverUrl);
	}
}

class ScribeView extends ItemView {
	constructor(leaf: WorkspaceLeaf, private client: ScribeClient) {
		super(leaf);
	}

	getViewType() { return VIEW_TYPE_SCRIBE; }
	getDisplayText() { return 'Scribe Graph'; }
	getIcon() { return 'network'; }

	async onOpen() {
		const container = this.containerEl.children[1];
		container.empty();
		container.createEl('h4', { text: 'Scribe Graph' });

		try {
			const data = await this.client.fetchGraph({ max_nodes: '100' });
			const info = container.createEl('p', {
				text: `${data.nodes.length} nodes, ${data.links.length} edges`,
				cls: 'scribe-info',
			});

			const list = container.createEl('ul');
			for (const node of data.nodes.slice(0, 20)) {
				list.createEl('li', { text: `${node.kind}: ${node.name}` });
			}
			if (data.nodes.length > 20) {
				container.createEl('p', { text: `... and ${data.nodes.length - 20} more`, cls: 'scribe-muted' });
			}
		} catch (e) {
			container.createEl('p', { text: `Connection error: ${e}`, cls: 'scribe-error' });
			container.createEl('p', { text: 'Ensure Scribe server is running.', cls: 'scribe-muted' });
		}
	}

	async onClose() {}
}

class ScribeSettingTab extends PluginSettingTab {
	constructor(app: App, private plugin: ScribePlugin) {
		super(app, plugin);
	}

	display() {
		const { containerEl } = this;
		containerEl.empty();
		containerEl.createEl('h2', { text: 'Scribe Graph Settings' });

		new Setting(containerEl)
			.setName('Server URL')
			.setDesc('URL of the Scribe HTTP server')
			.addText(text => text
				.setPlaceholder('http://localhost:8080')
				.setValue(this.plugin.settings.serverUrl)
				.onChange(async (value) => {
					this.plugin.settings.serverUrl = value;
					await this.plugin.saveSettings();
				}));

		new Setting(containerEl)
			.setName('Default scope')
			.setDesc('Scope filter applied to all views')
			.addText(text => text
				.setValue(this.plugin.settings.defaultScope)
				.onChange(async (value) => {
					this.plugin.settings.defaultScope = value;
					await this.plugin.saveSettings();
				}));

		new Setting(containerEl)
			.setName('Auto-sync')
			.setDesc('Automatically sync vault changes to Scribe')
			.addToggle(toggle => toggle
				.setValue(this.plugin.settings.autoSync)
				.onChange(async (value) => {
					this.plugin.settings.autoSync = value;
					await this.plugin.saveSettings();
				}));
	}
}

function parseBlockParams(source: string): Record<string, string> {
	const params: Record<string, string> = {};
	for (const line of source.split('\n')) {
		const match = line.match(/^(\w+):\s*(.+)$/);
		if (match) params[match[1]] = match[2].trim();
	}
	return params;
}

function renderMiniGraph(el: HTMLElement, data: GraphData) {
	const wrap = el.createDiv({ cls: 'scribe-mini-graph' });
	wrap.createEl('p', { text: `${data.nodes.length} nodes, ${data.links.length} edges`, cls: 'scribe-info' });
	const list = wrap.createEl('ul');
	const shown = data.nodes.slice(0, 10);
	for (const node of shown) {
		list.createEl('li', { text: `[${node.kind}] ${node.name}` });
	}
	if (data.nodes.length > 10) {
		wrap.createEl('p', { text: `+ ${data.nodes.length - 10} more`, cls: 'scribe-muted' });
	}
}

function renderMiniBoard(el: HTMLElement, arts: Array<{ id: string; title: string; kind: string; status: string }>) {
	const wrap = el.createDiv({ cls: 'scribe-mini-board' });
	const groups = new Map<string, typeof arts>();
	for (const a of arts) {
		const list = groups.get(a.status) ?? [];
		list.push(a);
		groups.set(a.status, list);
	}
	for (const [status, items] of [...groups.entries()].sort()) {
		wrap.createEl('h5', { text: `${status} (${items.length})` });
		const list = wrap.createEl('ul');
		for (const item of items.slice(0, 5)) {
			list.createEl('li', { text: `${item.title} [${item.kind}]` });
		}
		if (items.length > 5) {
			wrap.createEl('p', { text: `+ ${items.length - 5} more`, cls: 'scribe-muted' });
		}
	}
}
