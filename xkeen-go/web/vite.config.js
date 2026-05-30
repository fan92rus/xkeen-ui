import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';

export default defineConfig({
    plugins: [vue()],
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
                inlineDynamicImports: true
            }
        }
    }
});
