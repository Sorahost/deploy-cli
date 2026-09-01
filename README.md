# SORAHOST deploy-cli

SORAHOST（PteWorker）へプロジェクトを公開するためのデプロイスクリプトです。

- Windows: `deploy.bat`
- macOS / Linux: `deploy.sh`

難しいインストール作業はありません。プロジェクトへスクリプトを置いて実行し、PteWorkerのコンソールに表示された「エンドポイント」と「デプロイトークン」を貼り付けます。

## はじめてのデプロイ

### 1. PteWorkerを起動する

PterodactylでPteWorkerを起動すると、コンソールに次の2つが表示されます。

- **エンドポイント**: デプロイ先のURL
- **デプロイトークン**: デプロイを許可するための秘密情報

トークンの全文が表示されるのは発行時の一度だけです。第三者に見せず、安全な場所へ保存してください。保存し忘れた場合は、PteWorkerのコンソールで `token rotate` を実行すると再発行できます。

### 2. スクリプトをプロジェクトへ置く

#### Windows

[`deploy.bat`](deploy.bat) をダウンロードし、デプロイしたいプロジェクトの一番上のフォルダーへ置きます。

```text
my-project/
├─ deploy.bat
├─ sorahost.json
├─ package.json
└─ dist/
```

#### macOS / Linux

プロジェクトの一番上のフォルダーで実行します。

```sh
curl -fsSLO https://raw.githubusercontent.com/Sorahost/deploy-cli/main/deploy.sh
chmod +x deploy.sh
```

### 3. `sorahost.json`を用意する

`sorahost.json`は「どのフォルダーやファイルを公開するか」をPteWorkerへ伝える設定ファイルです。プロジェクトの一番上に作成します。

一般的な静的サイト（Vite、React、Vueなど）の例です。

```json
{
  "mode": "static",
  "framework": "vite",
  "dir": "dist",
  "spa": true
}
```

`npm run build`などを先に実行し、`dir`で指定したフォルダーが実際に存在することを確認してください。デプロイスクリプト自体は、依存パッケージのインストールやビルドを行いません。

### 4. 実行する

Windowsは `deploy.bat` をダブルクリックします。コマンドプロンプトから実行しても構いません。

```bat
deploy.bat
```

macOS / Linuxはターミナルで実行します。

```sh
./deploy.sh
```

画面上で聞かれたら、手順1のエンドポイントとデプロイトークンを貼り付けます。トークンの入力内容は画面に表示されません。

```text
=== SORAHOSTへデプロイ ===
対象: /path/to/my-project

エンドポイント: https://example.com/_sorahost/...
デプロイトークン（入力内容は表示されません）:

[1/3] デプロイするファイルをまとめています...
[2/3] サーバーへアップロードしています...
[3/3] 公開が完了しました。

デプロイ成功！ サイトが新しい内容に切り替わりました。
```

## `sorahost.json`の例

### 静的サイト

ビルド結果が `dist/` にあり、SPAのルーティングを使う場合です。

```json
{
  "mode": "static",
  "framework": "vite",
  "dir": "dist",
  "spa": true
}
```

SPAではない場合は `"spa": false` にします。

### Node.jsアプリ

PteWorker上でNode.jsプロセスを起動する場合です。実行に必要なファイルと本番用の依存パッケージを、あらかじめプロジェクト内へ用意してください。

```json
{
  "mode": "node",
  "framework": "express",
  "start": "node dist/server.js"
}
```

アプリはPteWorkerから渡されるポート設定を使用し、外部へ直接公開せずループバックへバインドしてください。

### Worker

あらかじめES Module形式へバンドルした `worker.js` を実行する場合です。

```json
{
  "mode": "worker",
  "framework": "hono",
  "entry": "worker.js",
  "compatibilityDate": "2026-09-01"
}
```

## 別のフォルダーをデプロイする

スクリプトの引数にプロジェクトのパスを指定できます。

Windows:

```bat
deploy.bat C:\path\to\my-project
```

macOS / Linux:

```sh
./deploy.sh /path/to/my-project
```

## CIや自動デプロイで使う

対話入力の代わりに、次の環境変数を設定できます。

| 環境変数 | 内容 |
| --- | --- |
| `SORAHOST_ENDPOINT` | PteWorkerのエンドポイント |
| `SORAHOST_TOKEN` | デプロイトークン |

トークンはリポジトリへ直接書かず、利用しているCIサービスのSecretsへ登録してください。

```sh
SORAHOST_ENDPOINT="https://example.com/_sorahost/..." \
SORAHOST_TOKEN="your-secret-token" \
./deploy.sh
```

## アップロードされないファイル

認証情報や開発用ファイルの誤送信を防ぐため、次のファイルは自動的に除外されます。

- `.git/`
- `.env`、`.env.*`
- `.npmrc`
- `.netrc`
- `.DS_Store`

上記以外のファイルは原則としてArtifactへ含まれます。秘密鍵、バックアップ、ローカルデータベースなどをプロジェクト内へ置いている場合は、実行前に取り除いてください。

## よくあるエラー

### `sorahost.json が見つかりません`

スクリプトと同じフォルダーへ `sorahost.json` を作成してください。別のフォルダーをデプロイする場合は、そのパスを引数に指定します。

### `PteWorkerへ接続できませんでした`

- PteWorkerが起動しているか
- エンドポイントを省略せず最後までコピーしたか
- Pterodactylの割り当てポートへ外部から接続できるか

を確認してください。

### `HTTP 401` / `invalid deploy token`

トークンが一致していません。PteWorkerのコンソールで `token rotate` を実行し、新しく表示されたトークンでもう一度試してください。

### `HTTP 404`

エンドポイントが古い可能性があります。PteWorkerのコンソールで `url` を実行し、表示されたURLを使用してください。

### デプロイ後にアプリが起動しない

- `sorahost.json`の `mode`、`dir`、`entry`、`start` が正しいか
- ビルド結果が指定した場所に存在するか
- Node.jsアプリの実行に必要な依存パッケージが含まれているか

を確認してください。PteWorkerのコンソールで `logs` を実行すると、直近のログを確認できます。

## セキュリティ

- デプロイトークンはパスワードと同じように扱ってください。
- トークンをGitへコミットしたり、チャットやスクリーンショットで共有したりしないでください。
- 漏えいした可能性がある場合は、PteWorkerのコンソールで `token rotate` を実行してください。
- スクリプトはアップロードしたArtifactのSHA-256を送信し、PteWorker側で破損や改ざんを検出します。

## 必要なコマンド

- Windows 10以降: `curl.exe`、`tar.exe`、`certutil.exe`、Windows PowerShell
- macOS / Linux: `curl`、`tar`、`sha256sum`または`shasum`

## ライセンス

[MIT License](LICENSE)
