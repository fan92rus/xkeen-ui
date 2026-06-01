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

// In production, Vite extracts CSS to a separate file but the Go binary
// serves index.html directly (not processed by Vite). The CSS <link> tag
// never gets added, so scoped styles are lost. This plugin inlines all
// generated CSS into the JS bundle at build time.
function cssInlinePlugin() {
	return {
		name: 'css-inline',
		enforce: 'post',
		generateBundle(_, bundles) {
			let cssSource = '';
			// Collect and remove CSS assets
			for (const [fileName, chunk] of Object.entries(bundles)) {
				if (fileName.endsWith('.css') && chunk.type === 'asset') {
					cssSource += chunk.source;
					delete bundles[fileName];
				}
			}
			// Prepend CSS injection to the entry JS chunk
			if (cssSource) {
				for (const [fileName, chunk] of Object.entries(bundles)) {
					if (fileName.endsWith('.js') && chunk.type === 'chunk' && chunk.isEntry) {
						const injected =
							`(function(){var s=document.createElement("style");s.textContent=${JSON.stringify(cssSource)};document.head.appendChild(s)})();\n`;
						chunk.code = injected + chunk.code;
						break;
					}
				}
			}
		},
	};
}

export default defineConfig({
    plugins: [vue(), ...(isDev ? [devEntryPlugin()] : [cssInlinePlugin()])],
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
                inlineDynamicImports: true,
            },
        },
    },
});
