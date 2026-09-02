'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const tar = require('tar');
const { isExcluded, selectRoots, isIgnored, toMatcher, pack } = require('../lib/pack');

function tmpProject() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'sorahost-pack-'));
  fs.mkdirSync(path.join(dir, 'dist/standalone/sub'), { recursive: true });
  fs.mkdirSync(path.join(dir, '.git'), { recursive: true });
  fs.mkdirSync(path.join(dir, 'src'), { recursive: true });
  fs.writeFileSync(path.join(dir, 'sorahost.json'), '{}');
  fs.writeFileSync(path.join(dir, '.env'), 'SECRET=1');
  fs.writeFileSync(path.join(dir, '.git/config'), 'x');
  fs.writeFileSync(path.join(dir, 'src/app.ts'), 'x');
  fs.writeFileSync(path.join(dir, 'dist/standalone/server.js'), 'x');
  fs.writeFileSync(path.join(dir, 'dist/standalone/sub/x.js'), 'x');
  return dir;
}

async function entriesOf(tgz) {
  const names = [];
  await tar.t({ file: tgz, onentry: (e) => names.push(e.path) });
  return names;
}

test('除外されるパス', () => {
  for (const p of [
    './.git',
    './.git/config',
    'src/.git/HEAD',
    './.env',
    './.env.local',
    './config/.env.production',
    './.npmrc',
    './.netrc',
    './.DS_Store',
    './.sorahost.json',
  ]) {
    assert.equal(isExcluded(p), true, p);
  }
});

test('含まれるパス', () => {
  for (const p of [
    './',
    './sorahost.json',
    './dist/index.html',
    './src/env.js',
    './package.json',
  ]) {
    assert.equal(isExcluded(p), false, p);
  }
});

test('selectRoots: static は dir に絞る', () => {
  assert.deepEqual(selectRoots({ mode: 'static', dir: 'dist' }), [
    'sorahost.json',
    'dist',
  ]);
  assert.deepEqual(selectRoots({ mode: 'static', dir: './build/web/' }), [
    'sorahost.json',
    'build/web',
  ]);
});

test('selectRoots: worker は entry に絞る', () => {
  assert.deepEqual(selectRoots({ mode: 'worker', entry: 'worker.js' }), [
    'sorahost.json',
    'worker.js',
  ]);
});

test('selectRoots: node / 不明は絞り込まない', () => {
  assert.equal(selectRoots({ mode: 'node', start: 'node server.js' }), null);
  assert.equal(selectRoots({}), null);
  assert.equal(selectRoots({ mode: 'static' }), null);
  assert.equal(selectRoots({ mode: 'static', dir: '../escape' }), null);
});

test('selectRoots: include はどの mode でも優先', () => {
  assert.deepEqual(selectRoots({ mode: 'node', include: ['dist/standalone'] }), [
    'sorahost.json',
    'dist/standalone',
  ]);
  assert.deepEqual(selectRoots({ mode: 'node', include: ['./dist/', 'public'] }), [
    'sorahost.json',
    'dist',
    'public',
  ]);
  assert.equal(selectRoots({ mode: 'node', include: ['../oops'] }), null);
});

test('アーカイブに空パス（"./"）エントリーを入れない', async () => {
  const dir = tmpProject();
  const out = path.join(dir, 'a.tgz');
  await pack(dir, out, { mode: 'node' });
  const names = await entriesOf(out);
  for (const n of names) {
    const c = n.replace(/\/+$/, '');
    assert.ok(c !== '' && c !== '.' && c !== './' && c !== '/', `空パス: ${JSON.stringify(n)}`);
  }
});

test('include 指定時: 対象パスだけ + 親ディレクトリ + .git/.env 除外', async () => {
  const dir = tmpProject();
  const out = path.join(dir, 'b.tgz');
  const stats = await pack(dir, out, { mode: 'node', include: ['dist/standalone'] });
  const names = await entriesOf(out);
  assert.equal(stats.files, 3); // sorahost.json + server.js + sub/x.js
  assert.ok(names.includes('sorahost.json'));
  assert.ok(names.includes('dist/')); // 親ディレクトリのエントリー
  assert.ok(names.includes('dist/standalone/server.js'));
  assert.ok(!names.some((n) => n.startsWith('src/')));
  assert.ok(!names.some((n) => n.split('/').includes('.git')));
  assert.ok(!names.some((n) => n.replace(/\/$/, '') === '.env'));
});

test('isExcluded: dotfile を壊さない（.git 判定）', () => {
  assert.equal(isExcluded('.git/config'), true);
  assert.equal(isExcluded('.gitignore'), false);
});

test('isIgnored: .sorahostignore 相当のマッチ', () => {
  const matchers = ['node_modules/', '*.log', '/build'].map(toMatcher);
  assert.equal(isIgnored(matchers, 'node_modules'), true);
  assert.equal(isIgnored(matchers, 'node_modules/tar/index.js'), true);
  assert.equal(isIgnored(matchers, 'packages/app/node_modules/x'), true);
  assert.equal(isIgnored(matchers, 'logs/app.log'), true);
  assert.equal(isIgnored(matchers, 'build'), true);
  assert.equal(isIgnored(matchers, 'src/build'), false);
  assert.equal(isIgnored(matchers, 'src/index.js'), false);
});
