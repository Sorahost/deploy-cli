'use strict';

const fs = require('node:fs');
const path = require('node:path');
const tar = require('tar');

// 認証情報や開発用ファイルの誤送信を防ぐための組み込み除外ルール。
const EXCLUDED_BASENAMES = new Set([
  '.env',
  '.npmrc',
  '.netrc',
  '.DS_Store',
  '.sorahost.json',
]);

function isExcluded(entryPath) {
  const parts = entryPath.split('/').filter((s) => s && s !== '.');
  if (parts.includes('.git')) return true;

  const base = parts[parts.length - 1] || '';
  if (EXCLUDED_BASENAMES.has(base)) return true;
  if (base.startsWith('.env.')) return true;

  return false;
}

function toRelative(entryPath) {
  if (entryPath === '.' || entryPath === './' || entryPath === '') return '';
  // 先頭の "./" だけを取り除く（".git" のような dotfile を壊さない）
  return entryPath.replace(/^\.\//, '').replace(/\/+$/, '');
}

function cleanRelative(value) {
  const s = String(value || '')
    .trim()
    .replace(/^\.\//, '')
    .replace(/^\/+/, '')
    .replace(/\/+\*{1,2}$/, '') // "dist/standalone/**" のような末尾グロブを許容
    .replace(/\/+$/, '');
  if (!s || s === '.' || s === '*' || s.split('/').includes('..')) return null;
  return s;
}

// sorahost.json に応じて、アップロードに必要なパスだけへ絞り込む。
//  - "include": [...] があれば、どの mode でもそのパスだけを送る
//  - static: sorahost.json と "dir"
//  - worker: sorahost.json と "entry"
//  - それ以外（node など）: プロジェクト全体（除外ルールのみ適用）
function selectRoots(config) {
  if (!config || typeof config !== 'object') return null;

  const include = Array.isArray(config.include)
    ? config.include.map(cleanRelative).filter(Boolean)
    : [];
  if (include.length) return ['sorahost.json', ...include];

  if (config.mode === 'static') {
    const dir = cleanRelative(config.dir);
    if (dir) return ['sorahost.json', dir];
  }
  if (config.mode === 'worker') {
    const entry = cleanRelative(config.entry);
    if (entry) return ['sorahost.json', entry];
  }
  return null;
}

function withinRoots(roots, rel) {
  if (rel === '') return true;
  return roots.some(
    (root) =>
      rel === root ||
      rel.startsWith(`${root}/`) || // root の中身
      root.startsWith(`${rel}/`), // root へ辿るための親ディレクトリ
  );
}

// .sorahostignore ... .gitignore ライクな簡易マッチャー
function loadIgnoreMatchers(projectDir) {
  let raw;
  try {
    raw = fs.readFileSync(path.join(projectDir, '.sorahostignore'), 'utf8');
  } catch {
    return [];
  }
  return raw
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter((line) => line && !line.startsWith('#'))
    .map(toMatcher);
}

function toMatcher(pattern) {
  const negate = pattern.startsWith('!');
  const body = (negate ? pattern.slice(1) : pattern).replace(/\/+$/, '');
  const anchored = body.startsWith('/');
  const clean = body.replace(/^\/+/, '');
  const source = clean
    .split('/')
    .map((seg) =>
      seg === '**'
        ? '.*'
        : seg.replace(/[.+^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '[^/]*'),
    )
    .join('/');
  return {
    negate,
    anchored,
    hasSlash: clean.includes('/'),
    rx: new RegExp(`^${source}(?:/.*)?$`),
  };
}

function isIgnored(matchers, rel) {
  let ignored = false;
  for (const m of matchers) {
    let hit = m.rx.test(rel);
    if (!hit && !m.anchored && !m.hasSlash) {
      hit = rel.split('/').some((seg) => m.rx.test(seg));
    }
    if (hit) ignored = !m.negate;
  }
  return ignored;
}

function pack(projectDir, outFile, config) {
  const roots = selectRoots(config);
  const matchers = loadIgnoreMatchers(projectDir);
  let files = 0;
  let bytes = 0;
  let ignoredByRules = 0;
  let hasNodeModules = false;

  // tar へ渡すトップレベルのエントリー。"." を渡すと "./" という
  // 空パス相当のエントリーが先頭に入り、厳格なアーカイブ検証で弾かれる。
  const entryList = roots
    ? [...new Set(roots.map((r) => r.split('/')[0]))]
    : fs.readdirSync(projectDir);

  return tar
    .create(
      {
        gzip: true,
        file: outFile,
        cwd: projectDir,
        portable: true,
        filter: (entryPath, stat) => {
          const rel = toRelative(entryPath);
          if (rel === '') return false;
          if (isExcluded(entryPath)) return false;
          if (roots && !withinRoots(roots, rel)) return false;
          if (isIgnored(matchers, rel)) {
            ignoredByRules += 1;
            return false;
          }
          if (stat && typeof stat.isFile === 'function' && stat.isFile()) {
            files += 1;
            bytes += stat.size;
            if (rel.split('/').includes('node_modules')) hasNodeModules = true;
          }
          return true;
        },
      },
      entryList,
    )
    .then(() => ({
      roots: roots ? roots.filter((r) => r !== 'sorahost.json') : null,
      files,
      bytes,
      ignoredByRules,
      hasNodeModules,
      compressedBytes: fs.statSync(outFile).size,
    }));
}

module.exports = {
  pack,
  isExcluded,
  selectRoots,
  isIgnored,
  loadIgnoreMatchers,
  toMatcher,
};
