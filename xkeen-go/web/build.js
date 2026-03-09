#!/usr/bin/env node
/**
 * Build script for XKEEN-UI frontend bundle
 * Usage: node build.js [--watch]
 */

import * as esbuild from 'esbuild';
import { dirname } from 'path';
import { fileURLToPath } from 'url';
import { existsSync, mkdirSync } from 'fs';

const __dirname = dirname(fileURLToPath(import.meta.url));
const isWatch = process.argv.includes('--watch');

// Ensure output directory exists
const outDir = 'static/dist';
if (!existsSync(outDir)) {
    mkdirSync(outDir, { recursive: true });
}

const buildOptions = {
    entryPoints: ['static/js/main-entry.js'],
    bundle: true,
    outfile: 'static/dist/bundle.js',
    format: 'esm',
    minify: true,
    sourcemap: false,
    target: ['es2020'],
    loader: {
        '.js': 'js',
    },
    external: [], // Bundle everything
    logLevel: 'info',
    banner: {
        js: '// XKEEN-UI Frontend Bundle - Built with esbuild'
    }
};

async function build() {
    try {
        if (isWatch) {
            const ctx = await esbuild.context(buildOptions);
            await ctx.watch();
            console.log('Watching for changes...');
        } else {
            await esbuild.build(buildOptions);
            console.log('Build complete!');
        }
    } catch (error) {
        console.error('Build failed:', error);
        process.exit(1);
    }
}

build();
