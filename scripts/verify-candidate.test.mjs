import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync, spawnSync } from 'node:child_process';
import { prepareCandidate, verifyCandidate } from './verify-candidate.mjs';

const checkout = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const scratch = path.join(checkout, 'tmp/agent');
const script = path.join(checkout, 'scripts/verify-candidate.mjs');
const full = ['docs', 'go', 'postgres', 'business-postgres', 'web', 'browser', 'image'];
const web = ['docs', 'go', 'web', 'browser', 'image'];
const zero = '0'.repeat(40);

function repository(t, files = {}) {
  fs.mkdirSync(scratch, { recursive: true });
  const area = fs.mkdtempSync(path.join(scratch, 'candidate-'));
  const root = path.join(area, 'repo');
  fs.mkdirSync(root);
  t.after(() => fs.rmSync(area, { recursive: true, force: true }));
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('GIT_')));
  Object.assign(env, {
    GIT_CONFIG_NOSYSTEM: '1', GIT_CONFIG_GLOBAL: '/dev/null', GIT_TERMINAL_PROMPT: '0',
    GIT_AUTHOR_NAME: 'Candidate test', GIT_AUTHOR_EMAIL: 'candidate@example.invalid',
    GIT_COMMITTER_NAME: 'Candidate test', GIT_COMMITTER_EMAIL: 'candidate@example.invalid',
  });
  const git = (args, options = {}) => execFileSync('git', args, {
    cwd: root, env, encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'], ...options,
  }).trim();
  const put = (name, content = 'changed\n') => {
    const target = path.join(root, name);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, content);
  };
  const commit = () => {
    git(['add', '--all']);
    git(['commit', '-m', 'fixture']);
    return git(['rev-parse', 'HEAD']);
  };
  git(['init', '--initial-branch=feature']);
  git(['config', 'core.hooksPath', path.join(area, 'unused-hooks')]);
  for (const [name, content] of Object.entries({
    '.gitignore': '/tmp/\n/ignored-index\n/ignored-cache/\n',
    'CHANGELOG.md': 'initial\n', 'tracked.txt': 'initial\n', ...files,
  })) put(name, content);
  const base = commit();
  git(['update-ref', 'refs/remotes/origin/main', base]);
  const prepare = (mode = 'staged', options = {}) => prepareCandidate({ mode, cwd: root, env, ...options });
  const cli = (args, options = {}) => spawnSync(process.execPath, [script, ...args], {
    cwd: root, env, encoding: 'utf8', ...options,
  });
  const update = (oid = git(['rev-parse', 'HEAD']), remoteRef = 'refs/heads/feature', localRef = 'refs/heads/feature') =>
    `${localRef} ${oid} ${remoteRef} ${zero}\n`;
  const snapshot = (index = path.join(root, '.git/index')) => ({
    index: fs.readFileSync(index),
    files: ['CHANGELOG.md', 'tracked.txt'].map((name) => fs.readFileSync(path.join(root, name))),
  });
  return { root, area, env, git, put, commit, prepare, cli, update, snapshot, base };
}

async function observe(candidate, during = () => {}) {
  const visited = [];
  await verifyCandidate(candidate, async (gates, { beforeGate, afterGate }) => {
    for (const gate of gates) {
      await beforeGate(gate);
      visited.push(gate);
      await during(gate);
      await afterGate(gate);
    }
  });
  return visited;
}

function rejectUnchanged(repo, pattern, options = {}) {
  const before = repo.snapshot();
  assert.throws(() => repo.prepare('staged', options), pattern);
  assert.deepEqual(repo.snapshot(), before, 'rejected candidate must preserve source index and file bytes');
}

test('metadata and no-change staged candidates run docs each time without caching', async (t) => {
  const repo = repository(t);
  assert.deepEqual(await observe(repo.prepare()), ['docs']);
  repo.put('CHANGELOG.md', 'metadata\n');
  repo.git(['add', 'CHANGELOG.md']);
  const before = repo.snapshot();
  const candidate = repo.prepare();
  assert.equal(candidate.head, repo.base);
  assert.equal(candidate.branch, 'refs/heads/feature');
  assert.equal(candidate.baseline, repo.base);
  assert.equal(candidate.indexPath, path.join(repo.root, '.git/index'));
  assert.deepEqual(await observe(candidate), ['docs']);
  assert.deepEqual(await observe(candidate), ['docs']);
  assert.deepEqual(repo.snapshot(), before);
});

test('staged classification includes all earlier branch changes since merge-base', async (t) => {
  const repo = repository(t);
  repo.put('db/schema.sql', 'select 1;\n');
  repo.commit();
  repo.put('CHANGELOG.md', 'metadata\n');
  repo.git(['add', 'CHANGELOG.md']);
  assert.deepEqual(await observe(repo.prepare()), full);
});

test('Web staged paths select Go, Web, browser, image but no PostgreSQL', async (t) => {
  const repo = repository(t);
  repo.put('web/src/with space\nand newline.ts', 'export {};\n');
  repo.git(['add', 'web']);
  assert.deepEqual(await observe(repo.prepare()), web);
});

test('deleted and renamed backend paths retain their old heavy classification', async (t) => {
  const repo = repository(t, { 'db/schema.sql': 'select 1;\n' });
  fs.mkdirSync(path.join(repo.root, 'docs'));
  repo.git(['mv', 'db/schema.sql', 'docs/schema.md']);
  assert.deepEqual(await observe(repo.prepare()), full);
});

test('undecodable staged paths conservatively select all lanes', (t) => {
  const repo = repository(t);
  fs.mkdirSync(path.join(repo.root, 'docs'));
  const target = Buffer.concat([Buffer.from(`${repo.root}/docs/`), Buffer.from([0xff]), Buffer.from('.md')]);
  fs.writeFileSync(target, 'text\n');
  repo.git(['add', '--all']);
  assert.deepEqual(repo.prepare().gates, full);
});

test('staged good file plus unstaged tracked changes fails without mutation', (t) => {
  const repo = repository(t);
  repo.put('CHANGELOG.md', 'metadata\n');
  repo.git(['add', 'CHANGELOG.md']);
  repo.put('tracked.txt', 'unstaged\n');
  rejectUnchanged(repo, /Unstaged tracked/);
  const result = repo.cli(['--staged']);
  assert.equal(result.status, 1);
  assert.match(result.stderr, /Unstaged tracked/);
});

test('nonignored untracked files fail while ignored cache files are not candidates', async (t) => {
  const repo = repository(t);
  repo.put('ignored-cache/entry', 'cache\n');
  assert.deepEqual(await observe(repo.prepare()), ['docs']);
  repo.put('untracked name\n.txt', 'private\n');
  rejectUnchanged(repo, /Nonignored untracked/);
  assert.equal(fs.readFileSync(path.join(repo.root, 'untracked name\n.txt'), 'utf8'), 'private\n');
});

test('unmerged index is rejected without editing stages or files', (t) => {
  const repo = repository(t);
  const blob = repo.git(['rev-parse', 'HEAD:CHANGELOG.md']);
  repo.git(['update-index', '--index-info'], {
    input: `0 ${zero}\tCHANGELOG.md\n100644 ${blob} 1\tCHANGELOG.md\n100644 ${blob} 2\tCHANGELOG.md\n100644 ${blob} 3\tCHANGELOG.md\n`,
  });
  rejectUnchanged(repo, /Unmerged index/);
});

for (const flag of ['--skip-worktree', '--assume-unchanged']) {
  test(`hidden index flag ${flag} fails even when file content is unchanged`, (t) => {
    const repo = repository(t);
    repo.git(['update-index', flag, 'tracked.txt']);
    rejectUnchanged(repo, /Hidden index entries/);
  });
}

test('sparse checkout, gitlinks and detached HEAD are rejected before any lane', (t) => {
  const sparse = repository(t);
  sparse.git(['config', 'core.sparseCheckout', 'true']);
  rejectUnchanged(sparse, /Sparse checkout/);
  const submodule = repository(t);
  submodule.git(['update-index', '--add', '--cacheinfo', `160000,${submodule.base},module`]);
  rejectUnchanged(submodule, /Gitlinks/);
  const detached = repository(t);
  detached.git(['checkout', '--detach', 'HEAD']);
  rejectUnchanged(detached, /Detached HEAD/);
});

test('symlink index modes fail rather than silently omitting tracked inputs', (t) => {
  const repo = repository(t);
  fs.symlinkSync('tracked.txt', path.join(repo.root, 'link'));
  repo.git(['add', 'link']);
  rejectUnchanged(repo, /Unsupported tracked entry mode/);
});

test('relative effective index is resolved from original nested cwd and never changes caller state', async (t) => {
  const repo = repository(t);
  const nested = path.join(repo.root, 'tmp/nested');
  fs.mkdirSync(nested, { recursive: true });
  const index = path.join(repo.root, 'tmp/effective-index');
  fs.copyFileSync(path.join(repo.root, '.git/index'), index);
  const env = { ...repo.env, GIT_INDEX_FILE: '../effective-index', GIT_LITERAL_PATHSPECS: '1' };
  repo.put('web/src/app.ts', 'export {};\n');
  repo.git(['add', '--all'], { cwd: nested, env });
  const defaultIndex = fs.readFileSync(path.join(repo.root, '.git/index'));
  const effectiveIndex = fs.readFileSync(index);
  const originalEnvironment = { ...process.env };
  const originalCwd = process.cwd();
  const candidate = repo.prepare('staged', { cwd: nested, env });
  assert.equal(candidate.root, repo.root);
  assert.equal(candidate.indexPath, index);
  assert.deepEqual(await observe(candidate), web);
  assert.equal(env.GIT_INDEX_FILE, '../effective-index');
  assert.equal(env.GIT_LITERAL_PATHSPECS, '1');
  assert.equal(process.cwd(), originalCwd);
  assert.deepEqual({ ...process.env }, originalEnvironment);
  assert.deepEqual(fs.readFileSync(index), effectiveIndex);
  assert.deepEqual(fs.readFileSync(path.join(repo.root, '.git/index')), defaultIndex);
  repo.put('tracked.txt', 'unstaged alternate-index input\n');
  const beforeRejectedIndex = fs.readFileSync(index);
  const beforeRejectedFile = fs.readFileSync(path.join(repo.root, 'tracked.txt'));
  assert.throws(() => repo.prepare('staged', { cwd: nested, env }), /Unstaged tracked/);
  assert.deepEqual(fs.readFileSync(index), beforeRejectedIndex);
  assert.deepEqual(fs.readFileSync(path.join(repo.root, '.git/index')), defaultIndex);
  assert.deepEqual(fs.readFileSync(path.join(repo.root, 'tracked.txt')), beforeRejectedFile);
});

test('linked worktree uses its own root, branch and effective index', async (t) => {
  const repo = repository(t);
  const worktree = path.join(repo.area, 'linked');
  repo.git(['worktree', 'add', '-b', 'linked-feature', worktree]);
  fs.writeFileSync(path.join(worktree, 'CHANGELOG.md'), 'linked metadata\n');
  repo.git(['add', 'CHANGELOG.md'], { cwd: worktree });
  const candidate = repo.prepare('staged', { cwd: worktree });
  assert.equal(candidate.root, worktree);
  assert.equal(candidate.branch, 'refs/heads/linked-feature');
  assert.notEqual(candidate.indexPath, path.join(repo.root, '.git/index'));
  assert.deepEqual(await observe(candidate), ['docs']);
  assert.equal(fs.readFileSync(path.join(repo.root, 'CHANGELOG.md'), 'utf8'), 'initial\n');
});

test('missing origin/main or unrelated histories select all without fetching', (t) => {
  const missing = repository(t);
  missing.git(['update-ref', '-d', 'refs/remotes/origin/main']);
  missing.git(['remote', 'add', 'origin', 'file:///nonexistent-candidate-remote']);
  assert.equal(missing.prepare().baseline, undefined);
  assert.deepEqual(missing.prepare().gates, full);
  const unrelated = repository(t);
  const tree = unrelated.git(['rev-parse', 'HEAD^{tree}']);
  const orphan = unrelated.git(['commit-tree', tree, '-m', 'unrelated root']);
  unrelated.git(['update-ref', 'refs/remotes/origin/main', orphan]);
  assert.equal(unrelated.prepare().baseline, undefined);
  assert.deepEqual(unrelated.prepare().gates, full);
});

test('shallow history selects all even if a local merge-base appears available', (t) => {
  const repo = repository(t);
  fs.writeFileSync(path.join(repo.root, '.git/shallow'), `${repo.base}\n`);
  assert.equal(repo.prepare().baseline, undefined);
  assert.deepEqual(repo.prepare().gates, full);
});

test('multiple merge-bases select all instead of guessing one ancestor', (t) => {
  const repo = repository(t);
  const tree = repo.git(['rev-parse', 'HEAD^{tree}']);
  const left = repo.git(['commit-tree', tree, '-p', repo.base, '-m', 'left']);
  const right = repo.git(['commit-tree', tree, '-p', repo.base, '-m', 'right']);
  const a = repo.git(['commit-tree', tree, '-p', left, '-p', right, '-m', 'merge a']);
  const b = repo.git(['commit-tree', tree, '-p', right, '-p', left, '-m', 'merge b']);
  repo.git(['update-ref', 'refs/heads/feature', a]);
  repo.git(['update-ref', 'refs/remotes/origin/main', b]);
  assert.equal(repo.prepare().baseline, undefined);
  assert.deepEqual(repo.prepare().gates, full);
});

for (const identity of ['index', 'HEAD', 'branch']) {
  test(`candidate ${identity} change between lanes invalidates checks`, async (t) => {
    const repo = repository(t);
    repo.put('web/src/app.ts', 'export {};\n');
    repo.git(['add', 'web']);
    const candidate = repo.prepare();
    const visited = [];
    await assert.rejects(verifyCandidate(candidate, async (gates, hooks) => {
      for (const gate of gates) {
        await hooks.beforeGate(gate);
        visited.push(gate);
        await hooks.afterGate(gate);
        if (gate !== 'docs') continue;
        if (identity === 'index') {
          repo.put('CHANGELOG.md', 'new candidate\n');
          repo.git(['add', 'CHANGELOG.md']);
        } else if (identity === 'HEAD') {
          const tree = repo.git(['rev-parse', 'HEAD^{tree}']);
          const next = repo.git(['commit-tree', tree, '-p', repo.base, '-m', 'new head']);
          repo.git(['update-ref', 'HEAD', next]);
        } else {
          repo.git(['update-ref', 'refs/heads/other-feature', repo.base]);
          repo.git(['symbolic-ref', 'HEAD', 'refs/heads/other-feature']);
        }
      }
    }), /Candidate identity changed/);
    assert.deepEqual(visited, ['docs']);
    if (identity === 'index') assert.equal(fs.readFileSync(path.join(repo.root, 'CHANGELOG.md'), 'utf8'), 'new candidate\n');
  });
}

test('tracked build mutation is detected after a lane and left intact', async (t) => {
  const repo = repository(t);
  const originalIndex = fs.readFileSync(path.join(repo.root, '.git/index'));
  await assert.rejects(observe(repo.prepare(), () => repo.put('tracked.txt', 'build mutation\n')), /Unstaged tracked/);
  assert.equal(fs.readFileSync(path.join(repo.root, 'tracked.txt'), 'utf8'), 'build mutation\n');
  assert.deepEqual(fs.readFileSync(path.join(repo.root, '.git/index')), originalIndex);
});

test('end-of-run check detects mutation after the final afterGate callback', async (t) => {
  const repo = repository(t);
  await assert.rejects(verifyCandidate(repo.prepare(), async (gates, hooks) => {
    for (const gate of gates) {
      await hooks.beforeGate(gate);
      await hooks.afterGate(gate);
    }
    repo.put('unexpected.txt', 'new input\n');
  }), /Nonignored untracked/);
});

test('candidate is rechecked before the executor is invoked', async (t) => {
  const repo = repository(t);
  const candidate = repo.prepare();
  repo.put('tracked.txt', 'changed after preparation\n');
  let called = false;
  await assert.rejects(verifyCandidate(candidate, async () => { called = true; }), /Unstaged tracked/);
  assert.equal(called, false);
});

test('shared runner errors propagate without a success record', async (t) => {
  const repo = repository(t);
  const failure = new Error('actual lane failed');
  await assert.rejects(verifyCandidate(repo.prepare(), async (gates, hooks) => {
    await hooks.beforeGate(gates[0]);
    await hooks.afterGate(gates[0]);
    throw failure;
  }), (error) => error === failure);
  assert.deepEqual(await observe(repo.prepare()), ['docs']);
});

test('all protected remote refs reject updates, force updates and deletes before gates', (t) => {
  const repo = repository(t);
  for (const remote of ['refs/heads/main', 'refs/heads/master', 'main', 'master']) {
    for (const oid of [repo.base, zero]) {
      const before = repo.snapshot();
      const input = repo.update(repo.base) + repo.update(oid, remote, oid === zero ? '(delete)' : 'refs/heads/feature');
      assert.throws(() => repo.prepare('push', { input }), /remote .* is protected/);
      assert.deepEqual(repo.snapshot(), before);
    }
  }
});

test('all push input is parsed before any lane and malformed records fail closed', (t) => {
  const repo = repository(t);
  for (const input of [
    'bad\n', `${repo.update()}\n`, repo.update().replace(repo.base, 'HEAD'),
    repo.update().replace('refs/heads/feature ', 'refs/heads/bad\tref '),
    repo.update() + `refs/heads/other ${repo.base} refs/heads/other invalid\n`,
    repo.update(repo.base, 'invalid-ref'),
  ]) {
    assert.throws(() => repo.prepare('push', { input }), /Invalid .*push input|Invalid ref/);
  }
});

test('actual input OID, not local ref lookup, determines the push boundary', (t) => {
  const repo = repository(t);
  repo.put('CHANGELOG.md', 'new head\n');
  const head = repo.commit();
  repo.git(['update-ref', 'refs/heads/old-name', repo.base]);
  // The supplied old OID must not silently become the current tip of feature.
  assert.throws(() => repo.prepare('push', { input: repo.update(repo.base) }), /must peel to current HEAD/);
  // Conversely, do not re-resolve a possibly changed local ref name.
  const candidate = repo.prepare('push', { input: repo.update(head, 'refs/heads/topic', 'refs/heads/old-name') });
  assert.equal(candidate.head, head);
  assert.deepEqual(candidate.gates, ['docs']);
});

test('HEAD object IDs and revision-expression push sources use the actual supplied OID', async (t) => {
  const repo = repository(t);
  repo.put('CHANGELOG.md', 'new head\n');
  const head = repo.commit();
  for (const source of [head, 'HEAD~0', 'HEAD']) {
    const candidate = repo.prepare('push', { input: repo.update(head, 'refs/heads/topic', source) });
    assert.deepEqual(await observe(candidate), ['docs']);
    assert.throws(() => repo.prepare('push', {
      input: repo.update(repo.base, 'refs/heads/topic', source),
    }), /must peel to current HEAD/);
  }
});

test('another branch commit, tree and blob cannot be pushed as the current candidate', (t) => {
  const repo = repository(t);
  const tree = repo.git(['rev-parse', 'HEAD^{tree}']);
  const other = repo.git(['commit-tree', tree, '-p', repo.base, '-m', 'other branch']);
  const blob = repo.git(['rev-parse', 'HEAD:CHANGELOG.md']);
  for (const oid of [other, tree, blob, 'f'.repeat(40)]) {
    assert.throws(() => repo.prepare('push', { input: repo.update(oid, 'refs/tags/object', 'refs/tags/object') }), /must peel to current HEAD/);
  }
});

test('annotated tags peeling to HEAD and multiple HEAD refs validate exactly once', async (t) => {
  const repo = repository(t);
  repo.git(['tag', '-a', 'candidate-tag', '-m', 'annotated candidate']);
  const tag = repo.git(['rev-parse', 'refs/tags/candidate-tag']);
  let executions = 0;
  const candidate = repo.prepare('push', {
    input: repo.update() + repo.update(tag, 'refs/tags/candidate-tag', 'refs/tags/candidate-tag'),
  });
  await verifyCandidate(candidate, async (gates, hooks) => {
    executions++;
    assert.deepEqual(gates, ['docs']);
    for (const gate of gates) {
      await hooks.beforeGate(gate);
      await hooks.afterGate(gate);
    }
  });
  assert.equal(executions, 1);
});

test('push requires index and working tree to match HEAD without mutation', (t) => {
  const staged = repository(t);
  staged.put('CHANGELOG.md', 'staged\n');
  staged.git(['add', 'CHANGELOG.md']);
  const before = staged.snapshot();
  assert.throws(() => staged.prepare('push', { input: staged.update() }), /index and working tree to match HEAD/);
  assert.deepEqual(staged.snapshot(), before);
  const dirty = repository(t);
  dirty.put('tracked.txt', 'unstaged\n');
  assert.throws(() => dirty.prepare('push', { input: dirty.update() }), /Unstaged tracked/);
  const untracked = repository(t);
  untracked.put('untracked.txt');
  assert.throws(() => untracked.prepare('push', { input: untracked.update() }), /Nonignored untracked/);
});

test('nonprotected deletes and empty push input do not execute gates, even with dirty files', async (t) => {
  const repo = repository(t);
  repo.put('tracked.txt', 'dirty\n');
  repo.put('untracked.txt');
  for (const input of ['', repo.update(zero, 'refs/heads/obsolete', '(delete)')]) {
    let executed = false;
    await verifyCandidate(repo.prepare('push', { input }), async () => { executed = true; });
    assert.equal(executed, false);
    const result = repo.cli(['--push'], { input });
    assert.equal(result.status, 0, result.stderr);
  }
});

test('mixed push deletes and HEAD updates still validate the live candidate', async (t) => {
  const repo = repository(t);
  const input = repo.update(zero, 'refs/heads/obsolete', '(delete)') + repo.update();
  assert.deepEqual(await observe(repo.prepare('push', { input })), ['docs']);
  repo.put('tracked.txt', 'dirty\n');
  assert.throws(() => repo.prepare('push', { input }), /Unstaged tracked/);
});

test('CLI rejects unknown, combined, duplicate and missing mode arguments', (t) => {
  const repo = repository(t);
  for (const args of [[], ['--all'], ['--staged', '--push'], ['--staged', '--staged'], ['--push', '--skip']]) {
    const result = repo.cli(args);
    assert.equal(result.status, 1);
    assert.match(result.stderr, /Use exactly --staged or --push/);
  }
});

test('commit and merge hooks preserve main/master protection without invoking gates', (t) => {
  const repo = repository(t);
  for (const branch of ['main', 'master']) {
    repo.git(['checkout', '-B', branch]);
    for (const hook of ['pre-commit', 'pre-merge-commit']) {
      const before = repo.snapshot();
      const result = spawnSync('sh', [path.join(checkout, '.githooks', hook)], {
        cwd: repo.root, env: repo.env, encoding: 'utf8',
      });
      assert.equal(result.status, 1);
      assert.match(result.stderr, /local main\/master is read-only/);
      assert.deepEqual(repo.snapshot(), before);
    }
  }
});

test('push hook forwards the entire stdin including a later protected deletion', (t) => {
  const repo = repository(t);
  // Install the real modules, not fake gate commands. The protected-ref path
  // must reject before runGates, and deletion-only input must require no tools.
  fs.mkdirSync(path.join(repo.root, 'scripts'));
  for (const name of ['verify-candidate.mjs', 'ci-scope.mjs', 'verify-ci.mjs']) {
    fs.copyFileSync(path.join(checkout, 'scripts', name), path.join(repo.root, 'scripts', name));
  }
  const invoke = (input) => spawnSync('sh', [path.join(checkout, '.githooks/pre-push')], {
    cwd: repo.root, env: repo.env, input, encoding: 'utf8',
  });
  const blocked = invoke(repo.update() + repo.update(zero, 'refs/heads/main', '(delete)'));
  assert.equal(blocked.status, 1);
  assert.match(blocked.stderr, /remote refs\/heads\/main is protected/);
  const deletion = invoke(repo.update(zero, 'refs/heads/obsolete', '(delete)'));
  assert.equal(deletion.status, 0, deletion.stderr);
});
