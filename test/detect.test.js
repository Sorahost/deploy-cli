'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');
const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');

const { detectFramework, projectName } = require('../lib/detect');

function fixture(pkg) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'sorahost-detect-'));
  if (pkg) fs.writeFileSync(path.join(dir, 'package.json'), JSON.stringify(pkg));
  return dir;
}

test('Vite を検出して static/dist を推奨', () => {
  const d = detectFramework(fixture({ devDependencies: { vite: '^5' }, scripts: { build: 'vite build' } }));
  assert.equal(d.framework, 'vite');
  assert.equal(d.mode, 'static');
  assert.equal(d.dir, 'dist');
  assert.equal(d.buildScript, 'build');
});

test('Next.js は node を推奨', () => {
  const d = detectFramework(fixture({ dependencies: { next: '^14' } }));
  assert.equal(d.framework, 'next');
  assert.equal(d.mode, 'node');
});

test('Hono は worker を推奨', () => {
  const d = detectFramework(fixture({ dependencies: { hono: '^4' } }));
  assert.equal(d.mode, 'worker');
});

test('package.json 無し -> 既定は static/dist', () => {
  const d = detectFramework(fixture(null));
  assert.equal(d.framework, null);
  assert.equal(d.mode, 'static');
  assert.equal(d.buildScript, null);
});

test('projectName は package.json name -> フォルダー名', () => {
  assert.equal(projectName(fixture({ name: 'cool-app' })), 'cool-app');
  const dir = fixture(null);
  assert.equal(projectName(dir), path.basename(dir));
});
