#!/usr/bin/env node
'use strict';

const { main } = require('../lib/cli');

main(process.argv)
  .then((code) => {
    process.exitCode = code || 0;
  })
  .catch((err) => {
    try {
      require('../lib/ui').restoreCursor();
    } catch {
      /* noop */
    }
    const message = err && err.message ? err.message : String(err);
    process.stderr.write(`\n  \x1b[31m✗\x1b[39m 予期しないエラー: ${message}\n`);
    if (process.env.SORAHOST_DEBUG && err && err.stack) {
      process.stderr.write(`\n${err.stack}\n`);
    }
    process.stderr.write('\n  詳細ログ: SORAHOST_DEBUG=1 を付けて再実行してください。\n');
    process.stderr.write('  https://github.com/Sorahost/deploy-cli\n\n');
    process.exitCode = 1;
  });
