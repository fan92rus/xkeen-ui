import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';

const isDev = process.env.NODE_ENV !== 'production';

function devEntryPlugin() {
	return {
		name: 'dev-entry',
		transformIndexHtml(html) {
			return html.replace(
				'<script src="/static/dist/bundle.js"></script>',
				'<script type="module" src="/src/main.js"></script>'
			);
		},
	};
}

// Production CSS handling: Vite extracts CSS to a separate file
// (static/dist/bundle.css). Because the Go binary serves index.html directly
// (not processed by Vite), the <link> tag must be added to index.html by hand.
// External CSS extraction keeps the bulk of Vue component styles out of the
// JS bundle and into bundle.css (linked from index.html). CodeMirror 6 still
// injects a small amount of <style> elements at runtime for theme/syntax
// highlighting — the CSP uses style-src 'self' 'unsafe-inline' for this.

export default defineConfig({
    plugins: [vue(), ...(isDev ? [devEntryPlugin()] : [])],
    server: isDev ? {
        port: 5173,
        proxy: {
            '/api': 'http://localhost:8089',
            '/ws': {
                target: 'http://localhost:8089',
                ws: true,
            },
            '/health': 'http://localhost:8089',
        },
    } : undefined,
    build: {
        outDir: 'static/dist',
        emptyOutDir: true,
        minify: true,
        sourcemap: false,
        target: 'es2020',
        cssCodeSplit: false,
        rollupOptions: {
            input: 'src/main.js',
            output: {
                format: 'iife',
                entryFileNames: 'bundle.js',
                assetFileNames: 'bundle.css',
                inlineDynamicImports: true,
            },
        },
    },
    test: {
        exclude: ['test/**', 'node_modules/**'],
        // Component tests opt into a DOM via a per-file
        // `// @vitest-environment happy-dom` docblock, keeping pure-logic util
        // tests in the fast node environment by default.
    },
});
