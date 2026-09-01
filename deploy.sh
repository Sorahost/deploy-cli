#!/usr/bin/env sh
set -eu

REPOSITORY_URL="https://github.com/Sorahost/deploy-cli"

fail() {
  printf '\nエラー: %s\n' "$1" >&2
  printf '困ったとき: %s\n' "$REPOSITORY_URL" >&2
  exit 1
}

usage() {
  cat <<'EOF'
SORAHOST デプロイスクリプト（macOS / Linux）

使い方:
  1. デプロイしたいプロジェクトへ deploy.sh を置きます
  2. chmod +x deploy.sh を実行します（初回のみ）
  3. ./deploy.sh を実行します

別のフォルダーを指定する場合:
  ./deploy.sh /path/to/project

エンドポイントとトークンは、未設定なら画面上で入力できます。
自動実行では SORAHOST_ENDPOINT と SORAHOST_TOKEN を設定してください。
EOF
}

if [ "${1:-}" = "-h" ] || [ "${1:-}" = "--help" ]; then
  usage
  exit 0
fi

command -v curl >/dev/null 2>&1 || fail "curl が見つかりません。先にインストールしてください"
command -v tar >/dev/null 2>&1 || fail "tar が見つかりません。先にインストールしてください"

project_dir=${1:-.}
[ -d "$project_dir" ] || fail "プロジェクトのフォルダーが見つかりません: $project_dir"
project_dir=$(cd "$project_dir" && pwd -P)
[ -f "$project_dir/sorahost.json" ] || fail "sorahost.json が見つかりません。ビルド済みプロジェクトのルートで実行してください: $project_dir"

endpoint=${SORAHOST_ENDPOINT:-}
token=${SORAHOST_TOKEN:-}
printf '\n=== SORAHOSTへデプロイ ===\n'
printf '対象: %s\n\n' "$project_dir"
if [ -z "$endpoint" ]; then
  printf 'PteWorkerのコンソールに表示された「エンドポイント」を貼り付けてください。\n'
  printf 'エンドポイント: '
  IFS= read -r endpoint
fi
if [ -z "$token" ]; then
  printf '\nPteWorkerのコンソールに表示された「デプロイトークン」を貼り付けてください。\n'
  printf 'デプロイトークン（入力内容は表示されません）: '
  if [ -t 0 ]; then stty -echo; fi
  IFS= read -r token
  if [ -t 0 ]; then stty echo; fi
  printf '\n'
fi
[ -n "$endpoint" ] || fail "エンドポイントが入力されていません"
[ -n "$token" ] || fail "デプロイトークンが入力されていません"
case "$endpoint" in
  http://*|https://*) ;;
  *) fail "エンドポイントは http:// または https:// で始まるURLを入力してください" ;;
esac
endpoint=${endpoint%/}

tmp_dir=$(mktemp -d 2>/dev/null || mktemp -d -t sorahost)
artifact="$tmp_dir/artifact.tar.gz"
cleanup() { rm -rf "$tmp_dir"; }
trap cleanup EXIT HUP INT TERM

printf '\n[1/3] デプロイするファイルをまとめています...\n'
tar -czf "$artifact" -C "$project_dir" \
  --exclude=.git --exclude=.git/'*' \
  --exclude=.env --exclude='.env.*' \
  --exclude=.npmrc --exclude=.netrc \
  --exclude=.DS_Store \
  . || fail "ファイルをまとめられませんでした"

if command -v sha256sum >/dev/null 2>&1; then
  digest=$(sha256sum "$artifact" | awk '{print $1}')
elif command -v shasum >/dev/null 2>&1; then
  digest=$(shasum -a 256 "$artifact" | awk '{print $1}')
else
  fail "SHA-256を計算するコマンド（sha256sum / shasum）が見つかりません"
fi

printf '[2/3] サーバーへアップロードしています...\n'
response="$tmp_dir/response.json"
status=$(curl --silent --show-error --output "$response" --write-out '%{http_code}' \
  --request POST \
  --header "Authorization: Bearer $token" \
  --header 'Content-Type: application/gzip' \
  --header "X-Artifact-Sha256: $digest" \
  --data-binary "@$artifact" \
  --max-time 1800 \
  "$endpoint/deploy") || fail "PteWorkerへ接続できませんでした。エンドポイントとサーバーの起動状態を確認してください"

case "$status" in
  2??)
    printf '[3/3] 公開が完了しました。\n\n'
    printf 'デプロイ成功！ サイトが新しい内容に切り替わりました。\n'
    ;;
  *)
    printf '\nエラー: デプロイに失敗しました（HTTP %s）\n' "$status" >&2
    cat "$response" >&2
    printf '\nSee %s\n' "$REPOSITORY_URL" >&2
    exit 1
    ;;
esac
