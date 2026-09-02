'use strict';

const readline = require('node:readline');
const { c, sym } = require('./ui');

const isTTY = Boolean(process.stdin.isTTY);
let iface = null;
let ttyClosed = false;

// --- パイプ入力（CI・テスト）用の行キュー ---------------------------------
const queued = [];
const waiters = [];
let closed = false;

function pipedInterface() {
  if (!iface && !closed) {
    iface = readline.createInterface({ input: process.stdin, terminal: false });
    iface.on('line', (line) => {
      const w = waiters.shift();
      if (w) w(line);
      else queued.push(line);
    });
    iface.on('close', () => {
      closed = true;
      while (waiters.length) waiters.shift()('');
    });
  }
  return iface;
}

function nextLine() {
  if (queued.length) return Promise.resolve(queued.shift());
  if (closed) return Promise.resolve('');
  return new Promise((resolve) => waiters.push(resolve));
}

async function askPiped(query) {
  pipedInterface();
  process.stdout.write(query);
  const line = await nextLine();
  // terminal:false では入力エコーが無いので、ログが読めるように補う。
  process.stdout.write(`${line}\n`);
  return line;
}

function inputClosed() {
  return isTTY ? ttyClosed : closed;
}

// --- 対話端末（TTY）用 -------------------------------------------------
function ttyInterface() {
  if (!iface) {
    iface = readline.createInterface({
      input: process.stdin,
      output: process.stdout,
      terminal: true,
    });
    iface.on('close', () => {
      iface = null;
      ttyClosed = true;
    });
  }
  return iface;
}

function askTty(query) {
  const rl = ttyInterface();
  return new Promise((resolve) => rl.question(query, resolve));
}

function askHiddenTty(query) {
  const rl = ttyInterface();
  return new Promise((resolve) => {
    const original = rl._writeToOutput.bind(rl);
    let muted = false;
    rl._writeToOutput = (str) => {
      if (!muted || str.startsWith(query)) original(str);
    };
    rl.question(query, (answer) => {
      rl._writeToOutput = original;
      rl.output.write('\n');
      resolve(answer);
    });
    muted = true;
  });
}

// --- 低レベル API ----------------------------------------------------
function ask(query) {
  return isTTY ? askTty(query) : askPiped(query);
}

function askHidden(query) {
  return isTTY ? askHiddenTty(query) : askPiped(query);
}

function closePrompt() {
  if (iface) {
    try {
      iface.close();
    } catch {
      /* noop */
    }
  }
}

// --- 高レベルプロンプト --------------------------------------------------
const Q = () => `  ${c.cyan('?')} `;

class NoInputError extends Error {
  constructor() {
    super('入力がありません');
    this.name = 'NoInputError';
  }
}

async function text({ message, initial, validate }) {
  const hint = initial ? c.gray(` (${initial})`) : '';
  for (;;) {
    const raw = (await ask(`${Q()}${c.bold(message)}${hint} ${c.gray(sym.pointer)} `)).trim();
    const value = raw || initial || '';
    if (validate) {
      const result = validate(value);
      if (result !== true) {
        if (inputClosed()) throw new NoInputError();
        process.stdout.write(`  ${c.red(sym.cross)} ${result}\n`);
        continue;
      }
    }
    if (!value && !initial && inputClosed()) throw new NoInputError();
    return value;
  }
}

async function password({ message }) {
  const value = (await askHidden(`${Q()}${c.bold(message)} ${c.gray(sym.pointer)} `)).trim();
  return value;
}

async function confirm({ message, initial = true }) {
  const suffix = initial ? '[Y/n]' : '[y/N]';
  const raw = (await ask(`${Q()}${c.bold(message)} ${c.gray(suffix)} `)).trim().toLowerCase();
  if (!raw) return initial;
  return raw === 'y' || raw === 'yes';
}

async function fallbackSelect({ message, choices, initial }) {
  const list = choices
    .map((ch, i) => `    ${c.cyan(String(i + 1))}) ${ch.label}${ch.hint ? c.gray(`  ${ch.hint}`) : ''}`)
    .join('\n');
  const raw = (await ask(`${Q()}${c.bold(message)}\n${list}\n  ${c.gray(sym.pointer)} `)).trim();
  if (!raw) return choices[initial].value;
  const n = Number(raw);
  if (Number.isInteger(n) && n >= 1 && n <= choices.length) return choices[n - 1].value;
  const match = choices.find((ch) => ch.value === raw || ch.label === raw);
  return match ? match.value : choices[initial].value;
}

// 矢印キーで選ぶセレクト。TTY 以外では番号入力にフォールバック。
function select({ message, choices, initial = 0 }) {
  const start = Math.max(0, Math.min(choices.length - 1, initial));
  if (!isTTY) return fallbackSelect({ message, choices, initial: start });

  const stdin = process.stdin;
  const stdout = process.stdout;

  return new Promise((resolve) => {
    let idx = start;
    let rendered = 0;

    const paint = () => {
      if (rendered) stdout.write(`\x1b[${rendered}A`);
      const lines = [`  ${c.cyan('?')} ${c.bold(message)}`];
      choices.forEach((ch, i) => {
        const on = i === idx;
        const label = on ? c.cyan(ch.label) : ch.label;
        const hint = ch.hint ? c.gray(`  ${ch.hint}`) : '';
        lines.push(on ? `  ${c.cyan(sym.pointer)} ${label}${hint}` : `    ${label}${hint}`);
      });
      stdout.write(`${lines.map((l) => `${l}\x1b[K`).join('\n')}\n`);
      rendered = lines.length;
    };

    const finish = (aborted) => {
      stdin.removeListener('keypress', onKey);
      if (stdin.isTTY) stdin.setRawMode(false);
      stdin.pause();
      stdout.write(`\x1b[${rendered}A\x1b[J`);
      stdout.write('\x1b[?25h');
      if (aborted) {
        stdout.write('\n');
        process.exit(130);
      }
      stdout.write(
        `  ${c.green(sym.tick)} ${c.bold(message)} ${c.gray(sym.pointer)} ${c.cyan(choices[idx].label)}\n`,
      );
      resolve(choices[idx].value);
    };

    const onKey = (_str, key) => {
      if (!key) return;
      if (key.ctrl && key.name === 'c') return finish(true);
      if (key.name === 'up' || key.name === 'k') {
        idx = (idx - 1 + choices.length) % choices.length;
        paint();
      } else if (key.name === 'down' || key.name === 'j') {
        idx = (idx + 1) % choices.length;
        paint();
      } else if (key.name === 'return' || key.name === 'enter') {
        finish(false);
      }
    };

    readline.emitKeypressEvents(stdin);
    stdin.setRawMode(true);
    stdin.resume();
    stdout.write('\x1b[?25l');
    paint();
    stdin.on('keypress', onKey);
  });
}

module.exports = {
  ask,
  askHidden,
  closePrompt,
  inputClosed,
  text,
  password,
  confirm,
  select,
  NoInputError,
};
