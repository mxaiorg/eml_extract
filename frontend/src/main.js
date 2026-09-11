import { EventsOn } from '../wailsjs/runtime/runtime.js';
import {
  GetStatus, ChooseOutputDir, ChooseInputFiles, StartExtract, OpenPath, RevealInFinder,
} from '../wailsjs/go/main/App.js';

const $ = (id) => document.getElementById(id);
const el = {
  status: $('status'),
  outdir: $('outdir'),
  chooseOut: $('choose-out'),
  openOut: $('open-out'),
  includeNameless: $('include-nameless'),
  dropzone: $('dropzone'),
  browse: $('browse'),
  progress: $('progress'),
  barFill: $('bar-fill'),
  progressText: $('progress-text'),
  summary: $('summary'),
  summaryText: $('summary-text'),
  revealAll: $('reveal-all'),
  clear: $('clear'),
  results: $('results'),
};

const state = {
  ripmimeFound: false,
  busy: false,
  total: 0,
  done: 0,
  lastOutputDir: '',
};

const STORE_KEY = 'emlxtract.outdir';

// ---------- startup ----------

async function init() {
  const s = await GetStatus();
  state.ripmimeFound = s.ripmimeFound;
  if (s.ripmimeFound) {
    el.status.textContent = `ripmime ✓ ${s.ripmimeVersion || ''}`.trim();
    el.status.className = 'pill pill-ok';
    el.status.title = s.ripmimePath;
  } else {
    el.status.textContent = 'ripmime not found — brew install ripmime';
    el.status.className = 'pill pill-err';
    el.status.title = '.eml extraction needs ripmime on PATH. .msg files still work.';
  }

  let saved = '';
  try { saved = localStorage.getItem(STORE_KEY) || ''; } catch (_) {}
  el.outdir.value = saved || s.defaultOutputDir;
}

function outputDir() {
  return el.outdir.value.trim();
}

el.outdir.addEventListener('change', () => {
  try { localStorage.setItem(STORE_KEY, outputDir()); } catch (_) {}
});

el.chooseOut.addEventListener('click', async () => {
  try {
    const dir = await ChooseOutputDir(outputDir());
    if (dir) {
      el.outdir.value = dir;
      el.outdir.dispatchEvent(new Event('change'));
    }
  } catch (e) {
    console.error(e);
  }
});

el.openOut.addEventListener('click', () => {
  const d = outputDir();
  if (d) OpenPath(d).catch(() => {});
});

// ---------- input ----------

el.browse.addEventListener('click', async (ev) => {
  ev.preventDefault();
  ev.stopPropagation();
  try {
    const paths = await ChooseInputFiles();
    if (paths && paths.length) startExtract(paths);
  } catch (e) {
    console.error(e);
  }
});

el.dropzone.addEventListener('click', () => el.browse.click());
el.dropzone.addEventListener('keydown', (ev) => {
  if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); el.browse.click(); }
});

// Visual hover state. The actual file paths arrive via the Wails native
// file-drop event (files:dropped) — WebKit never exposes paths to JS.
let dragDepth = 0;
for (const evName of ['dragenter', 'dragover']) {
  document.addEventListener(evName, (ev) => {
    ev.preventDefault();
    if (evName === 'dragenter') dragDepth++;
    el.dropzone.classList.add('hover');
  });
}
document.addEventListener('dragleave', () => {
  dragDepth = Math.max(0, dragDepth - 1);
  if (dragDepth === 0) el.dropzone.classList.remove('hover');
});
document.addEventListener('drop', (ev) => {
  ev.preventDefault();
  dragDepth = 0;
  el.dropzone.classList.remove('hover');
});

EventsOn('files:dropped', (paths) => {
  dragDepth = 0;
  el.dropzone.classList.remove('hover');
  if (Array.isArray(paths) && paths.length) startExtract(paths);
});

// ---------- extraction ----------

async function startExtract(paths) {
  if (state.busy) return;
  state.busy = true;
  state.total = 0;
  state.done = 0;
  el.dropzone.classList.add('busy');
  el.summary.classList.add('hidden');
  el.progress.classList.remove('hidden');
  el.barFill.style.width = '0%';
  el.progressText.textContent = 'Scanning…';
  try {
    await StartExtract({
      paths,
      outputDir: outputDir(),
      includeNameless: el.includeNameless.checked,
    });
  } catch (e) {
    finish({ ok: 0, failed: 0, attachments: 0, error: String(e) });
  }
}

EventsOn('extract:begin', ({ total }) => {
  state.total = total;
  state.done = 0;
  el.progressText.textContent = total === 0
    ? 'No .eml or .msg files found in what you dropped.'
    : `Extracting 0 / ${total}…`;
});

EventsOn('extract:file', (r) => {
  state.done++;
  const pct = state.total ? Math.round((state.done / state.total) * 100) : 100;
  el.barFill.style.width = `${pct}%`;
  el.progressText.textContent = `Extracting ${state.done} / ${state.total}…`;
  el.results.prepend(renderCard(r));
});

EventsOn('extract:done', finish);

function finish(d) {
  state.busy = false;
  el.dropzone.classList.remove('busy');
  el.progress.classList.add('hidden');
  el.summary.classList.remove('hidden');
  state.lastOutputDir = d.outputDir || outputDir();

  if (d.error) {
    el.summaryText.textContent = `Failed: ${d.error}`;
    return;
  }
  const parts = [];
  parts.push(`${d.attachments} attachment${d.attachments === 1 ? '' : 's'}`);
  parts.push(`from ${d.ok} file${d.ok === 1 ? '' : 's'}`);
  if (d.failed) parts.push(`· ${d.failed} failed`);
  el.summaryText.textContent = parts.join(' ');
}

el.revealAll.addEventListener('click', () => {
  const d = state.lastOutputDir || outputDir();
  if (d) OpenPath(d).catch(() => {});
});

el.clear.addEventListener('click', () => {
  el.results.innerHTML = '';
  el.summary.classList.add('hidden');
});

// ---------- rendering ----------

function renderCard(r) {
  const card = document.createElement('div');
  card.className = 'card';
  if (r.error) card.classList.add('err');
  else if (r.warnings && r.warnings.length) card.classList.add('warn');

  const head = document.createElement('div');
  head.className = 'card-head';

  const kind = document.createElement('span');
  kind.className = 'kind';
  kind.textContent = r.kind || '?';

  const name = document.createElement('span');
  name.className = 'name';
  name.textContent = basename(r.source);
  name.title = r.source;

  const count = document.createElement('span');
  count.className = 'count';
  const n = (r.attachments || []).length;
  count.textContent = r.error ? 'error' : `${n} attachment${n === 1 ? '' : 's'}`;

  head.append(kind, name, count);

  if (r.outputDir) {
    const reveal = button('Reveal', 'btn btn-sm', () => RevealInFinder(r.outputDir).catch(() => {}));
    reveal.title = r.outputDir;
    head.append(reveal);
  }
  card.append(head);

  if (r.error) {
    const note = document.createElement('div');
    note.className = 'note err';
    note.textContent = r.error;
    card.append(note);
  }
  for (const w of r.warnings || []) {
    const note = document.createElement('div');
    note.className = 'note warn';
    note.textContent = w;
    card.append(note);
  }

  if (n > 0) {
    const ul = document.createElement('ul');
    ul.className = 'att-list';
    for (const a of r.attachments) {
      const li = document.createElement('li');
      li.className = 'att';
      li.title = a.path;
      const nm = document.createElement('span');
      nm.className = 'att-name';
      nm.textContent = a.name;
      const sz = document.createElement('span');
      sz.className = 'att-size';
      sz.textContent = fmtSize(a.size);
      const open = button('Open', 'btn btn-sm btn-ghost', () => OpenPath(a.path).catch(() => {}));
      li.append(nm, sz, open);
      li.addEventListener('dblclick', () => OpenPath(a.path).catch(() => {}));
      ul.append(li);
    }
    card.append(ul);
  } else if (!r.error) {
    const e = document.createElement('div');
    e.className = 'empty';
    e.textContent = 'No attachments.';
    card.append(e);
  }

  if (r.stderr) {
    const det = document.createElement('details');
    const sum = document.createElement('summary');
    sum.textContent = 'ripmime output';
    const pre = document.createElement('pre');
    pre.className = 'stderr';
    pre.textContent = r.stderr;
    det.append(sum, pre);
    card.append(det);
  }
  return card;
}

function button(label, cls, onClick) {
  const b = document.createElement('button');
  b.className = cls;
  b.textContent = label;
  b.addEventListener('click', (ev) => { ev.stopPropagation(); onClick(); });
  return b;
}

function basename(p) {
  return (p || '').split('/').pop();
}

function fmtSize(n) {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

init();
