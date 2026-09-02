'use strict';

const fs = require('node:fs');
const path = require('node:path');

// プロジェクトごとの認証情報 + 直近のデプロイ記録を保存するファイル。
// トークンを含むため、コミットしないよう .gitignore へ登録し 600 で保存する。
const CREDENTIALS_FILE = '.sorahost.json';

function credentialsPath(projectDir) {
  return path.join(projectDir, CREDENTIALS_FILE);
}

function readFile(projectDir) {
  try {
    const data = JSON.parse(fs.readFileSync(credentialsPath(projectDir), 'utf8'));
    return data && typeof data === 'object' ? data : {};
  } catch {
    return null;
  }
}

function loadCredentials(projectDir) {
  const data = readFile(projectDir) || {};
  return {
    endpoint: typeof data.endpoint === 'string' ? data.endpoint.trim() : '',
    token: typeof data.token === 'string' ? data.token.trim() : '',
    lastDeployedAt: data.lastDeployedAt || null,
    lastDeployStatus: data.lastDeployStatus || null,
    siteUrl: data.siteUrl || null,
  };
}

function writeFile(projectDir, data) {
  const file = credentialsPath(projectDir);
  fs.writeFileSync(file, `${JSON.stringify(data, null, 2)}\n`, { mode: 0o600 });
  try {
    fs.chmodSync(file, 0o600);
  } catch {
    /* Windows などでは無視 */
  }
  return file;
}

function saveCredentials(projectDir, { endpoint, token }) {
  const existing = readFile(projectDir) || {};
  const file = writeFile(projectDir, { ...existing, endpoint, token });
  const gitignoreUpdated = ensureGitignored(projectDir, CREDENTIALS_FILE);
  return { file, gitignoreUpdated };
}

// デプロイ結果を追記する。認証情報ファイルが無い場合は何もしない。
function recordDeploy(projectDir, patch) {
  const existing = readFile(projectDir);
  if (!existing) return;
  writeFile(projectDir, { ...existing, ...patch });
}

function removeCredentials(projectDir) {
  try {
    fs.rmSync(credentialsPath(projectDir));
    return true;
  } catch {
    return false;
  }
}

// .gitignore があり、まだ登録されていなければ追記する。
function ensureGitignored(projectDir, entry) {
  const file = path.join(projectDir, '.gitignore');
  let content;
  try {
    content = fs.readFileSync(file, 'utf8');
  } catch {
    return false;
  }
  const listed = content
    .split(/\r?\n/)
    .map((line) => line.trim())
    .some((line) => line === entry || line === `/${entry}`);
  if (listed) return false;

  const prefix = content.length === 0 || content.endsWith('\n') ? '' : '\n';
  fs.appendFileSync(file, `${prefix}${entry}\n`);
  return true;
}

module.exports = {
  CREDENTIALS_FILE,
  credentialsPath,
  loadCredentials,
  saveCredentials,
  recordDeploy,
  removeCredentials,
};
