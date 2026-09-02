# sorahost-cli

SORAHOST（PteWorker）へプロジェクトを公開するためのCLIツールです。

```sh
npm install -g sorahost-cli
cd my-project
sorahost deploy
```

## インストール

Node.js 18 以降が必要です。

```sh
npm install -g sorahost-cli
```

`npx` で都度実行することもできます。

```sh
npx sorahost-cli deploy
```

> このリポジトリを clone して試す場合は、リポジトリ内で `npm install` のあと
> `npm link`（`sorahost` コマンドを作る）または `node bin/sorahost.js deploy` で実行します。

## はじめてのデプロイ

### 1. PteWorkerを起動する

PterodactylでPteWorkerを起動すると、コンソールに次の2つが表示されます。

- **エンドポイント**: デプロイ先のURL
- **デプロイトークン**: デプロイを許可するための秘密情報

トークンの全文が表示されるのは発行時の一度だけです。第三者に見せず、安全な場所へ保存してください。保存し忘れた場合は、PteWorkerのコンソールで `token rotate` を実行すると再発行できます。

### 2. `sorahost.json`を用意する

`sorahost.json` は「どのフォルダーやファイルを公開するか」をPteWorkerへ伝える設定ファイルです。プロジェクトの一番上に作成します。

`sorahost init` を実行すると、`package.json` からフレームワーク（Vite / Next.js / Astro / Hono など）を推測し、対話で作成できます。

```sh
sorahost init
```

```text
  ◆ sorahost init

  プロジェクト   my-app
  検出          Vite
  推奨設定       mode: static · dir: dist

  ? デプロイモード ❯ static
  ? 公開するフォルダー (dist) ❯
  ? SPA ルーティングを使う（未知のパスを index.html に返す） [Y/n] ❯

  ✓ sorahost.json を作成しました
```

手書きする場合の例です。

```json
{
  "mode": "static",
  "framework": "vite",
  "dir": "dist",
  "spa": true
}
```

`npm run build` などを先に実行し、`dir` で指定したフォルダーが実際に存在することを確認してください。このCLIは依存パッケージのインストールやビルドを行いません（`dir` が無い場合は、対話で `npm run build` を実行するか確認します）。

### 3. 実行する

プロジェクトの一番上のフォルダーで実行します。

```sh
sorahost deploy
```

画面上で聞かれたら、手順1のエンドポイントとデプロイトークンを貼り付けます。トークンの入力内容は画面に表示されません。

```text
  ◆ my-app をデプロイ

  対象    /path/to/my-app
  モード   static · dist/  (SPA)
  前回    3分前 · 成功

  ? エンドポイント ❯ https://example.com/_sorahost/…
  ? デプロイトークン ❯
  ✓ デプロイトークン ····a1b2
  ✓ 保存しました /path/to/my-app/.sorahost.json (600)

  ✓ 42 ファイル · 2.1 MB (圧縮後 480 KB)   1.2s
  ⠸ アップロード中  ███████████████░░░░░  74%  1.6 MB / 2.1 MB

  ✓ デプロイ成功   ·   4.6s

    サイト    https://example.com/
    ファイル   42
    サイズ    2.1 MB
```

保存すると、2回目以降は `sorahost deploy` だけでデプロイできます。

## コマンド

```text
sorahost [deploy] [パス]   プロジェクトをデプロイする（既定コマンド）
sorahost init    [パス]    sorahost.json を対話で作成する
sorahost login   [パス]    エンドポイントとトークンを保存する
sorahost logout  [パス]    保存した認証情報を削除する
sorahost whoami  [パス]    現在の設定と直近のデプロイを表示する
sorahost open    [パス]    公開サイトをブラウザーで開く
```

## オプション

```text
-y, --yes         確認を省略する（CI 向け）
    --dry-run     アップロードせず、送信内容とサイズだけ表示する
    --json        機械可読な JSON で結果を出力する
    --open        デプロイ成功後にブラウザーで開く
-q, --quiet       進捗表示を抑える
    --no-color    色を使わない（NO_COLOR 環境変数にも対応）
-h, --help        ヘルプを表示する
-v, --version     バージョンを表示する
```

パイプ実行・CI（TTY でない）環境では、自動的にアニメーションや色を止めた行ベースの出力になります。

### 別のフォルダーをデプロイする

```sh
sorahost deploy /path/to/my-project
```

## 認証情報の保存（毎回入力しない）

エンドポイントとトークンは、次の順で解決されます。

1. 環境変数 `SORAHOST_ENDPOINT` / `SORAHOST_TOKEN`
2. プロジェクト内の `.sorahost.json`
3. 画面入力（入力後に `.sorahost.json` への保存を確認します）

`.sorahost.json` はトークンを含むため、パーミッション `600` で保存し、`.gitignore` があれば自動で追記します。直近のデプロイ結果もここに記録され、`sorahost whoami` で確認できます。**絶対にコミットしないでください。**

```jsonc
// .sorahost.json（自動生成、Git 管理対象外）
{
  "endpoint": "https://example.com/_sorahost/...",
  "token": "your-secret-token",
  "lastDeployedAt": "2026-09-02T04:08:38.544Z",
  "lastDeployStatus": "success",
  "siteUrl": "https://example.com/"
}
```

先に保存だけしておくこともできます。

```sh
sorahost login          # エンドポイントとトークンを入力して保存
sorahost logout         # 保存した内容を削除
```

## アップロードサイズと除外設定

PteWorker 側にアップロードサイズの上限があります（既定 256 MB）。この上限は**サーバー（PteWorker）側の設定**で、CLI からは変更できません。上限を引き上げたい場合は PteWorker 側で設定してください。CLI 側では、送信前にサイズを表示し、上限を超えていればアップロードせずに中止します。`sorahost deploy --dry-run` で送信内容とサイズだけ確認できます。

不要なファイルを含めないことで、ほとんどの場合は上限に達しません。

- **`include`（推奨・どの mode でも有効）**: `sorahost.json` に送信するパスを列挙すると、それ以外は一切送りません。

  ```json
  {
    "mode": "node",
    "start": "node dist/standalone/server.js",
    "include": ["dist/standalone"]
  }
  ```

- **`mode` による自動絞り込み**（`include` 未指定時）
  - `"static"`: `sorahost.json` と `dir` で指定したフォルダーだけを送信します。
  - `"worker"`: `sorahost.json` と `entry` のファイルだけを送信します。
  - `"node"` など: プロジェクト全体を送信します（`include` で絞るのが安全です）。
- **`.sorahostignore`**: `.gitignore` と同じ書式で、追加の除外パターンを書けます。

```gitignore
# .sorahostignore の例
/node_modules      # 先頭の / でリポジトリ直下のみ。ネストした node_modules は残る
/.next
/.cache
*.log
/coverage
```

> **注意**: `node_modules`（先頭 `/` なし）と書くと、`dist/standalone/node_modules` のような
> **アプリの実行に必要なネストした node_modules も除外されます**。`node` アプリを
> `include` で送るときは `.sorahostignore` は基本的に不要です（`include` 外は元々送られません）。
> CLI は「node アプリなのに node_modules が1つも入っていない」場合に警告します。

- サーバー側の上限を変更した場合は、環境変数 `SORAHOST_MAX_UPLOAD_BYTES`（バイト数）で CLI の事前チェックも合わせられます。

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

## CIや自動デプロイで使う

対話入力の代わりに環境変数を設定し、`--yes` を付けます。`--json` で結果を機械可読に受け取れます。

| 環境変数 | 内容 |
| --- | --- |
| `SORAHOST_ENDPOINT` | PteWorkerのエンドポイント |
| `SORAHOST_TOKEN` | デプロイトークン |

トークンはリポジトリへ直接書かず、利用しているCIサービスのSecretsへ登録してください。

```sh
npm install -g sorahost-cli

SORAHOST_ENDPOINT="https://example.com/_sorahost/..." \
SORAHOST_TOKEN="your-secret-token" \
sorahost deploy --yes --json
```

`--yes` 実行時は、認証情報と `sorahost.json` が揃っていない場合はエラーで停止します（対話プロンプトは出ません）。

## アップロードされないファイル

認証情報や開発用ファイルの誤送信を防ぐため、次のファイルは常に除外されます。

- `.git/`
- `.env`、`.env.*`
- `.npmrc`
- `.netrc`
- `.DS_Store`
- `.sorahost.json`（保存した認証情報）

さらに `mode` による自動絞り込みと `.sorahostignore` が適用されます（「アップロードサイズと除外設定」を参照）。上記以外のファイルは原則としてArtifactへ含まれます。秘密鍵、バックアップ、ローカルデータベースなどをプロジェクト内へ置いている場合は、実行前に取り除いてください。

## よくあるエラー

CLI は失敗時に「何が起きたか / なぜか / どうすればよいか」を表示します。代表的なもの:

### `sorahost.json が見つかりません`

`sorahost init` で作成できます。別のフォルダーをデプロイする場合は、そのパスを引数に指定します。

### `サーバーに接続を拒否されました` / `サーバーが見つかりません`

- PteWorkerが起動しているか
- エンドポイントを省略せず最後までコピーしたか
- Pterodactylの割り当てポートへ外部から接続できるか

を確認してください。PteWorkerのコンソールで `url` を実行すると、正しいエンドポイントを再表示できます。

### `デプロイトークンが受け付けられませんでした`（HTTP 401 / 403）

トークンが一致していません。PteWorkerのコンソールで `token rotate` を実行し、`sorahost login` で新しいトークンを設定し直してください。

### `エンドポイントが見つかりませんでした`（HTTP 404）

エンドポイントが古い可能性があります。PteWorkerのコンソールで `url` を実行し、表示されたURLで `sorahost login` をやり直してください。

### `アップロードサイズが上限を超えています`

「アップロードサイズと除外設定」を参照し、`sorahost.json` の `include` で送るフォルダーを限定してください。上限そのものを変更するには PteWorker 側の設定が必要です。

### デプロイ後にアプリが起動しない

- `sorahost.json` の `mode`、`dir`、`entry`、`start` が正しいか
- ビルド結果が指定した場所に存在するか
- Node.jsアプリの実行に必要な依存パッケージが含まれているか

を確認してください。PteWorkerのコンソールで `logs` を実行すると、直近のログを確認できます。

## セキュリティ

- デプロイトークンはパスワードと同じように扱ってください。
- トークンをGitへコミットしたり、チャットやスクリーンショットで共有したりしないでください。
- 漏えいした可能性がある場合は、PteWorkerのコンソールで `token rotate` を実行してください。
- CLIはアップロードしたArtifactのSHA-256を送信し、PteWorker側で破損や改ざんを検出します。

## ライセンス

[MIT License](LICENSE)
