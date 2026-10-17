#!/usr/bin/env node
import fs from 'node:fs';
import path from 'node:path';
import { execFileSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const gateNames = ['go', 'postgres', 'web', 'browser', 'image'];
const selection = (enabled = []) => Object.fromEntries(gateNames.map((gate) => [gate, enabled.includes(gate)]));
const allGates = () => selection(gateNames);

// Unknown inputs stay conservative. Keep this table aligned with cross-layer consumers.
export function classifyPaths(paths) {
  if (!Array.isArray(paths)) throw new TypeError('paths must be an array of Git-root-relative paths');
  const result = selection();
  for (const name of paths) {
    let enabled;
    if (typeof name !== 'string' || !name || name.includes('\0') || name.split('/').some((part) => !part || part === '.' || part === '..')) {
      return allGates();
    }
    if (name === 'CHANGELOG.md' || name === '.release-please-manifest.json') {
      enabled = [];
    } else if (name === 'README.md' || name.startsWith('docs/deploy/') || [
      'docs/spec/contracts/platform/production-compose.md',
      'docs/spec/contracts/platform/compose-authority.md',
    ].includes(name)) {
      enabled = gateNames;
    } else if (name.startsWith('docs/spec/web/') || name === 'docs/development/ui-preview-and-browser-sanity.md') {
      enabled = ['web', 'browser'];
    } else if (name.startsWith('docs/')) {
      enabled = [];
    } else if (name.startsWith('web/') || name === '.node-version') {
      enabled = ['go', 'web', 'browser', 'image'];
    } else {
      // Backend, automation, build inputs and every unlisted path require all lanes.
      enabled = gateNames;
    }
    for (const gate of enabled) result[gate] = true;
  }
  return result;
}

function parseArguments(args) {
  const options = {};
  for (let index = 0; index < args.length; index++) {
    const flag = args[index];
    if (!['--base', '--head', '--staged', '--all', '--github-output'].includes(flag)) throw new Error(`Unknown argument: ${flag}`);
    const key = flag.slice(2);
    if (Object.hasOwn(options, key)) throw new Error(`Duplicate argument: ${flag}`);
    if (flag === '--all' || flag === '--staged') {
      options[key] = true;
    } else {
      const value = args[++index];
      if (!value || value.startsWith('--') || value.includes('\0')) throw new Error(`Missing or invalid value for ${flag}`);
      options[key] = value;
    }
  }
  if (options.all) {
    if (options.base || options.head || options.staged) throw new Error('--all cannot be combined with --base, --head or --staged');
  } else if (!options.base || Boolean(options.head) === Boolean(options.staged)) {
    throw new Error('Use --all, --base <oid> --head <oid>, or --base <oid> --staged');
  }
  for (const key of ['base', 'head']) {
    if (options[key] && (!/^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$/.test(options[key]) || /^0+$/.test(options[key]))) {
      throw new Error(`--${key} must be a nonzero full Git object ID; use --all when no reliable baseline exists`);
    }
  }
  return options;
}

function git(args) {
  return execFileSync('git', args, { stdio: ['ignore', 'pipe', 'pipe'], maxBuffer: 64 * 1024 * 1024 });
}

function tree(oid) {
  return git(['rev-parse', '--verify', `${oid}^{tree}`]).toString('utf8').trim();
}

function classifyDiff(options) {
  const base = tree(options.base);
  const endpoints = options.staged ? ['--cached', base] : [base, tree(options.head)];
  const bytes = git(['diff', '--name-only', '-z', '--no-renames', '--no-ext-diff', '--no-textconv', '--no-relative', ...endpoints, '--']);
  if (bytes.length === 0) return classifyPaths([]);
  if (bytes.at(-1) !== 0) throw new Error('Git returned an invalid NUL-delimited path list');
  // Fatal decoding avoids treating lossy replacement characters as harmless docs paths.
  let decoded;
  try {
    decoded = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(bytes);
  } catch {
    return allGates();
  }
  return classifyPaths(decoded.slice(0, -1).split('\0'));
}

function main(args) {
  const options = parseArguments(args);
  const result = options.all ? allGates() : classifyDiff(options);
  if (options['github-output']) {
    fs.appendFileSync(options['github-output'], gateNames.map((gate) => `${gate}=${result[gate]}\n`).join(''));
  }
  console.log(JSON.stringify(result));
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    main(process.argv.slice(2));
  } catch (error) {
    console.error(`ci-scope: ${error.message}`);
    process.exitCode = 1;
  }
}
