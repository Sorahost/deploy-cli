'use strict';

const fs = require('node:fs');
const os = require('node:os');
const path = require('node:path');
const { spawn, spawnSync } = require('node:child_process');

const pkg = require('../package.json');
const ui = require('./ui');
const { c, sym } = ui;
const { createReporter } = require('./reporter');
const { pack } = require('./pack');
const { sha256File, upload } = require('./http');
const prompt = require('./prompt');
const { detectFramework, projectName } = require('./detect');
const { runInit } = require('./init');
const {
  UsageError,
  DeployError,
  fromNetworkError,
  fromHttpStatus,
  SIZE_HINT,
} = require('./errors');
const {
  CREDENTIALS_FILE,
  credentialsPath,
  loadCredentials,
  saveCredentials,
  recordDeploy,
  removeCredentials,
} = require('./config');

const REPOSITORY_URL = 'https://github.com/Sorahost/deploy-cli';
const MAX_UPLOAD_BYTES =
  Number(process.env.SORAHOST_MAX_UPLOAD_BYTES) || 256 * 1024 * 1024;

const HELP = `${c.cyan(sym.diamond)} ${c.bold('sorahost')} ${c.gray(`v${pkg.version}`)}  ${c.gray('— SORAHOST (PteWorker) デプロイ CLI')}

${c.bold('使い方')}
  sorahost ${c.gray('[deploy]')} ${c.gray('[パス]')}   プロジェクトをデプロイする（既定コマンド）
  sorahost init ${c.gray('[パス]')}         sorahost.json を対話で作成する
  sorahost login ${c.gray('[パス]')}        エンドポイントとトークンを保存する
  sorahost logout ${c.gray('[パス]')}       保存した認証情報を削除する
  sorahost whoami ${c.gray('[パス]')}       現在の設定と直近のデプロイを表示する
  sorahost open ${c.gray('[パス]')}         公開サイトをブラウザーで開く

${c.bold('オプション')}
  -y, --yes         確認を省略する（CI 向け）
      --dry-run     アップロードせず、送信内容とサイズだけ表示する
      --json        機械可読な JSON で結果を出力する
      --open        デプロイ成功後にブラウザーで開く
  -q, --quiet       進捗表示を抑える
      --no-color    色を使わない
  -h, --help        このヘルプを表示する
  -v, --version     バージョンを表示する

${c.bold('認証情報の読み取り順')}
  1. 環境変数 SORAHOST_ENDPOINT / SORAHOST_TOKEN
  2. プロジェクト内の ${CREDENTIALS_FILE}
  3. 画面入力（入力後に保存するか確認します）

${c.gray(REPOSITORY_URL)}
`;

// --- 引数解析 --------------------------------------------------------
const COMMANDS = new Set(['deploy', 'init', 'login', 'logout', 'whoami', 'open']);

function parseArgs(argv) {
  const flags = {
    yes: false,
    dryRun: false,
    json: false,
    open: false,
    quiet: false,
    color: undefined,
    help: false,
    version: false,
  };
  const positional = [];

  for (const arg of argv.slice(2)) {
    switch (arg) {
      case '-y':
      case '--yes':
        flags.yes = true;
        break;
      case '--dry-run':
        flags.dryRun = true;
        break;
      case '--json':
        flags.json = true;
        break;
      case '--open':
        flags.open = true;
        break;
      case '-q':
      case '--quiet':
        flags.quiet = true;
        break;
      case '--no-color':
        flags.color = false;
        break;
      case '--color':
        flags.color = true;
        break;
      case '-h':
      case '--help':
        flags.help = true;
        break;
      case '-v':
      case '--version':
        flags.version = true;
        break;
      default:
        if (arg.startsWith('-')) throw new UsageError(`不明なオプション: ${arg}`);
        positional.push(arg);
    }
  }

  let command = 'deploy';
  if (positional.length && COMMANDS.has(positional[0])) {
    command = positional.shift();
  }
  return { command, dir: positional[0] || '.', flags };
}

// --- 共通ヘルパー ----------------------------------------------------
function resolveProjectDir(dirArg) {
  const abs = path.resolve(dirArg);
  if (!fs.existsSync(abs) || !fs.statSync(abs).isDirectory()) {
    throw new DeployError('プロジェクトのフォルダーが見つかりません', {
      detail: abs,
      code: 'NO_PROJECT_DIR',
    });
  }
  return fs.realpathSync(abs);
}

function readConfig(projectDir) {
  const file = path.join(projectDir, 'sorahost.json');
  if (!fs.existsSync(file)) return null;
  try {
    return JSON.parse(fs.readFileSync(file, 'utf8'));
  } catch (err) {
    throw new DeployError('sorahost.json を読み込めませんでした', {
      detail: `JSON として不正です: ${err.message}`,
      hint: 'カンマや括弧の閉じ忘れがないか確認してください。',
      code: 'BAD_CONFIG',
    });
  }
}

function modeSummary(config) {
  if (!config) return c.gray('未設定');
  const inc = Array.isArray(config.include) && config.include.length
    ? c.gray(`  ${sym.dot} 送信 ${config.include.join(', ')}/`)
    : '';
  if (config.mode === 'static') {
    return `static ${c.gray(sym.dot)} ${config.dir || 'dist'}/${config.spa ? c.gray('  (SPA)') : ''}${inc}`;
  }
  if (config.mode === 'worker') return `worker ${c.gray(sym.dot)} ${config.entry || 'worker.js'}${inc}`;
  if (config.mode === 'node') return `node ${c.gray(sym.dot)} ${config.start || ''}`.trimEnd() + inc;
  return (config.mode || c.gray('不明')) + inc;
}

function shortEndpoint(endpoint) {
  try {
    const u = new URL(endpoint);
    const tail = u.pathname.length > 14 ? `…${u.pathname.slice(-10)}` : u.pathname;
    return c.gray(`${u.host}${tail}`);
  } catch {
    return c.gray(endpoint);
  }
}

function maskToken(token) {
  if (!token) return c.gray('-');
  return c.gray(`····${token.slice(-4)}`);
}

function siteUrlFor(endpoint, response) {
  const fromServer =
    response && response.json && (response.json.url || response.json.siteUrl || response.json.deployUrl);
  if (typeof fromServer === 'string' && /^https?:\/\//.test(fromServer)) return fromServer;
  try {
    return new URL(endpoint).origin + '/';
  } catch {
    return null;
  }
}

function validateEndpoint(endpoint) {
  if (!/^https?:\/\//i.test(endpoint)) {
    throw new DeployError('エンドポイントの形式が正しくありません', {
      detail: `http:// または https:// で始まるURLを入力してください: ${endpoint}`,
      code: 'BAD_ENDPOINT',
    });
  }
  return endpoint.replace(/\/+$/, '');
}

function openInBrowser(url) {
  const cmd =
    process.platform === 'darwin'
      ? 'open'
      : process.platform === 'win32'
        ? 'cmd'
        : 'xdg-open';
  const args = process.platform === 'win32' ? ['/c', 'start', '', url] : [url];
  try {
    spawn(cmd, args, { stdio: 'ignore', detached: true }).unref();
    return true;
  } catch {
    return false;
  }
}

// --- 認証情報の解決 -------------------------------------------------
async function resolveCredentials(projectDir, reporter, { yes }) {
  const saved = loadCredentials(projectDir);
  let endpoint = (process.env.SORAHOST_ENDPOINT || '').trim();
  let token = (process.env.SORAHOST_TOKEN || '').trim();
  let source = endpoint || token ? '環境変数' : null;

  if (!endpoint && saved.endpoint) {
    endpoint = saved.endpoint;
    source = source || CREDENTIALS_FILE;
  }
  if (!token && saved.token) {
    token = saved.token;
    source = source || CREDENTIALS_FILE;
  }

  const promptedEndpoint = !endpoint;
  const promptedToken = !token;

  if ((promptedEndpoint || promptedToken) && yes) {
    throw new DeployError('認証情報が足りません', {
      detail: '--yes 実行時は SORAHOST_ENDPOINT / SORAHOST_TOKEN、または保存済みの認証情報が必要です。',
      command: 'sorahost login',
      code: 'NO_CREDENTIALS',
    });
  }

  if (promptedEndpoint || promptedToken) {
    reporter.line(
      `  ${c.gray(sym.arrow)}  ${c.gray('PteWorker のコンソールに表示された値を貼り付けてください')}`,
    );
    reporter.blank();
  }
  if (promptedEndpoint) {
    endpoint = await prompt.text({
      message: 'エンドポイント',
      validate: (v) => (/^https?:\/\//i.test(v) ? true : 'http:// または https:// で始まるURL'),
    });
  }
  if (promptedToken) {
    token = await prompt.password({ message: 'デプロイトークン' });
    if (token) reporter.line(`  ${c.green(sym.tick)} ${c.bold('デプロイトークン')} ${maskToken(token)}`);
  }

  if (!endpoint) throw new DeployError('エンドポイントが入力されていません', { code: 'NO_ENDPOINT' });
  if (!token) throw new DeployError('デプロイトークンが入力されていません', { code: 'NO_TOKEN' });
  endpoint = validateEndpoint(endpoint);

  if (promptedEndpoint || promptedToken) {
    const save = process.stdin.isTTY
      ? await prompt.confirm({
          message: `この内容を ${CREDENTIALS_FILE} に保存して次回から省略しますか？`,
          initial: true,
        })
      : true;
    if (save) {
      const { file, gitignoreUpdated } = saveCredentials(projectDir, { endpoint, token });
      reporter.line(`  ${c.green(sym.tick)} 保存しました ${c.gray(file)} ${c.gray('(600)')}`);
      if (gitignoreUpdated) {
        reporter.line(`  ${c.green(sym.tick)} .gitignore に ${CREDENTIALS_FILE} を追記しました`);
      }
      source = CREDENTIALS_FILE;
    }
  }

  if (promptedEndpoint || promptedToken) reporter.blank();
  return { endpoint, token, source: source || '画面入力' };
}

// --- deploy --------------------------------------------------------
async function commandDeploy(projectDir, reporter, flags) {
  const startedAt = Date.now();
  let config = readConfig(projectDir);

  if (!config) {
    if (flags.yes || !process.stdin.isTTY) {
      throw new DeployError('sorahost.json が見つかりません', {
        detail: `デプロイするフォルダーの一番上に sorahost.json が必要です: ${projectDir}`,
        hint: 'sorahost init で対話的に作成できます。',
        command: 'sorahost init',
        code: 'NO_CONFIG',
      });
    }
    reporter.warn('sorahost.json がありません。');
    const create = await prompt.confirm({ message: 'いま作成しますか？', initial: true });
    if (!create) {
      throw new DeployError('sorahost.json が必要です', { command: 'sorahost init', code: 'NO_CONFIG' });
    }
    const result = await runInit(projectDir, { reporter, yes: false });
    if (!result.created) throw new DeployError('中止しました', { code: 'ABORTED' });
    config = result.config;
    reporter.blank();
  }

  const name = projectName(projectDir);
  const saved = loadCredentials(projectDir);
  reporter.header({
    title: `${name} をデプロイ`,
    rows: [
      ['対象', c.gray(projectDir)],
      ['モード', modeSummary(config)],
      [
        '前回',
        saved.lastDeployedAt
          ? `${ui.relativeTime(saved.lastDeployedAt)} ${c.gray(sym.dot)} ${
              saved.lastDeployStatus === 'success' ? c.green('成功') : c.yellow(saved.lastDeployStatus || '不明')
            }`
          : c.gray('なし'),
      ],
    ],
  });

  const { endpoint, token, source } = await resolveCredentials(projectDir, reporter, flags);

  // static / worker で対象パスが存在するか確認
  await ensureBuildOutput(projectDir, config, reporter, flags);

  const tmpDir = fs.mkdtempSync(path.join(os.tmpdir(), 'sorahost-'));
  const artifact = path.join(tmpDir, 'artifact.tar.gz');
  const cleanup = () => {
    try {
      fs.rmSync(tmpDir, { recursive: true, force: true });
    } catch {
      /* noop */
    }
  };
  process.on('exit', cleanup);

  try {
    const packTask = reporter.task('ファイルをまとめています');
    let stats;
    try {
      stats = await pack(projectDir, artifact, config);
    } catch (err) {
      packTask.fail('ファイルをまとめられませんでした');
      throw new DeployError('アーカイブの作成に失敗しました', { detail: err.message, code: 'PACK_FAILED' });
    }
    const digest = await sha256File(artifact);
    packTask.succeed(
      `${c.bold(stats.files)} ファイル ${c.gray(sym.dot)} ${c.bold(ui.humanBytes(stats.bytes))} ` +
        c.gray(`(圧縮後 ${ui.humanBytes(stats.compressedBytes)})`),
    );
    if (stats.roots) {
      reporter.note(`送信対象: ${stats.roots.map((r) => c.cyan(`${r}/`)).join(' ')}`);
    }
    if (stats.ignoredByRules > 0) {
      reporter.note(`.sorahostignore で ${stats.ignoredByRules} 個除外しました`);
    }

    // node アプリなのに依存が1つも入っていない場合の警告
    if (config.mode === 'node' && !stats.hasNodeModules) {
      reporter.warn('アーカイブに node_modules が含まれていません。');
      reporter.note(
        'バンドル済みなら問題ありません。そうでなければ、.sorahostignore の ' +
          'node_modules 行を消すか、"include" に依存フォルダーを含めてください。',
      );
    }

    if (stats.compressedBytes > MAX_UPLOAD_BYTES) {
      throw new DeployError('アップロードサイズが上限を超えています', {
        detail: `${ui.humanBytes(stats.compressedBytes)}  ${c.gray('>')}  上限 ${ui.humanBytes(MAX_UPLOAD_BYTES)}`,
        hint: SIZE_HINT,
        command: 'sorahost deploy --dry-run',
        code: 'PAYLOAD_TOO_LARGE',
      });
    }

    if (flags.dryRun) {
      reporter.success({
        title: 'ドライラン完了（アップロードは行っていません）',
        duration: Date.now() - startedAt,
        rows: [
          ['ファイル', String(stats.files)],
          ['サイズ', `${ui.humanBytes(stats.bytes)}  ${c.gray(`圧縮後 ${ui.humanBytes(stats.compressedBytes)}`)}`],
          ['SHA-256', c.gray(digest)],
          ['送信先', shortEndpoint(endpoint) + c.gray('/deploy')],
        ],
      });
      if (flags.json) {
        reporter.result({
          ok: true,
          dryRun: true,
          project: name,
          mode: config.mode,
          artifact: { files: stats.files, bytes: stats.bytes, compressedBytes: stats.compressedBytes, sha256: digest },
        });
      }
      return 0;
    }

    const uploadTask = reporter.task('アップロード中');
    let response;
    try {
      response = await upload({
        endpoint,
        token,
        artifactPath: artifact,
        digest,
        onProgress: (fraction, sent, total) =>
          uploadTask.progress(fraction, `${ui.humanBytes(sent)} / ${ui.humanBytes(total)}`),
      });
    } catch (err) {
      uploadTask.fail('アップロードに失敗しました');
      throw fromNetworkError(err, endpoint);
    }

    if (response.status < 200 || response.status >= 300) {
      uploadTask.fail('サーバーに拒否されました');
      recordDeploy(projectDir, {
        lastDeployedAt: new Date().toISOString(),
        lastDeployStatus: `HTTP ${response.status}`,
      });
      throw fromHttpStatus(response.status, response.body, endpoint);
    }

    uploadTask.succeed('アップロード完了');

    const url = siteUrlFor(endpoint, response);
    recordDeploy(projectDir, {
      lastDeployedAt: new Date().toISOString(),
      lastDeployStatus: 'success',
      siteUrl: url || undefined,
    });

    reporter.success({
      title: 'デプロイ成功',
      duration: Date.now() - startedAt,
      rows: [
        ['サイト', url ? c.cyan(c.underline(url)) : c.gray('(PteWorker のコンソールで url を確認)')],
        ['ファイル', String(stats.files)],
        ['サイズ', ui.humanBytes(stats.bytes)],
        ['認証情報', c.gray(source)],
      ],
    });
    reporter.note('公開が反映されるまで数秒かかることがあります。');

    if ((flags.open || false) && url) openInBrowser(url);

    if (flags.json) {
      reporter.result({
        ok: true,
        project: name,
        mode: config.mode,
        endpoint,
        url: url || null,
        artifact: { files: stats.files, bytes: stats.bytes, compressedBytes: stats.compressedBytes, sha256: digest },
        durationMs: Date.now() - startedAt,
      });
    }
    return 0;
  } finally {
    cleanup();
  }
}

async function ensureBuildOutput(projectDir, config, reporter, flags) {
  const targets = [];
  if (Array.isArray(config.include)) targets.push(...config.include.filter(Boolean));
  if (config.mode === 'static' && config.dir) targets.push(config.dir);
  if (config.mode === 'worker' && config.entry) targets.push(config.entry);
  if (targets.length === 0) return;

  const missing = targets.filter((rel) => !fs.existsSync(path.join(projectDir, rel)));
  if (missing.length === 0) return;

  const label = config.include ? '送信対象' : config.mode === 'static' ? '公開フォルダー' : 'エントリー';
  const field = config.include ? 'include' : config.mode === 'static' ? 'dir' : 'entry';
  const detected = detectFramework(projectDir);
  const buildCmd = detected.buildScript ? `npm run ${detected.buildScript}` : null;

  if (!flags.yes && process.stdin.isTTY && buildCmd) {
    reporter.warn(`${c.bold(missing.join(', '))} が見つかりません。`);
    const run = await prompt.confirm({ message: `いま \`${buildCmd}\` を実行しますか？`, initial: true });
    if (run) {
      reporter.blank();
      const code = runBuild(detected.buildScript, projectDir);
      reporter.blank();
      if (code === 0 && missing.every((rel) => fs.existsSync(path.join(projectDir, rel)))) return;
      throw new DeployError('ビルド後も対象が見つかりません', {
        detail: missing.map((rel) => path.join(projectDir, rel)).join('\n'),
        hint: `sorahost.json の "${field}" が正しいか確認してください。`,
        code: 'NO_BUILD_OUTPUT',
      });
    }
  }

  throw new DeployError(
    `${label} ${missing.join(', ')} が見つかりません`,
    {
      detail: missing.map((rel) => path.join(projectDir, rel)).join('\n'),
      hint: buildCmd
        ? `先にビルドしてください: ${buildCmd}\nsorahost.json の "${field}" も確認してください。`
        : `sorahost.json の "${field}" が正しいか確認してください。`,
      code: 'NO_BUILD_OUTPUT',
    },
  );
}

function runBuild(script, cwd) {
  const result = spawnSync('npm', ['run', script], { cwd, stdio: 'inherit' });
  return result.status == null ? 1 : result.status;
}

// --- その他コマンド ------------------------------------------------
async function commandLogin(projectDir, reporter) {
  reporter.header({ title: 'sorahost login', rows: [['対象', c.gray(projectDir)]] });
  const endpoint = validateEndpoint(
    await prompt.text({
      message: 'エンドポイント',
      validate: (v) => (/^https?:\/\//i.test(v) ? true : 'http:// または https:// で始まるURL'),
    }),
  );
  const token = await prompt.password({ message: 'デプロイトークン' });
  if (!token) throw new DeployError('デプロイトークンが入力されていません', { code: 'NO_TOKEN' });

  const { file, gitignoreUpdated } = saveCredentials(projectDir, { endpoint, token });
  reporter.success({
    title: '保存しました',
    rows: [
      ['ファイル', c.gray(`${file} (600)`)],
      ['エンドポイント', shortEndpoint(endpoint)],
      ['トークン', maskToken(token)],
      ['.gitignore', gitignoreUpdated ? c.green(`${CREDENTIALS_FILE} を追記`) : c.gray('変更なし')],
    ],
  });
  return 0;
}

function commandLogout(projectDir, reporter) {
  const removed = removeCredentials(projectDir);
  if (removed) reporter.success({ title: '認証情報を削除しました', rows: [['ファイル', c.gray(credentialsPath(projectDir))]] });
  else reporter.note(`保存された ${CREDENTIALS_FILE} はありません。`);
  return 0;
}

function commandWhoami(projectDir, reporter, flags) {
  const saved = loadCredentials(projectDir);
  const envEndpoint = (process.env.SORAHOST_ENDPOINT || '').trim();
  const envToken = (process.env.SORAHOST_TOKEN || '').trim();
  const endpoint = envEndpoint || saved.endpoint;
  const token = envToken || saved.token;
  const source = envEndpoint || envToken ? '環境変数' : saved.endpoint || saved.token ? CREDENTIALS_FILE : null;

  if (flags.json) {
    reporter.result({
      configured: Boolean(endpoint && token),
      source,
      endpoint: endpoint || null,
      tokenSuffix: token ? token.slice(-4) : null,
      lastDeployedAt: saved.lastDeployedAt,
      lastDeployStatus: saved.lastDeployStatus,
      siteUrl: saved.siteUrl,
    });
    return 0;
  }

  if (!endpoint || !token) {
    reporter.header({ title: projectName(projectDir), rows: [['対象', c.gray(projectDir)]] });
    reporter.note('まだ設定されていません。');
    reporter.line(`  ${c.gray(sym.arrow)}  ${c.cyan('sorahost deploy')} か ${c.cyan('sorahost login')} を実行してください。`);
    return 0;
  }

  reporter.header({
    title: projectName(projectDir),
    rows: [
      ['対象', c.gray(projectDir)],
      ['エンドポイント', shortEndpoint(endpoint)],
      ['トークン', maskToken(token)],
      ['認証情報', c.gray(source)],
      [
        '前回デプロイ',
        saved.lastDeployedAt
          ? `${ui.relativeTime(saved.lastDeployedAt)} ${c.gray(sym.dot)} ${
              saved.lastDeployStatus === 'success' ? c.green('成功') : c.yellow(saved.lastDeployStatus || '不明')
            }`
          : c.gray('記録なし'),
      ],
      ['サイト', saved.siteUrl ? c.cyan(saved.siteUrl) : c.gray('-')],
    ],
  });
  return 0;
}

function commandOpen(projectDir, reporter) {
  const saved = loadCredentials(projectDir);
  const url = saved.siteUrl || siteUrlFor(saved.endpoint || process.env.SORAHOST_ENDPOINT || '', null);
  if (!url) {
    throw new DeployError('開くURLが分かりません', {
      hint: '一度デプロイするか、sorahost login でエンドポイントを設定してください。',
      code: 'NO_URL',
    });
  }
  reporter.note(`${c.cyan(url)} を開いています…`);
  openInBrowser(url);
  return 0;
}

// --- エントリポイント ---------------------------------------------
async function main(argv) {
  let parsed;
  try {
    parsed = parseArgs(argv);
  } catch (err) {
    if (err instanceof UsageError) {
      process.stderr.write(`\n  ${c.red(sym.cross)} ${err.message}\n\n  ${c.gray('sorahost --help でオプション一覧を表示します。')}\n\n`);
      return 2;
    }
    throw err;
  }

  const { command, dir, flags } = parsed;

  if (flags.color != null) ui.configure({ color: flags.color });
  if (flags.help) {
    process.stdout.write(`${HELP}\n`);
    return 0;
  }
  if (flags.version) {
    process.stdout.write(`${pkg.version}\n`);
    return 0;
  }

  const level = flags.json ? 'json' : flags.quiet ? 'quiet' : ui.state.interactive ? 'pretty' : 'plain';
  const reporter = createReporter(level);

  const onSigint = () => {
    reporter.stopActive();
    ui.restoreCursor();
    try {
      if (process.stdin.isTTY) process.stdin.setRawMode(false);
    } catch {
      /* noop */
    }
    process.stderr.write('\n');
    process.exit(130);
  };
  process.on('SIGINT', onSigint);

  try {
    const projectDir = resolveProjectDir(dir);
    switch (command) {
      case 'init':
        await runInit(projectDir, { reporter, yes: flags.yes });
        return 0;
      case 'login':
        return await commandLogin(projectDir, reporter);
      case 'logout':
        return commandLogout(projectDir, reporter);
      case 'whoami':
        return commandWhoami(projectDir, reporter, flags);
      case 'open':
        return commandOpen(projectDir, reporter);
      default:
        return await commandDeploy(projectDir, reporter, flags);
    }
  } catch (err) {
    reporter.stopActive();
    ui.restoreCursor();
    if (err instanceof prompt.NoInputError) {
      reporter.error({
        title: '入力が読み取れませんでした',
        detail: '対話入力が必要ですが、標準入力がありません。',
        hint: '環境変数 SORAHOST_ENDPOINT / SORAHOST_TOKEN を設定するか、先に sorahost login を実行してください。',
        command: 'sorahost login',
      });
      return 1;
    }
    if (err instanceof DeployError) {
      reporter.error({ title: err.title, detail: err.detail, hint: err.hint, command: err.command });
      if (flags.json) {
        process.stdout.write(
          `${JSON.stringify(
            { ok: false, error: { code: err.code, message: err.title, hint: err.hint }, status: err.status || null },
            null,
            2,
          )}\n`,
        );
      }
      return 1;
    }
    throw err;
  } finally {
    prompt.closePrompt();
    process.removeListener('SIGINT', onSigint);
  }
}

module.exports = { main };
