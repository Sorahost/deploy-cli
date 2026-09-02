'use strict';

class UsageError extends Error {
  constructor(message, hint) {
    super(message);
    this.name = 'UsageError';
    this.hint = hint;
  }
}

// 画面へ丁寧に表示できる想定内のエラー。
class DeployError extends Error {
  constructor(title, { detail, hint, command, code, status } = {}) {
    super(title);
    this.name = 'DeployError';
    this.title = title;
    this.detail = detail;
    this.hint = hint;
    this.command = command;
    this.code = code || 'ERROR';
    this.status = status;
  }
}

function safeHost(endpoint) {
  try {
    return new URL(endpoint).host;
  } catch {
    return endpoint || 'サーバー';
  }
}

function extractServerMessage(body) {
  const text = String(body || '').trim();
  if (!text) return '';
  try {
    const json = JSON.parse(text);
    const msg = json.error || json.message || json.detail;
    if (typeof msg === 'string') return msg;
  } catch {
    /* プレーンテキスト */
  }
  return text.split('\n')[0].slice(0, 200);
}

function fromNetworkError(err, endpoint) {
  const host = safeHost(endpoint);
  const table = {
    ENOTFOUND: {
      title: 'サーバーが見つかりません',
      detail: `${host} を名前解決できませんでした。\nエンドポイントのURLに打ち間違いがないか確認してください。`,
      code: 'DNS',
    },
    EAI_AGAIN: {
      title: 'ネットワークに接続できません',
      detail: `${host} を名前解決できませんでした。ネットワーク接続を確認してください。`,
      code: 'DNS',
    },
    ECONNREFUSED: {
      title: 'サーバーに接続を拒否されました',
      detail: `${host} が接続を拒否しました。\nPteWorker が起動しているか、Pterodactyl の割り当てポートへ外部から届くかを確認してください。`,
      code: 'CONN_REFUSED',
    },
    ETIMEDOUT: {
      title: '接続がタイムアウトしました',
      detail: `${host} から時間内に応答がありませんでした。`,
      code: 'TIMEOUT',
    },
    ECONNRESET: {
      title: '接続が切断されました',
      detail: 'アップロードの途中で接続が切れました。もう一度実行してください。',
      code: 'CONN_RESET',
    },
    CERT_HAS_EXPIRED: {
      title: 'サーバー証明書の期限が切れています',
      detail: `${host} のTLS証明書が期限切れです。`,
      code: 'TLS',
    },
  };
  const info = table[err.code] || {
    title: 'サーバーへ接続できませんでした',
    detail: err.message,
    code: err.code || 'NETWORK',
  };
  return new DeployError(info.title, {
    detail: info.detail,
    code: info.code,
    hint: 'PteWorker のコンソールで url を実行し、表示されたエンドポイントを使ってください。',
  });
}

const SIZE_HINT =
  '送信するファイルを絞ってください:\n' +
  '  - sorahost.json に "include": ["dist/standalone"] のように、送るフォルダーだけを列挙する\n' +
  '  - .sorahostignore に node_modules/ などを追加する（.gitignore と同じ書式）\n' +
  '  - mode:"static" は "dir"、mode:"worker" は "entry" だけが自動で送られます\n' +
  '  - まず sorahost deploy --dry-run で中身とサイズを確認できます';

function fromHttpStatus(status, body, endpoint) {
  const serverMsg = extractServerMessage(body);
  const withMsg = (base) => (serverMsg ? `${base}\n\nサーバーからの応答: ${serverMsg}` : base);

  if (status === 401 || status === 403) {
    return new DeployError('デプロイトークンが受け付けられませんでした', {
      status,
      code: `HTTP_${status}`,
      detail: withMsg(`HTTP ${status} ${'·'} トークンが正しくないか、権限がありません。`),
      hint: 'PteWorker のコンソールで token rotate を実行し、新しいトークンで設定し直してください。',
      command: 'sorahost login',
    });
  }
  if (status === 404) {
    return new DeployError('エンドポイントが見つかりませんでした', {
      status,
      code: 'HTTP_404',
      detail: withMsg(
        `HTTP 404 ${'·'} ${safeHost(endpoint)} にデプロイ先が見つかりません。エンドポイントが古い可能性があります。`,
      ),
      hint: 'PteWorker のコンソールで url を実行し、表示されたURLで設定し直してください。',
      command: 'sorahost login',
    });
  }
  if (status === 413 || /limit|exceed|too large|payload/i.test(body || '')) {
    return new DeployError('アップロードサイズが上限を超えています', {
      status,
      code: 'PAYLOAD_TOO_LARGE',
      detail: withMsg(`HTTP ${status} ${'·'} 送信データが大きすぎます。`),
      hint: SIZE_HINT,
      command: 'sorahost deploy --dry-run',
    });
  }
  if (status >= 500) {
    return new DeployError('サーバー側でエラーが発生しました', {
      status,
      code: `HTTP_${status}`,
      detail: withMsg(`HTTP ${status} ${'·'} PteWorker 側の処理に失敗しました。`),
      hint: '時間をおいて再実行し、PteWorker のコンソールで logs を確認してください。',
    });
  }
  return new DeployError('デプロイに失敗しました', {
    status,
    code: `HTTP_${status}`,
    detail: withMsg(`HTTP ${status}`),
    hint: 'PteWorker のコンソールで logs を確認してください。',
  });
}

module.exports = {
  UsageError,
  DeployError,
  fromNetworkError,
  fromHttpStatus,
  extractServerMessage,
  SIZE_HINT,
};
