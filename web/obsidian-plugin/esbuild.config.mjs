import esbuild from 'esbuild';
import { existsSync } from 'fs';

const watch = process.argv.includes('--watch');

const ctx = await esbuild.context({
	entryPoints: ['src/main.ts'],
	bundle: true,
	external: ['obsidian', 'electron', '@codemirror/*', '@lezer/*'],
	format: 'cjs',
	target: 'es2022',
	outfile: 'main.js',
	sourcemap: watch ? 'inline' : false,
	logLevel: 'info',
});

if (watch) {
	await ctx.watch();
} else {
	await ctx.rebuild();
	await ctx.dispose();
}
