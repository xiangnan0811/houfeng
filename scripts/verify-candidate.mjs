#!/usr/bin/env node
import fs from 'node:fs';
import path from 'node:path';
import { createHash } from 'node:crypto';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { classifyPaths } from './ci-scope.mjs';
import { runGates } from './verify-ci.mjs';

const ALL = { go: true, postgres: true, web: true, browser: true, image: true };
const OID = /^(?:[a-fA-F0-9]{40}|[a-fA-F0-9]{64})$/;
const isZero = (oid) => /^0+$/.test(oid);

function parsePushInput(input) {
  if (typeof input !== 'string' || input.includes('\0')) throw new Error('Invalid pre-push input');
  const lines = input.split('\n');
  if (lines.at(-1) === '') lines.pop();
  const updates = lines.map((line) => {
    const fields = line.split(' ');
    if (fields.length !== 4 || fields.some((field) => !field || /\s/.test(field))) {
      throw new Error('Invalid pre-push input: expected local_ref local_oid remote_ref remote_oid');
    }
    const [localRef, localOid, remoteRef, remoteOid] = fields;
    if (!OID.test(localOid) || !OID.test(remoteOid) || localOid.length !== remoteOid.length) {
      throw new Error('Invalid pre-push input: expected full Git object IDs');
    }
    return { localRef, localOid, remoteRef, remoteOid };
  });
  // Check every ref before inspecting any candidate or starting a lane.
  for (const { remoteRef } of updates) {
    if (['refs/heads/main', 'refs/heads/master', 'main', 'master'].includes(remoteRef)) {
      throw new Error(`Push blocked: remote ${remoteRef} is protected. Direct pushes, deletes, and force pushes to remote main/master are not allowed; push a feature branch and open a pull request instead.`);
    }
  }
  return updates;
}

function gitContext(cwd, source) {
  // Keep relative GIT_INDEX_FILE and other Git-local inputs relative to the
  // original invocation directory. Never chdir or alter the caller's environment.
  const env = {
    ...source, GIT_OPTIONAL_LOCKS: '0',
    // Root-wide pathspecs must not become literal or case-folded via ambient
    // Git knobs. These private query settings never escape into the caller.
    GIT_LITERAL_PATHSPECS: '0', GIT_NOGLOB_PATHSPECS: '0',
    GIT_GLOB_PATHSPECS: '0', GIT_ICASE_PATHSPECS: '0',
  };
  const query = (args, allowed = [0]) => {
    const result = spawnSync('git', args, { cwd, env, maxBuffer: 64 * 1024 * 1024 });
    if (result.error || !allowed.includes(result.status)) {
      // Git's stderr/config can contain secrets; report only the operation.
      throw new Error(`Candidate Git query failed: git ${args[0]} (${result.error?.code ?? result.status ?? result.signal})`);
    }
    return result;
  };
  const text = (args) => query(args).stdout.toString('utf8').trimEnd();
  // --git-path may rebase a relative override against the worktree root;
  // actual Git index access instead interprets it from the invocation cwd.
  const indexPath = () => env.GIT_INDEX_FILE !== undefined
    ? path.resolve(cwd, env.GIT_INDEX_FILE)
    : path.resolve(cwd, text(['rev-parse', '--path-format=absolute', '--git-path', 'index']));
  return { query, text, indexPath };
}

function readState(git, mode) {
  const root = fs.realpathSync(git.text(['rev-parse', '--show-toplevel']));
  const indexPath = git.indexPath();
  const head = git.text(['rev-parse', '--verify', 'HEAD^{commit}']);
  const symbolic = git.query(['symbolic-ref', '--quiet', 'HEAD'], [0, 1]);
  if (symbolic.status !== 0) throw new Error('Detached HEAD is not a verifiable candidate; switch to a feature branch first');
  const branch = symbolic.stdout.toString('utf8').trimEnd();
  if (mode === 'staged' && ['refs/heads/main', 'refs/heads/master'].includes(branch)) {
    throw new Error('Commit blocked: local main/master is read-only; switch to a feature branch first');
  }
  const sparse = git.query(['config', '--bool', '--get', 'core.sparseCheckout'], [0, 1]);
  if (sparse.stdout.toString('utf8').trim() === 'true') {
    throw new Error('Sparse checkout is unsupported; restore a normal complete checkout first');
  }
  const stage = git.query(['ls-files', '--stage', '--full-name', '-z', '--', ':(top)']).stdout;
  for (const entry of stage.toString('latin1').split('\0').filter(Boolean)) {
    const header = entry.slice(0, entry.indexOf('\t')).split(' ');
    if (header[2] !== '0') throw new Error('Unmerged index: resolve all conflicts before verification');
    if (header[0] === '160000') throw new Error('Gitlinks are unsupported; use a normal complete checkout without submodules');
    if (!['100644', '100755'].includes(header[0])) {
      throw new Error('Unsupported tracked entry mode; restore a normal complete checkout');
    }
  }
  const flags = git.query(['ls-files', '-v', '--full-name', '-z', '--', ':(top)']).stdout.toString('latin1');
  if (flags.split('\0').some((entry) => entry && (entry[0] === 'S' || /[a-z]/.test(entry[0])))) {
    throw new Error('Hidden index entries (skip-worktree/assume-unchanged) are unsupported; restore normal index flags first');
  }
  if (git.query(['diff', '--quiet', '--no-ext-diff', '--no-textconv', '--no-relative', '--ignore-submodules=none', '--', ':(top)'], [0, 1]).status !== 0) {
    throw new Error('Unstaged tracked changes: stage the complete candidate or restore it yourself before verification');
  }
  if (git.query(['ls-files', '--others', '--exclude-standard', '--full-name', '-z', '--', ':(top)']).stdout.length) {
    throw new Error('Nonignored untracked files: stage or move them yourself before verification');
  }
  if (mode === 'push' && git.query(['diff', '--cached', '--quiet', '--no-ext-diff', '--no-textconv', '--no-relative', head, '--', ':(top)'], [0, 1]).status !== 0) {
    throw new Error('Push requires the index and working tree to match HEAD; commit the complete candidate first');
  }
  const digest = createHash('sha256').update(stage).update('\0').update(head).update('\0').update(branch).digest('hex');
  return { root, indexPath, head, branch, digest };
}

function baseline(git, head) {
  if (git.text(['rev-parse', '--is-shallow-repository']) === 'true') return undefined;
  const main = git.query(['rev-parse', '--verify', '--quiet', 'refs/remotes/origin/main^{commit}'], [0, 1]);
  if (main.status !== 0) return undefined;
  const bases = git.query(['merge-base', '--all', main.stdout.toString('utf8').trim(), head], [0, 1]);
  if (bases.status !== 0) return undefined;
  const values = bases.stdout.toString('utf8').trim().split('\n');
  return values.length === 1 && OID.test(values[0]) ? values[0] : undefined;
}

function selectGates(git, base, mode, oid) {
  let selection = ALL;
  if (base) {
    const endpoints = mode === 'staged' ? ['--cached', base] : [base, oid];
    const bytes = git.query(['diff', '--name-only', '-z', '--no-renames', '--no-ext-diff', '--no-textconv', '--no-relative', ...endpoints, '--', ':(top)']).stdout;
    if (bytes.length && bytes.at(-1) !== 0) throw new Error('Invalid NUL-delimited candidate paths from Git');
    try {
      const paths = new TextDecoder('utf-8', { fatal: true, ignoreBOM: true }).decode(bytes);
      selection = classifyPaths(paths ? paths.slice(0, -1).split('\0') : []);
    } catch (error) {
      if (!(error instanceof TypeError)) throw error;
      selection = ALL;
    }
  }
  const gates = ['docs'];
  if (selection.go) gates.push('go');
  if (selection.postgres) gates.push('postgres', 'business-postgres');
  if (selection.web) gates.push('web');
  if (selection.browser) gates.push('browser');
  if (selection.image) gates.push('image');
  return gates;
}

// Read-only seam for real-repository behavioral tests. Queries retain their
// original cwd and a private environment copy, including the effective index.
// Custom cwd/env belong to this inspection seam; callers using them must inject
// their executor into verifyCandidate. CLI preparation and runGates both retain
// the same original process cwd/environment.
export function prepareCandidate({ mode, input = '', cwd = process.cwd(), env = process.env } = {}) {
  if (!['staged', 'push'].includes(mode)) throw new Error('Use exactly --staged or --push');
  const updates = mode === 'push' ? parsePushInput(input) : [];
  const git = gitContext(cwd, env);
  for (const { remoteRef } of updates) {
    // Git may describe the source as an OID or revision expression, not a ref.
    // Its supplied localOid below, rather than that description, binds the push.
    if (git.query(['check-ref-format', remoteRef], [0, 1]).status !== 0) throw new Error('Invalid ref in pre-push input');
  }
  const live = updates.filter(({ localOid }) => !isZero(localOid));
  if (mode === 'push' && live.length === 0) {
    return { gates: [], assertUnchanged() {} };
  }
  // Resolve all candidate identifiers before any lane runs (and without chdir).
  const initial = readState(git, mode);
  for (const { localOid } of live) {
    const peeled = git.query(['rev-parse', '--verify', '--quiet', `${localOid}^{commit}`], [0, 1, 128]);
    if (peeled.status !== 0 || peeled.stdout.toString('utf8').trim() !== initial.head) {
      throw new Error('Push candidate must peel to current HEAD; switch to the target branch and push it separately (tree/blob tags and old or other commits are unsupported)');
    }
  }
  const base = baseline(git, initial.head);
  const gates = selectGates(git, base, mode, live[0]?.localOid);
  const assertUnchanged = () => {
    const current = readState(git, mode);
    if (current.digest !== initial.digest || current.root !== initial.root || current.indexPath !== initial.indexPath) {
      throw new Error('Candidate identity changed (index, HEAD, branch, or checkout); checks are not valid for this candidate');
    }
  };
  assertUnchanged();
  return { ...initial, baseline: base, gates, assertUnchanged };
}

// The injected executor is an import-only test seam, not a CLI/environment skip
// switch. Production always invokes the shared runner and never caches success.
export async function verifyCandidate(candidate, executeGates = runGates) {
  if (!candidate.gates.length) return;
  candidate.assertUnchanged();
  await executeGates(candidate.gates, {
    beforeGate: candidate.assertUnchanged,
    afterGate: candidate.assertUnchanged,
  });
  candidate.assertUnchanged();
}

function isDirectInvocation() {
  if (!process.argv[1]) return false;
  return fs.realpathSync(path.resolve(process.argv[1])) === fs.realpathSync(fileURLToPath(import.meta.url));
}

if (isDirectInvocation()) {
  try {
    const args = process.argv.slice(2);
    if (args.length !== 1 || !['--staged', '--push'].includes(args[0])) throw new Error('Use exactly --staged or --push');
    const mode = args[0].slice(2);
    const input = mode === 'push' ? fs.readFileSync(0, 'utf8') : '';
    await verifyCandidate(prepareCandidate({ mode, input }));
  } catch (error) {
    console.error(`[verify-candidate] ${error?.message ?? String(error)}`);
    process.exitCode = 1;
  }
}
