'use strict';

const fs = require('node:fs');
const path = require('node:path');

const { c } = require('./ui');
const { detectFramework, projectName } = require('./detect');
const { select, text, confirm } = require('./prompt');

function relPathValidator(value) {
  if (!value) return 'パスを入力してください';
  if (value.startsWith('/') || value.split('/').includes('..')) {
    return 'プロジェクト内の相対パスを入力してください';
  }
  return true;
}

function summarize(d) {
  if (d.mode === 'static') return `mode: static ${c.gray('·')} dir: ${d.dir || 'dist'}`;
  if (d.mode === 'worker') return `mode: worker ${c.gray('·')} entry: ${d.entry || 'worker.js'}`;
  return `mode: node ${c.gray('·')} start: ${d.start || 'node server.js'}`;
}

function buildRecommended(d) {
  const config = { mode: d.mode };
  if (d.framework) config.framework = d.framework;
  if (d.mode === 'static') {
    config.dir = d.dir || 'dist';
    config.spa = d.spa !== false;
  } else if (d.mode === 'worker') {
    config.entry = d.entry || 'worker.js';
    config.compatibilityDate = new Date().toISOString().slice(0, 10);
  } else {
    config.start = d.start || 'node server.js';
  }
  return config;
}

async function buildInteractive(d) {
  const mode = await select({
    message: 'デプロイモード',
    initial: ['static', 'node', 'worker'].indexOf(d.mode),
    choices: [
      { value: 'static', label: 'static', hint: '静的サイト（HTML / CSS / JS）' },
      { value: 'node', label: 'node', hint: 'Node.js サーバープロセス' },
      { value: 'worker', label: 'worker', hint: 'Worker（ES Module）' },
    ],
  });

  const config = { mode };
  if (d.framework) config.framework = d.framework;

  if (mode === 'static') {
    config.dir = await text({
      message: '公開するフォルダー',
      initial: d.mode === 'static' ? d.dir || 'dist' : 'dist',
      validate: relPathValidator,
    });
    config.spa = await confirm({
      message: 'SPA ルーティングを使う（未知のパスを index.html に返す）',
      initial: d.spa !== false,
    });
  } else if (mode === 'worker') {
    config.entry = await text({
      message: 'エントリーファイル（ES Module にバンドル済み）',
      initial: d.entry || 'worker.js',
      validate: relPathValidator,
    });
    config.compatibilityDate = await text({
      message: 'compatibilityDate',
      initial: new Date().toISOString().slice(0, 10),
    });
  } else {
    config.start = await text({
      message: '起動コマンド',
      initial: d.start || 'node server.js',
    });
  }

  if (mode !== 'static') {
    const include = (
      await text({
        message: '送信するフォルダーを限定（カンマ区切り。空欄でプロジェクト全体）',
        initial: '',
      })
    )
      .split(',')
      .map((s) => s.trim())
      .filter(Boolean);
    if (include.length) config.include = include;
  }
  return config;
}

async function runInit(projectDir, { reporter, yes }) {
  const target = path.join(projectDir, 'sorahost.json');
  const exists = fs.existsSync(target);
  const detected = detectFramework(projectDir);

  reporter.header({
    title: 'sorahost init',
    rows: [
      ['プロジェクト', projectName(projectDir)],
      ['検出', detected.label || c.gray('不明（手動で設定します）')],
      ['推奨設定', summarize(detected)],
    ],
  });

  if (detected.note) reporter.note(detected.note);

  if (exists && !yes) {
    const overwrite = await confirm({
      message: 'sorahost.json は既にあります。上書きしますか？',
      initial: false,
    });
    if (!overwrite) {
      reporter.note('中止しました。');
      return { created: false };
    }
  }

  const config = yes ? buildRecommended(detected) : await buildInteractive(detected);
  fs.writeFileSync(target, `${JSON.stringify(config, null, 2)}\n`);

  reporter.success({
    title: 'sorahost.json を作成しました',
    rows: Object.entries(config).map(([k, v]) => [k, c.cyan(String(v))]),
  });

  if (config.mode === 'static') {
    reporter.note(`次に \`npm run build\` などでビルドし、${c.bold(config.dir)}/ を用意してください。`);
  }
  return { created: true, config };
}

module.exports = { runInit, buildRecommended };
