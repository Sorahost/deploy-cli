'use strict';

// 表示状態。cli 側から configure() で上書きする。
const state = {
  color:
    process.env.NO_COLOR == null &&
    process.env.TERM !== 'dumb' &&
    (process.env.FORCE_COLOR != null || Boolean(process.stdout.isTTY)),
  interactive: Boolean(process.stdout.isTTY) && process.env.CI == null,
};

function configure(opts = {}) {
  if (opts.color != null) state.color = opts.color;
  if (opts.interactive != null) state.interactive = opts.interactive;
}

const paint = (open, close) => (s) =>
  state.color ? `\x1b[${open}m${s}\x1b[${close}m` : String(s);

const c = {
  bold: paint(1, 22),
  dim: paint(2, 22),
  italic: paint(3, 23),
  underline: paint(4, 24),
  red: paint(31, 39),
  green: paint(32, 39),
  yellow: paint(33, 39),
  blue: paint(34, 39),
  magenta: paint(35, 39),
  cyan: paint(36, 39),
  gray: paint(90, 39),
};

const ascii = process.platform === 'win32' && process.env.WT_SESSION == null;
const sym = {
  tick: ascii ? '√' : '✓',
  cross: ascii ? '×' : '✗',
  warn: ascii ? '!' : '⚠',
  info: ascii ? 'i' : 'ℹ',
  arrow: '→',
  pointer: ascii ? '>' : '❯',
  dot: '·',
  diamond: ascii ? '*' : '◆',
};

function humanBytes(n) {
  if (!Number.isFinite(n)) return '-';
  if (n < 1000) return `${n} B`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i += 1;
  }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`;
}

function humanDuration(ms) {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const s = ms / 1000;
  if (s < 60) return `${s.toFixed(1)}s`;
  const m = Math.floor(s / 60);
  const r = Math.round(s % 60);
  return `${m}m${r ? ` ${r}s` : ''}`;
}

function relativeTime(value) {
  const then = new Date(value).getTime();
  if (Number.isNaN(then)) return null;
  const diff = Date.now() - then;
  if (diff < 60_000) return 'たった今';
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}分前`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}時間前`;
  if (diff < 2_592_000_000) return `${Math.floor(diff / 86_400_000)}日前`;
  return `${Math.floor(diff / 2_592_000_000)}か月前`;
}

function progressBar(fraction, width = 20) {
  const f = Math.max(0, Math.min(1, fraction || 0));
  const filled = Math.round(f * width);
  return c.cyan('█'.repeat(filled)) + c.gray('░'.repeat(width - filled));
}

const SPINNER = ['⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'];

function createSpinner(stream = process.stdout) {
  let frame = 0;
  let timer = null;
  let label = '';
  let suffix = '';
  const render = () =>
    `\r  ${c.cyan(SPINNER[frame])} ${label}${suffix ? `  ${suffix}` : ''}\x1b[K`;

  return {
    start(text) {
      label = text;
      if (!state.interactive) return this;
      stream.write('\x1b[?25l');
      stream.write(render());
      timer = setInterval(() => {
        frame = (frame + 1) % SPINNER.length;
        stream.write(render());
      }, 80);
      if (timer.unref) timer.unref();
      return this;
    },
    setLabel(text) {
      label = text;
      if (state.interactive && timer) stream.write(render());
      return this;
    },
    setSuffix(text) {
      suffix = text;
      if (state.interactive && timer) stream.write(render());
      return this;
    },
    persist(symbol, text) {
      if (timer) {
        clearInterval(timer);
        timer = null;
      }
      const out = `  ${symbol} ${text == null ? label : text}`;
      if (state.interactive) stream.write(`\r${out}\x1b[K\n\x1b[?25h`);
      else stream.write(`${out}\n`);
    },
    clear() {
      if (timer) {
        clearInterval(timer);
        timer = null;
      }
      if (state.interactive) stream.write('\r\x1b[K\x1b[?25h');
    },
  };
}

function restoreCursor() {
  if (state.interactive) {
    try {
      process.stdout.write('\x1b[?25h');
    } catch {
      /* noop */
    }
  }
}

module.exports = {
  state,
  configure,
  c,
  sym,
  humanBytes,
  humanDuration,
  relativeTime,
  progressBar,
  createSpinner,
  restoreCursor,
};
