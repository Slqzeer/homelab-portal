import { cp, mkdir, rm, writeFile } from 'node:fs/promises';

import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'astro/config';

const embeddedAssets = new URL('../internal/assets/dist/', import.meta.url);

const stageEmbeddedAssets = {
  name: 'stage-embedded-assets',
  hooks: {
    'astro:build:done': async ({ dir }) => {
      await rm(embeddedAssets, { recursive: true, force: true });
      await mkdir(embeddedAssets, { recursive: true });
      await writeFile(new URL('.keep', embeddedAssets), '');
      await cp(dir, embeddedAssets, { recursive: true });
    },
  },
};

export default defineConfig({
  output: 'static',
  outDir: './dist',
  integrations: [stageEmbeddedAssets],
  vite: {
    plugins: [tailwindcss()],
  },
});
