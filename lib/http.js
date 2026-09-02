'use strict';

const fs = require('node:fs');
const crypto = require('node:crypto');
const http = require('node:http');
const https = require('node:https');

function sha256File(file) {
  return new Promise((resolve, reject) => {
    const hash = crypto.createHash('sha256');
    const stream = fs.createReadStream(file);
    stream.on('error', reject);
    stream.on('data', (chunk) => hash.update(chunk));
    stream.on('end', () => resolve(hash.digest('hex')));
  });
}

function parseBody(body) {
  try {
    return JSON.parse(body);
  } catch {
    return null;
  }
}

function upload({ endpoint, token, artifactPath, digest, onProgress, timeoutMs }) {
  const url = new URL(`${endpoint}/deploy`);
  const mod = url.protocol === 'https:' ? https : http;
  const total = fs.statSync(artifactPath).size;

  return new Promise((resolve, reject) => {
    const req = mod.request(
      url,
      {
        method: 'POST',
        headers: {
          Authorization: `Bearer ${token}`,
          'Content-Type': 'application/gzip',
          'X-Artifact-Sha256': digest,
          'Content-Length': total,
          'User-Agent': 'sorahost-cli',
        },
      },
      (res) => {
        const chunks = [];
        res.on('data', (chunk) => chunks.push(chunk));
        res.on('end', () => {
          const body = Buffer.concat(chunks).toString('utf8');
          resolve({ status: res.statusCode, body, json: parseBody(body) });
        });
      },
    );

    req.setTimeout(timeoutMs || 1800 * 1000, () => {
      req.destroy(Object.assign(new Error('timeout'), { code: 'ETIMEDOUT' }));
    });
    req.on('error', reject);

    let sent = 0;
    const source = fs.createReadStream(artifactPath);
    source.on('error', reject);
    source.on('data', (chunk) => {
      sent += chunk.length;
      if (onProgress) onProgress(sent / total, sent, total);
    });
    source.pipe(req);
  });
}

module.exports = { sha256File, upload };
