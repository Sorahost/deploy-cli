'use strict';

const fs = require('node:fs');
const path = require('node:path');

// 依存パッケージからフレームワークと推奨設定を推測する。
const TABLE = [
  {
    dep: 'next',
    framework: 'next',
    label: 'Next.js',
    mode: 'node',
    start: 'npx next start -p $PORT',
    note: '静的書き出し（next export / output: "export"）の場合は mode:"static", dir:"out"',
  },
  { dep: 'nuxt', framework: 'nuxt', label: 'Nuxt', mode: 'node', start: 'node .output/server/index.mjs' },
  { dep: '@sveltejs/kit', framework: 'sveltekit', label: 'SvelteKit', mode: 'node', start: 'node build' },
  { dep: 'astro', framework: 'astro', label: 'Astro', mode: 'static', dir: 'dist', spa: false },
  { dep: 'gatsby', framework: 'gatsby', label: 'Gatsby', mode: 'static', dir: 'public', spa: true },
  { dep: 'vitepress', framework: 'vitepress', label: 'VitePress', mode: 'static', dir: '.vitepress/dist', spa: false },
  { dep: '@docusaurus/core', framework: 'docusaurus', label: 'Docusaurus', mode: 'static', dir: 'build', spa: false },
  { dep: '@11ty/eleventy', framework: 'eleventy', label: 'Eleventy', mode: 'static', dir: '_site', spa: false },
  { dep: 'react-scripts', framework: 'cra', label: 'Create React App', mode: 'static', dir: 'build', spa: true },
  { dep: '@vue/cli-service', framework: 'vue-cli', label: 'Vue CLI', mode: 'static', dir: 'dist', spa: true },
  { dep: '@angular/cli', framework: 'angular', label: 'Angular', mode: 'static', dir: 'dist', spa: true },
  { dep: 'hono', framework: 'hono', label: 'Hono', mode: 'worker', entry: 'dist/index.js' },
  { dep: 'vite', framework: 'vite', label: 'Vite', mode: 'static', dir: 'dist', spa: true },
];

function readPackageJson(projectDir) {
  try {
    return JSON.parse(fs.readFileSync(path.join(projectDir, 'package.json'), 'utf8'));
  } catch {
    return null;
  }
}

function detectFramework(projectDir) {
  const pkg = readPackageJson(projectDir);
  const buildScript = pkg && pkg.scripts && pkg.scripts.build ? 'build' : null;
  if (!pkg) return { framework: null, label: null, mode: 'static', dir: 'dist', spa: true, buildScript };

  const deps = { ...pkg.dependencies, ...pkg.devDependencies };
  for (const row of TABLE) {
    if (deps[row.dep]) return { ...row, buildScript };
  }
  return { framework: null, label: null, mode: 'static', dir: 'dist', spa: true, buildScript };
}

function projectName(projectDir) {
  const pkg = readPackageJson(projectDir);
  if (pkg && typeof pkg.name === 'string' && pkg.name.trim()) return pkg.name.trim();
  return path.basename(projectDir);
}

module.exports = { detectFramework, projectName, readPackageJson };
