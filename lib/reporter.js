'use strict';

const {
  c,
  sym,
  createSpinner,
  humanDuration,
  progressBar,
} = require('./ui');

function indent(text, n = 4) {
  const pad = ' '.repeat(n);
  return String(text)
    .split('\n')
    .map((line) => (line ? pad + line : line))
    .join('\n');
}

function rowsBlock(rows, write, pad = 2) {
  const visible = rows.filter(([, v]) => v != null && v !== '');
  if (visible.length === 0) return;
  const width = Math.max(...visible.map(([k]) => [...k].length));
  for (const [k, v] of visible) {
    write(`${' '.repeat(pad)}${c.gray(k.padEnd(width))}   ${v}\n`);
  }
}

const nullTask = {
  update() {},
  progress() {},
  succeed() {},
  fail() {},
  warn() {},
  stop() {},
};

// level: 'pretty' | 'plain' | 'json' | 'quiet'
function createReporter(level) {
  const silent = level === 'json' || level === 'quiet';
  const out = process.stdout;
  const err = process.stderr;
  const write = (s) => {
    if (!silent) out.write(s);
  };
  let active = null;

  const api = {
    level,

    blank() {
      write('\n');
    },

    line(text = '') {
      write(`${text}\n`);
    },

    header({ title, rows = [] }) {
      if (silent) return;
      write(`\n  ${c.cyan(sym.diamond)} ${c.bold(title)}\n\n`);
      rowsBlock(rows, write);
      write('\n');
    },

    task(text) {
      if (silent) return nullTask;

      if (level === 'plain') {
        const started = Date.now();
        write(`  ${c.gray(sym.arrow)} ${text}\n`);
        const finish = (symbol, t, withTime) =>
          write(
            `  ${symbol} ${t == null ? text : t}${
              withTime ? c.gray(`  ${humanDuration(Date.now() - started)}`) : ''
            }\n`,
          );
        return {
          update() {},
          progress() {},
          succeed: (t) => finish(c.green(sym.tick), t, true),
          fail: (t) => finish(c.red(sym.cross), t),
          warn: (t) => finish(c.yellow(sym.warn), t),
          stop() {},
        };
      }

      const spin = createSpinner(out);
      const started = Date.now();
      spin.start(text);
      active = spin;
      const stop = (fn) => {
        active = null;
        fn();
      };
      return {
        update: (t) => spin.setLabel(t),
        progress: (fraction, detail) =>
          spin.setSuffix(
            `${progressBar(fraction)} ${c.bold(
              `${Math.round((fraction || 0) * 100)}%`.padStart(4),
            )}${detail ? c.gray(`  ${detail}`) : ''}`,
          ),
        succeed: (t) =>
          stop(() =>
            spin.persist(
              c.green(sym.tick),
              `${t == null ? text : t}  ${c.gray(humanDuration(Date.now() - started))}`,
            ),
          ),
        fail: (t) => stop(() => spin.persist(c.red(sym.cross), t)),
        warn: (t) => stop(() => spin.persist(c.yellow(sym.warn), t)),
        stop: () => stop(() => spin.clear()),
      };
    },

    success({ title, duration, rows }) {
      if (silent) return;
      const time =
        duration == null ? '' : c.gray(`   ${sym.dot}   ${humanDuration(duration)}`);
      write(`\n  ${c.green(sym.tick)} ${c.bold(title)}${time}\n\n`);
      if (rows) rowsBlock(rows, write, 4);
      write('\n');
    },

    note(text) {
      if (!silent) write(`  ${c.gray(sym.arrow)}  ${c.gray(text)}\n`);
    },

    warn(text) {
      if (!silent) write(`  ${c.yellow(sym.warn)}  ${text}\n`);
    },

    error({ title, detail, hint, command }) {
      if (level === 'json') return;
      err.write(`\n  ${c.red(sym.cross)} ${c.bold(title)}\n`);
      if (detail) err.write(`\n${indent(detail)}\n`);
      if (hint) err.write(`\n${indent(c.gray(hint))}\n`);
      if (command) err.write(`\n    ${c.cyan(`$ ${command}`)}\n`);
      err.write('\n');
    },

    result(obj) {
      if (level === 'json') out.write(`${JSON.stringify(obj, null, 2)}\n`);
    },

    stopActive() {
      if (active) {
        active.clear();
        active = null;
      }
    },
  };

  return api;
}

module.exports = { createReporter };
