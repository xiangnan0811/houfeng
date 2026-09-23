#!/usr/bin/env node
// Repository documentation integrity gate. No packages or network access required.
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync } from 'node:child_process';

const slash = (value) => value.split(path.sep).join('/');
const ignored = new Set(['.git', '.worktree', '.worktrees', 'node_modules', 'dist', 'coverage', 'playwright-report', 'test-results']);
function filesIn(root, dir = root) {
  return fs.readdirSync(dir, { withFileTypes: true }).flatMap((entry) => {
    if (ignored.has(entry.name) || entry.isSymbolicLink()) return [];
    const target = path.join(dir, entry.name);
    return entry.isDirectory() ? filesIn(root, target) : [slash(path.relative(root, target))];
  });
}
function repositoryFiles(root) {
  try {
    return execFileSync('git', ['ls-files', '-z', '--cached', '--others', '--exclude-standard'], { cwd: root, encoding: 'utf8', stdio: ['ignore', 'pipe', 'ignore'] })
      .split('\0').filter((name) => name && fs.existsSync(path.join(root, name)) && fs.lstatSync(path.join(root, name)).isFile() && !name.split('/').some((part) => ignored.has(part)));
  } catch { return filesIn(root); }
}

// Keep newlines intact so diagnostics continue to refer to the original source.
export function withoutCode(text) {
  let fence;
  const listIndents = [];
  return text.split('\n').map((line) => {
    const expanded = line.replace(/^\t+/, (tabs) => ' '.repeat(tabs.length * 4));
    const indent = expanded.match(/^ */)[0].length;
    if (line.trim() && !fence) {
      while (listIndents.length && indent < listIndents.at(-1)) listIndents.pop();
    }
    const containerIndent = fence?.indent ?? listIndents.at(-1) ?? 0;
    let content = expanded.slice(Math.min(indent, containerIndent));
    // List indentation belongs to a container; only an additional four spaces
    // introduce an indented code block. Keep nested list links inspectable.
    if (!fence) {
      const marker = content.match(/^ {0,3}(?:[-+*]|\d+[.)])([ \t]+)(?=\S)/);
      if (marker) {
        listIndents.push(Math.min(indent, containerIndent) + marker[0].length);
        content = content.slice(marker[0].length);
      }
    }
    const match = content.match(/^ {0,3}(`{3,}|~{3,})/);
    if (fence) {
      if (match && match[1][0] === fence.marker[0] && match[1].length >= fence.marker.length && /^ {0,3}(?:`+|~+)\s*$/.test(content)) fence = undefined;
      return ' '.repeat(line.length);
    }
    if (match) { fence = { marker: match[1], indent: listIndents.at(-1) ?? 0 }; return ' '.repeat(line.length); }
    if (/^(?: {4}|\t)/.test(content)) return ' '.repeat(line.length);
    return line;
  }).join('\n').replace(/(`+)([^`]|(?!\1)`)*?\1/g, (match) => match.replace(/[^\n]/g, ' '));
}

function headingText(text) {
  return text.replace(/!?(?:\[([^\]]*)\])\([^)]*\)/g, '$1').replace(/<[^>]*>/g, '')
    .replace(/&amp;/g, '&').replace(/&lt;/g, '<').replace(/&gt;/g, '>').replace(/&quot;/g, '"')
    .replace(/&#(\d+);/g, (_, value) => String.fromCodePoint(Number(value)))
    .replace(/(?<!\w)_([^_\n]+)_(?!\w)/g, '$1').replace(/[*~`]/g, '').trim().toLowerCase().replace(/[^\p{L}\p{M}\p{N}\p{Pc}\-\s]/gu, '').replace(/\s/g, '-');
}
export function anchors(text) {
  // Inline code contributes text to heading slugs; only fenced code is removed here.
  const clean = withoutCode(text.replace(/`([^`\n]+)`/g, '$1'));
  const result = new Set();
  const lines = clean.split('\n');
  for (let i = 0; i < lines.length; i++) {
    const atx = lines[i].match(/^ {0,3}#{1,6}\s+(.+?)\s*#*\s*$/);
    const setext = i + 1 < lines.length && /^ {0,3}(?:=+|-+)\s*$/.test(lines[i + 1]) && lines[i].trim();
    if (!atx && !setext) continue;
    const base = headingText(atx ? atx[1] : lines[i]);
    let slug = base;
    for (let suffix = 1; result.has(slug); suffix++) slug = `${base}-${suffix}`;
    result.add(slug);
    if (setext && !atx) i++;
  }
  for (const match of clean.matchAll(/<(?:a|h[1-6])\b[^>]*\b(?:id|name)=["']([^"']+)["']/gi)) result.add(match[1]);
  return result;
}

const referenceID = (value) => value.trim().replace(/\s+/g, ' ').toLowerCase();
export function links(text) {
  const clean = withoutCode(text).replace(/<!--[^]*?-->/g, (match) => match.replace(/[^\n]/g, ' '));
  const definitions = new Map();
  const result = [];
  for (const match of clean.matchAll(/^ {0,3}\[([^\]]+)\]:\s*(?:<([^>]+)>|(\S+))/gm)) definitions.set(referenceID(match[1]), match[2] ?? match[3]);
  const body = clean.replace(/^ {0,3}\[[^\]]+\]:[^\n]*/gm, '');
  const pattern = /!?\[([^\]]*(?:\[[^\]]*\][^\]]*)*)\]/g;
  let consumed = 0;
  for (const match of body.matchAll(pattern)) {
    if (match.index < consumed) continue;
    if (/\n[ \t]*\n/.test(match[1])) continue;
    const end = match.index + match[0].length;
    const rest = body.slice(end);
    let target;
    if (rest.startsWith('(')) {
      let depth = 1;
      let stop = 1;
      for (; stop < rest.length && depth; stop++) {
        if (rest[stop] === '\\') { stop++; continue; }
        if (rest[stop] === '(') depth++;
        if (rest[stop] === ')') depth--;
      }
      if (!depth) {
        target = rest.slice(1, stop - 1).trim().match(/^(?:<([^>]+)>|(\S+?))(?:\s+["'][^]*["'])?$/)?.slice(1).find(Boolean);
        consumed = end + stop;
      }
    } else if (rest.startsWith('[')) {
      const id = rest.match(/^\[([^\]]*)\]/)?.[1];
      if (id !== undefined) {
        consumed = end + id.length + 2;
        target = definitions.get(referenceID(id || match[1]));
        if (!target) result.push({ missingReference: id || match[1] });
      }
    } else target = definitions.get(referenceID(match[1]));
    if (target) result.push({ target });
  }
  return result;
}

export function auditRepository(root) {
  const errors = [];
  const files = repositoryFiles(root);
  const markdown = files.filter((name) => /\.md$/i.test(name));
  const contents = new Map(markdown.map((name) => [name, fs.readFileSync(path.join(root, name), 'utf8')]));
  const graph = new Map(markdown.map((name) => [name, new Set()]));
  function checkTarget(source, raw) {
    if (/^(?:[a-z][a-z\d+.-]*:|\/\/)/i.test(raw)) return;
    let decoded;
    try { decoded = decodeURIComponent(raw.replace(/\\([\\() ])/g, '$1')); } catch { errors.push(`${source}: malformed URL ${raw}`); return; }
    const [pathname, fragment] = decoded.split('#', 2);
    const targetPath = pathname.split('?', 1)[0];
    let absolute = targetPath ? path.resolve(targetPath.startsWith('/') ? root : path.dirname(path.join(root, source)), targetPath.replace(/^\//, '')) : path.join(root, source);
    if (!absolute.startsWith(root + path.sep) && absolute !== root) { errors.push(`${source}: link escapes repository: ${raw}`); return; }
    if (!fs.existsSync(absolute)) { errors.push(`${source}: missing target ${raw}`); return; }
    if (fs.statSync(absolute).isDirectory()) {
      absolute = path.join(absolute, 'README.md');
      if (slash(path.relative(root, absolute)).startsWith('docs/') && !fs.existsSync(absolute)) {
        errors.push(`${source}: documentation directory lacks README.md: ${raw}`);
        return;
      }
    }
    const target = slash(path.relative(root, absolute));
    if (fs.existsSync(absolute)) graph.get(source)?.add(target);
    if (fragment && /\.md$/i.test(target)) {
      const text = contents.get(target) ?? (fs.existsSync(absolute) ? fs.readFileSync(absolute, 'utf8') : '');
      if (!anchors(text).has(fragment)) errors.push(`${source}: missing anchor ${raw}`);
    }
  }
  for (const [name, text] of contents) {
    for (const link of links(text)) {
      if (link.missingReference) errors.push(`${name}: undefined reference [${link.missingReference}]`);
      else checkTarget(name, link.target);
    }
    // Platform imports are line-oriented, not email addresses or inline mentions.
    for (const match of withoutCode(text).matchAll(/^\s*@((?:\.?\.?\/)?[^\s]+\.md)\s*$/gm)) checkTarget(name, match[1]);
  }
  // Scan source as well as prose: inline code and quoted runtime paths remain dependencies.
  // The checker and its negative fixtures necessarily contain the prohibited spellings.
  const oldPath = /(?:^|[^\w./-])\/?(?:spec\/|docs\/design\/(?:current|v1-baseline|v2-houfeng)(?:\/|\b)|docs\/design\/proposals\/monitoring-instance-detail-redesign\.md|docs\/operations\/ui-preview-and-browser-sanity\.md)/gm;
  for (const name of files) {
    if (name.startsWith('scripts/verify-docs') || /(?:package-lock\.json|go\.sum)$/.test(name)) continue;
    const buffer = fs.readFileSync(path.join(root, name));
    if (buffer.includes(0)) continue;
    const text = buffer.toString('utf8');
    // A Markdown destination is relative to its document, unlike repository-path
    // prose/source strings. Mask only valid relative spec destinations, retaining
    // line numbers and keeping root-absolute /spec references visible to the scan.
    const prose = /\.md$/i.test(name) ? withoutCode(text) : '';
    const scanned = /\.md$/i.test(name) ? text.replace(
      /(?:\]\(\s*<?|^ {0,3}\[[^\]\n]+\]:\s*<?)(spec\/[^\s)>"']+)/gm,
      (whole, target, offset) => {
        if (prose.slice(offset, offset + whole.length) !== whole) return whole;
        const absolute = path.resolve(path.dirname(path.join(root, name)), target.split(/[?#]/, 1)[0]);
        if (!slash(path.relative(root, absolute)).startsWith('docs/spec/') || !fs.existsSync(absolute)) return whole;
        return whole.replace(target, ' '.repeat(target.length));
      },
    ) : text;
    for (const match of scanned.matchAll(oldPath)) errors.push(`${name}:${scanned.slice(0, match.index).split('\n').length}: removed documentation path ${match[0].trim()}`);
    if (/\.md$/i.test(name)) {
      for (const match of text.matchAll(/(?:^|[^\w./-])((?:\.\.?\/)*(?:proposals\/)?monitoring-instance-detail-redesign\.md)(?=$|[^\w.-])/gm)) {
        const target = slash(path.relative(root, path.resolve(path.dirname(path.join(root, name)), match[1])));
        if (target === 'docs/design/proposals/monitoring-instance-detail-redesign.md') errors.push(`${name}:${text.slice(0, match.index).split('\n').length}: removed documentation path ${match[1]}`);
      }
    }
  }
  for (const old of ['spec', 'docs/design/current', 'docs/design/v1-baseline', 'docs/design/v2-houfeng']) {
    if (fs.existsSync(path.join(root, old))) errors.push(`${old}: removed documentation directory still exists`);
  }
  const completedProposal = 'docs/design/proposals/monitoring-instance-detail-redesign.md';
  if (fs.existsSync(path.join(root, completedProposal))) errors.push(`${completedProposal}: completed proposal still exists`);
  const docs = files.filter((name) => name.startsWith('docs/'));
  const docsMarkdown = markdown.filter((name) => name.startsWith('docs/'));
  const directories = new Set(docsMarkdown.map((name) => path.posix.dirname(name)));
  for (const directory of directories) {
    const index = `${directory}/README.md`;
    if (!contents.has(index)) { errors.push(`${directory}: missing README.md index`); continue; }
    // Resource-only subdirectories are indexed by their nearest maintained parent.
    const expected = docs.filter((name) => {
      if (name === index) return false;
      let owner = path.posix.dirname(name);
      while (!directories.has(owner) && owner.startsWith('docs/')) owner = path.posix.dirname(owner);
      return owner === directory;
    });
    for (const child of directories) if (path.posix.dirname(child) === directory) expected.push(`${child}/README.md`);
    for (const name of new Set(expected)) if (!graph.get(index)?.has(name)) errors.push(`${index}: index does not link ${name}`);
  }
  const reachable = new Set();
  function visit(name) { if (reachable.has(name)) return; reachable.add(name); for (const next of graph.get(name) ?? []) visit(next); }
  visit('docs/README.md');
  for (const name of docsMarkdown) if (!reachable.has(name)) errors.push(`${name}: unreachable from docs/README.md`);
  if (!contents.has('docs/README.md')) errors.push('docs/README.md: missing documentation entrypoint');
  return [...new Set(errors)].sort();
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const root = path.resolve(process.argv[2] ?? path.join(path.dirname(fileURLToPath(import.meta.url)), '..'));
  const errors = auditRepository(root);
  if (errors.length) { console.error(errors.join('\n')); console.error(`Documentation verification failed: ${errors.length} problem(s).`); process.exitCode = 1; }
  else console.log('Documentation links, indexes, imports and retired paths verified.');
}
