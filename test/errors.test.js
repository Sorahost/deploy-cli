'use strict';

const test = require('node:test');
const assert = require('node:assert/strict');

const { fromHttpStatus, fromNetworkError, DeployError } = require('../lib/errors');

test('HTTP ステータス -> 分かりやすいエラー', () => {
  assert.equal(fromHttpStatus(401, '{"error":"nope"}').code, 'HTTP_401');
  assert.match(fromHttpStatus(401, '').command, /login/);
  assert.equal(fromHttpStatus(404, '', 'https://x.example/y').code, 'HTTP_404');
  assert.equal(fromHttpStatus(413, '').code, 'PAYLOAD_TOO_LARGE');
  assert.equal(fromHttpStatus(400, 'upload exceeds the 123 byte limit').code, 'PAYLOAD_TOO_LARGE');
  assert.equal(fromHttpStatus(503, '').code, 'HTTP_503');
  assert.ok(fromHttpStatus(418, '') instanceof DeployError);
});

test('サーバーメッセージを取り込む', () => {
  const e = fromHttpStatus(400, '{"error":"bad artifact"}');
  assert.match(e.detail, /bad artifact/);
});

test('ネットワークエラー -> errno ごとの説明', () => {
  assert.equal(fromNetworkError({ code: 'ENOTFOUND' }, 'https://h.example').code, 'DNS');
  assert.equal(fromNetworkError({ code: 'ECONNREFUSED' }, 'https://h.example').code, 'CONN_REFUSED');
  assert.equal(fromNetworkError({ code: 'ETIMEDOUT' }, 'https://h.example').code, 'TIMEOUT');
  assert.equal(fromNetworkError({ code: 'WEIRD', message: 'x' }, '').code, 'WEIRD');
});
