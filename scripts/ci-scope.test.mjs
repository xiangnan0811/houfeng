import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { execFileSync, spawnSync } from 'node:child_process';
import { classifyPaths } from './ci-scope.mjs';

const checkout = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const script = path.join(checkout, 'scripts/ci-scope.mjs');
const scratch = path.join(checkout, 'tmp/agent');
const none = { go: false, postgres: false, web: false, browser: false, image: false };
const all = { go: true, postgres: true, web: true, browser: true, image: true };
const web = { go: true, postgres: false, web: true, browser: true, image: true };
const webDocs = { go: false, postgres: false, web: true, browser: true, image: false };

function repository(t, files = { 'CHANGELOG.md': 'initial\n' }) {
  fs.mkdirSync(scratch, { recursive: true });
  const root = fs.mkdtempSync(path.join(scratch, 'scope-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('GIT_')));
  Object.assign(env, {
    GIT_CONFIG_NOSYSTEM: '1', GIT_CONFIG_GLOBAL: '/dev/null', GIT_TERMINAL_PROMPT: '0',
    GIT_AUTHOR_NAME: 'Scope test', GIT_AUTHOR_EMAIL: 'scope@example.invalid',
    GIT_COMMITTER_NAME: 'Scope test', GIT_COMMITTER_EMAIL: 'scope@example.invalid',
  });
  const git = (args, overrides = {}) => execFileSync('git', args, { cwd: root, env: { ...env, ...overrides }, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'] }).trim();
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
  const cli = (args, options = {}) => spawnSync(process.execPath, [script, ...args], {
    cwd: options.cwd ?? root, env: { ...env, ...options.env }, encoding: 'utf8',
  });
  git(['init', '--initial-branch=feature']);
  for (const [name, content] of Object.entries(files)) put(name, content);
  const base = commit();
  return { root, git, put, commit, cli, base };
}

function result(run) {
  assert.equal(run.status, 0, run.stderr || run.error?.message);
  assert.equal(run.stderr, '');
  return JSON.parse(run.stdout);
}

test('classification has five fixed boolean keys and combines paths by union', () => {
  assert.deepEqual(classifyPaths([]), none);
  assert.deepEqual(classifyPaths(['CHANGELOG.md', '.release-please-manifest.json']), none);
  assert.deepEqual(classifyPaths(['CHANGELOG.md', 'db/migrations/001.sql']), all);
  assert.deepEqual(classifyPaths(['docs/guide.md', 'web/src/App.tsx']), web);
  assert.deepEqual(classifyPaths(['docs/spec/web/quality.md', 'internal/center/server.go']), all);
  const first = classifyPaths([]);
  first.go = true;
  assert.deepEqual(classifyPaths([]), none);
});

test('Web source, browser corpus and pinned Node retain Go coverage but not PostgreSQL', () => {
  for (const name of ['web/src/App.tsx', 'web/e2e/example.spec.ts', 'web/package-lock.json', '.node-version']) {
    assert.deepEqual(classifyPaths([name]), web, name);
  }
});

test('ordinary docs are light while deployment and Web contracts retain consumers', () => {
  for (const name of ['docs/README.md', 'docs/design/guide.md', 'docs/spec/backend/quality-guidelines.md']) {
    assert.deepEqual(classifyPaths([name]), none, name);
  }
  for (const name of ['README.md', 'docs/deploy/example.md', 'docs/deploy/sql/restore.sql',
    'docs/spec/contracts/platform/production-compose.md', 'docs/spec/contracts/platform/compose-authority.md']) {
    assert.deepEqual(classifyPaths([name]), all, name);
  }
  for (const name of ['docs/spec/web/quality.md', 'docs/development/ui-preview-and-browser-sanity.md']) {
    assert.deepEqual(classifyPaths([name]), webDocs, name);
  }
});

test('backend, automation and unknown paths always select every heavy lane', () => {
  for (const name of ['agent/main.go', 'cmd/center/main.go', 'internal/center/http/csp.go', 'db/schema.sql',
    'testdata/corpus.json', 'go.mod', 'go.sum', 'Makefile', 'scripts/ci-scope.mjs', '.githooks/pre-commit',
    '.github/workflows/ci.yml', 'Dockerfile', '.dockerignore', '.gitignore', 'LICENSE', 'compose.yaml',
    'compose.production.yaml', 'release-please-config.json', 'unknown.md', 'unknown.json']) {
    assert.deepEqual(classifyPaths([name]), all, name);
  }
});

test('directory boundaries and metadata names are exact, never guessed from extensions', () => {
  for (const name of ['docs-other/guide.md', 'web-other/app.ts', 'docs', 'web', 'CHANGELOG.md.old',
    'nested/CHANGELOG.md', 'nested/.release-please-manifest.json', '.node-version.old', '../docs/guide.md',
    '/docs/guide.md', 'docs/../other.md', 'docs//guide.md', '', 'docs/bad\0path']) {
    assert.deepEqual(classifyPaths([name]), all, name);
  }
  // These are ordinary docs, not descendants of a special directory.
  for (const name of ['docs/deployment/guide.md', 'docs/spec/website/guide.md',
    'docs/spec/contracts/platform/compose-authority.md.old']) {
    assert.deepEqual(classifyPaths([name]), none, name);
  }
});

test('CLI compares actual trees rather than index or working tree and permits no changes', (t) => {
  const repo = repository(t);
  repo.put('CHANGELOG.md', 'metadata\n');
  const head = repo.commit();
  repo.put('db/uncommitted.sql');
  repo.git(['add', 'db/uncommitted.sql']);
  assert.deepEqual(result(repo.cli(['--base', repo.base, '--head', head])), none);
  assert.deepEqual(result(repo.cli(['--base', head, '--head', head])), none);
  assert.deepEqual(result(repo.cli(['--base', head, '--staged'])), all);
  const baseTree = repo.git(['rev-parse', `${repo.base}^{tree}`]);
  const headTree = repo.git(['rev-parse', `${head}^{tree}`]);
  assert.deepEqual(result(repo.cli(['--base', baseTree, '--head', headTree])), none);
});

test('rename includes deleted backend path even when destination is ordinary docs', (t) => {
  const repo = repository(t, { 'db/schema.sql': 'select 1;\n' });
  fs.mkdirSync(path.join(repo.root, 'docs'));
  repo.git(['mv', 'db/schema.sql', 'docs/schema.md']);
  const head = repo.commit();
  assert.deepEqual(result(repo.cli(['--base', repo.base, '--head', head])), all);
});

test('deleting a Web path still requires its original consumer lanes', (t) => {
  const repo = repository(t, { 'web/src/app.ts': 'export {};\n', 'CHANGELOG.md': 'initial\n' });
  repo.git(['rm', 'web/src/app.ts']);
  assert.deepEqual(result(repo.cli(['--base', repo.base, '--staged'])), web);
});

test('spaces, tabs, newlines and Unicode remain whole NUL-delimited paths', (t) => {
  const repo = repository(t);
  for (const name of ['docs/with spaces.md', 'docs/with\ttab.md', 'docs/line\ndb/schema.sql', 'docs/中文.md']) repo.put(name);
  const head = repo.commit();
  assert.deepEqual(result(repo.cli(['--base', repo.base, '--head', head])), none);
});

test('undecodable Git path bytes conservatively select all lanes', (t) => {
  const repo = repository(t);
  fs.mkdirSync(path.join(repo.root, 'docs'));
  const target = Buffer.concat([Buffer.from(`${repo.root}/docs/`), Buffer.from([0xff]), Buffer.from('.md')]);
  fs.writeFileSync(target, 'invalid UTF-8 filename\n');
  const head = repo.commit();
  assert.deepEqual(result(repo.cli(['--base', repo.base, '--head', head])), all);
});

test('staged comparisons honor an effective relative index without using the default index', (t) => {
  const repo = repository(t);
  const alternate = 'candidate-index';
  fs.copyFileSync(path.join(repo.root, '.git/index'), path.join(repo.root, alternate));
  repo.put('db/staged.sql');
  repo.git(['add', 'db/staged.sql'], { GIT_INDEX_FILE: alternate });
  assert.deepEqual(result(repo.cli(['--base', repo.base, '--staged'], { env: { GIT_INDEX_FILE: alternate } })), all);
  assert.deepEqual(result(repo.cli(['--base', repo.base, '--staged'])), none);
});

test('Git root relative paths are retained from a subdirectory even with diff.relative enabled', (t) => {
  const repo = repository(t, { 'docs/guide.md': 'initial\n' });
  repo.git(['config', 'diff.relative', 'true']);
  repo.put('web/src/app.ts');
  repo.git(['add', '--all']);
  assert.deepEqual(result(repo.cli(['--base', repo.base, '--staged'], { cwd: path.join(repo.root, 'docs') })), web);
});

test('optional GitHub outputs append exactly five fixed keys and do not alter JSON stdout', (t) => {
  const repo = repository(t);
  const output = path.join(repo.root, 'actions output.txt');
  fs.writeFileSync(output, 'existing=value\n');
  assert.deepEqual(result(repo.cli(['--all', '--github-output', output])), all);
  assert.equal(fs.readFileSync(output, 'utf8'), 'existing=value\ngo=true\npostgres=true\nweb=true\nbrowser=true\nimage=true\n');
  assert.deepEqual(result(repo.cli(['--base', repo.base, '--head', repo.base, '--github-output', output])), none);
  assert.ok(fs.readFileSync(output, 'utf8').endsWith('go=false\npostgres=false\nweb=false\nbrowser=false\nimage=false\n'));
  const absent = path.join(repo.root, 'not-requested-output');
  assert.deepEqual(result(repo.cli(['--all'], { env: { GITHUB_OUTPUT: absent } })), all);
  assert.equal(fs.existsSync(absent), false);
});

test('missing or invalid baseline fails instead of becoming an empty diff; callers can explicitly select all', (t) => {
  const repo = repository(t);
  for (const base of ['0'.repeat(40), 'f'.repeat(40), 'not-an-oid', 'HEAD', '--all']) {
    const run = repo.cli(['--base', base, '--head', repo.base]);
    assert.notEqual(run.status, 0, base);
    assert.equal(run.stdout, '', base);
  }
  const run = repo.cli(['--base', repo.base, '--head', 'f'.repeat(40)]);
  assert.notEqual(run.status, 0);
  assert.equal(run.stdout, '');
  assert.deepEqual(result(repo.cli(['--all'])), all);
});

test('Git failures never write success outputs', (t) => {
  const repo = repository(t);
  const output = path.join(repo.root, 'actions-output');
  fs.writeFileSync(output, 'existing=value\n');
  const run = repo.cli(['--base', 'f'.repeat(40), '--staged', '--github-output', output]);
  assert.notEqual(run.status, 0);
  assert.equal(run.stdout, '');
  assert.equal(fs.readFileSync(output, 'utf8'), 'existing=value\n');
  const outside = path.join(repo.root, 'outside');
  fs.mkdirSync(outside);
  const noRepo = repo.cli(['--base', repo.base, '--staged'], { cwd: outside, env: { GIT_CEILING_DIRECTORIES: repo.root } });
  assert.notEqual(noRepo.status, 0);
  assert.equal(noRepo.stdout, '');
});

test('CLI rejects unknown, duplicate, incomplete and incompatible arguments', (t) => {
  const repo = repository(t);
  for (const args of [[], ['--unknown'], ['--all', '--all'], ['--all', '--staged'],
    ['--all', '--base', repo.base], ['--all', '--head', repo.base], ['--base', repo.base],
    ['--head', repo.base], ['--staged'], ['--base'], ['--base', repo.base, '--head'],
    ['--base', repo.base, '--head', repo.base, '--staged'], ['--base', repo.base, '--base', repo.base, '--staged'],
    ['--all', '--github-output'], ['--all', '--github-output', ''],
    ['--all', '--github-output', 'one', '--github-output', 'two'], ['--all', 'unexpected']]) {
    const run = repo.cli(args);
    assert.notEqual(run.status, 0, JSON.stringify(args));
    assert.equal(run.stdout, '', JSON.stringify(args));
  }
});
