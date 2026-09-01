# sorahost

プロジェクトを手元の環境でビルドし、完成した成果物をサーバーへデプロイする CLI です。

`sorahost` は依存関係のインストール、ビルド、単一 Artifact への梱包、[SORAHOST Deploy サーバー][runtime]へのアップロードを行います。サーバーは Artifact を検証して新しいリリースへ展開し、稼働対象を切り替えます。サーバー側でパッケージのインストールやビルドスクリプトの実行は行いません。

```console
$ sorahost deploy
    project /home/you/my-app  target https://app.example.com
    detected next (node), package manager pnpm
[1/4] Installing dependencies
    done (7.4s)
[2/4] Building (pnpm run build)
    done (31.2s)
[3/4] Packaging artifact
    done 48.1 MB in 1,204 files, 14.7 MB compressed (2.1s)
[4/4] Uploading and activating
    done (5.8s)

done deployed 20260901T104530Z-1f4a9c02
  serving next, node
```

## ローカルでビルドする理由

ビルドにはロックファイル、ツールチェイン、`node_modules`、ネットワーク接続が必要です。サーバー側でビルドすると、サーバーにもこれらすべてが必要になり、デバッグしにくい場所でビルドが失敗する可能性が生まれます。

クライアント側でビルドすることで、サーバーの責務をダイジェストの確認、安全な展開、再起動だけに限定できます。手元で確認した Artifact と、実際にサーバーで動作する Artifact はバイト単位で同一です。

## インストール

### ビルド済みバイナリ

[Releases ページ][releases]から利用環境に合ったファイルをダウンロードし、`sorahost` を `PATH` の通った場所へ配置してください。

| OS | アーキテクチャ | 配布形式 |
| --- | --- | --- |
| Linux | amd64 / arm64 | `.tar.gz` |
| macOS | Intel（amd64）/ Apple Silicon（arm64） | `.tar.gz` |
| Windows | amd64 / arm64 | `.zip` |

各リリースには検証用の `checksums.txt` も添付されます。

### `go install`

Go 1.25 以降が必要です。

```sh
go install github.com/Sorahost/deploy-cli@latest
```

### ソースコードからビルド

```sh
git clone https://github.com/Sorahost/deploy-cli.git
cd deploy-cli
go build -o sorahost .
```

## はじめに

SORAHOST Deploy サーバーを起動すると、コンソールにエンドポイント URL とデプロイトークンが表示されます。トークンが平文で表示されるのは発行時の一度だけなので、その場で安全な場所へ保存してください。

プロジェクトのディレクトリで、サーバーとの接続設定を行います。

```sh
cd my-app
sorahost link
```

この操作により、プロジェクトへ `sorahost.json` が作成され、トークンはユーザー設定ディレクトリへ保存されます。`sorahost.json` はコミットできますが、ユーザー設定ディレクトリの認証情報はコミットしないでください。

接続後は、次のコマンドでデプロイできます。

```sh
sorahost deploy
```

## コマンド

| コマンド | 内容 |
| --- | --- |
| `sorahost link` | プロジェクトをサーバーへ接続します。`login` も別名として利用できます。 |
| `sorahost deploy` | 依存関係をインストールし、ビルド、梱包、アップロードを行います。 |
| `sorahost status` | 稼働中のリリースとロールバック可能なリリースを表示します。 |
| `sorahost logs` | サーバーの直近のログを表示します。`-f` で継続的に取得します。 |
| `sorahost rollback` | 以前のリリースを有効にします。 |
| `sorahost logout` | この端末に保存されたトークンを削除します。 |

グローバルオプションはコマンドの前後どちらにも指定できます。

- `--cwd`
- `-v` / `--verbose`
- `--quiet`
- `--json`
- `-y` / `--yes`
- `--color` / `--no-color`

詳細は `sorahost <command> --help` で確認できます。

## 対応プロジェクト

依存パッケージ、設定ファイル、`package.json` のスクリプトからプロジェクトの種類を判定します。プロジェクト自身に `build` または `start` スクリプトがある場合は、実際に利用者が確認した設定を尊重するため、フレームワークの既定値より優先します。

| 検出対象 | 配信方法 |
| --- | --- |
| Cloudflare Workers、Hono | `worker` — CLI が単一モジュールへバンドル |
| Next.js | `node`（standalone build）、または `output: "export"` の場合は `static` |
| Nuxt | `.output` を使用する `node` |
| SvelteKit | `adapter-node` 使用時は `node`、それ以外は `static` |
| Astro | `@astrojs/node` 使用時は `node`、それ以外は `static` |
| Remix、NestJS | `node` |
| Vite（React、Vue、Svelte、Solid など）、Vue CLI、CRA、Angular | SPA fallback が有効な `static` |
| Eleventy | `static` |
| Express、Fastify、Koa、hapi | `node` |
| `index.html` が存在するディレクトリ | `static` |

判定できない場合や判定結果を変更したい場合は、次節の設定を明示してください。

## `sorahost.json`

`sorahost link` がプロジェクトへ作成する設定ファイルです。秘密情報は含まれないため、リポジトリへコミットできます。

```jsonc
{
  "endpoint": "https://app.example.com/_sorahost/Q8y10DRDZffm7fOnVcfHeg",
  "name": "my-app",

  // 以下はすべて任意です。自動判定の結果を上書きします。
  "mode": "node",                         // worker | static | node
  "installCommand": "pnpm install --frozen-lockfile",
  "buildCommand": "pnpm run build",
  "startCommand": "node dist/server.js", // node モード
  "outputDirectory": "dist",             // static モード
  "entry": "src/worker.ts",               // worker モード
  "spa": true,                             // static モードの index.html fallback

  // デプロイに埋め込む、秘密情報ではない設定値
  "vars": { "API_BASE": "https://api.example.com" },

  // アプリへ公開するサーバー環境変数の名前
  "env": ["FEATURE_FLAGS"],

  "compatibilityDate": "2025-08-01",
  "compatibilityFlags": []
}
```

### `.sorahostignore`

`node` プロジェクトの梱包対象からファイルを除外する設定です。`.gitignore` と同様に、1 行につき1つの glob パターンを記述します。

- `/` を含まない名前は、すべての階層で一致します。
- `/` を含むパターンは、プロジェクトルートを基準に評価されます。

## 秘密情報の取り扱い

CLI は、利用者が明示的に指定しない限り秘密情報を Artifact に含めない設計です。

- トークンはプロジェクトの外に保存されます。
  - Windows: `%AppData%\sorahost\credentials.json`
  - macOS: `~/Library/Application Support/sorahost/credentials.json`
  - Linux: `$XDG_CONFIG_HOME/sorahost/credentials.json`
- 認証情報ファイルの権限は、対応する環境では `0600` に制限されます。
- `sorahost.json` にトークンは保存されません。
- `.env`、秘密鍵、`.npmrc`、`.netrc` は Artifact に含まれません。
- 実行時の秘密情報はサーバーパネルの環境変数に保存し、その変数名だけを `env` に指定してください。
- ランダムな API パスを含むエンドポイントは秘密情報として扱われ、進捗表示やエラーから除去されます。
- `sorahost link --token` はシェル履歴へトークンが残るため警告を表示します。可能な限り、入力内容を表示しない対話プロンプトを利用してください。

## CI からのデプロイ

`SORAHOST_ENDPOINT` と `SORAHOST_TOKEN` の2つの環境変数を設定すると、`sorahost link` やプロジェクト設定ファイルなしでデプロイできます。標準入力が端末ではない場合、CLI が対話プロンプトを表示することはありません。

```yaml
# .github/workflows/deploy.yml
name: Deploy

on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 22
      - name: sorahost をインストール
        run: |
          curl -fsSL https://github.com/Sorahost/deploy-cli/releases/latest/download/sorahost_linux_amd64.tar.gz \
            | tar -xz -C /usr/local/bin sorahost
      - name: デプロイ
        env:
          SORAHOST_ENDPOINT: ${{ secrets.SORAHOST_ENDPOINT }}
          SORAHOST_TOKEN: ${{ secrets.SORAHOST_TOKEN }}
        run: sorahost deploy --yes --json
```

終了コードは次のとおりです。

| コード | 意味 |
| --- | --- |
| `0` | 成功 |
| `1` | コマンドの実行に失敗 |
| `2` | コマンドライン引数が不正 |

## アップロードされる内容

Artifact は `sorahost.json` マニフェストを含む gzip 圧縮された tar アーカイブです。

| モード | 内容 |
| --- | --- |
| `worker` | esbuild でバンドルした `worker.js`。存在する場合は `public/` も同梱 |
| `static` | ビルド済みの `public/` |
| `node` | 自己完結型のフレームワークビルド、またはソースコードと本番用依存関係のみ |

アーカイブには通常のファイルとディレクトリだけを格納します。パスは相対 POSIX パスへ統一され、権限とタイムスタンプは固定されます。シンボリックリンクとハードリンクは格納しません。このため、内容が同じプロジェクトからは同一ダイジェストの Artifact が生成されます。

## トラブルシューティング

### `this directory is not linked to a SORAHOST server`

`sorahost link` を実行するか、`SORAHOST_ENDPOINT` と `SORAHOST_TOKEN` を設定してください。

### `the deploy token was rejected`

トークンが更新されています。サーバーコンソールで `token rotate` を実行して新しいトークンを発行し、もう一度 `sorahost link` を実行してください。

### ビルドに失敗する

`-v` を付けて再実行すると、パッケージマネージャーとバンドラーの詳しい出力を確認できます。通常表示では末尾数千文字だけが表示されます。

### `could not work out how to build this project`

`sorahost.json` に `mode`、`buildCommand`、`outputDirectory` を明示してください。

### `node` デプロイがローカルでは起動するが、サーバーでは起動しない

Artifact に含まれるのは本番用依存関係だけです。実行時に必要なパッケージが `devDependencies` ではなく `dependencies` に含まれていることを確認してください。

## 開発

```sh
go build ./...
go test ./...
go vet ./...
gofmt -l .
```

| パッケージ | 責務 |
| --- | --- |
| `internal/cli` | コマンド、オプション、ヘルプ、終了コード |
| `internal/config` | `sorahost.json` と認証情報ストア |
| `internal/detect` | フレームワークとパッケージマネージャーの判定 |
| `internal/build` | インストールとビルドの実行、バンドル、Artifact の準備 |
| `internal/artifact` | tar.gz の生成とダイジェスト計算 |
| `internal/api` | Deploy Agent API クライアント |
| `internal/ui` | ターミナル表示 |

## リリース

`v` から始まるタグを GitHub へ push すると、GitHub Actions がテストと静的解析を行い、Linux、macOS、Windows 向けの amd64／arm64 バイナリをビルドして GitHub Release へ自動的に公開します。

## ライセンス

MIT。詳細は [LICENSE](LICENSE) を参照してください。

[runtime]: https://github.com/techfish-11/pteworker
[releases]: https://github.com/Sorahost/deploy-cli/releases
