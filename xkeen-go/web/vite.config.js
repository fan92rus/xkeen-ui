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

export default defineConfig({
    plugins: [vue(), ...(isDev ? [devEntryPlugin()] : [])],
    server: isDev ? {
        port: 5173,
        proxy: {
            // Proxy API & WS to Go backend
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
        rollupOptions: {
            input: 'src/main.js',
            output: {
                format: 'es',
                entryFileNames: 'bundle.js',
                inlineDynamicImports: true,
            },
        },
    },
});
