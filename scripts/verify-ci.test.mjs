import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { spawnSync } from 'node:child_process';
import { pathToFileURL } from 'node:url';
import { runGates, validateReleaseManifest } from './verify-ci.mjs';

const checkout = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const scratch = path.join(checkout, 'tmp/agent');

function fixture(t) {
  fs.mkdirSync(scratch, { recursive: true });
  const root = fs.mkdtempSync(path.join(scratch, 'manifest-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  return { root, file: path.join(root, '.release-please-manifest.json') };
}

test('manifest accepts only the single-root stable-version contract', (t) => {
  const { root, file } = fixture(t);
  for (const version of ['1.27.0', '0.0.0', '10.200.3000', '999999999999999999999.0.1']) {
    const manifest = { '.': version };
    fs.writeFileSync(file, `${JSON.stringify(manifest, null, 2)}\n`);
    assert.deepEqual(validateReleaseManifest(root), manifest);
  }
});

test('manifest rejects missing files and invalid JSON without creating or repairing files', (t) => {
  const { root, file } = fixture(t);
  assert.throws(() => validateReleaseManifest(root), /Invalid \.release-please-manifest\.json/);
  assert.equal(fs.existsSync(file), false);
  for (const text of ['', '{', '{".":"1.2.3",}', 'not JSON']) {
    fs.writeFileSync(file, text);
    assert.throws(() => validateReleaseManifest(root), /Invalid \.release-please-manifest\.json/);
    assert.equal(fs.readFileSync(file, 'utf8'), text);
  }
  fs.rmSync(file);
  assert.throws(() => validateReleaseManifest(root), /Invalid \.release-please-manifest\.json/);
});

test('manifest rejects arrays, primitive values, missing root and extra package keys', (t) => {
  const { root, file } = fixture(t);
  for (const manifest of [null, [], ['1.2.3'], '1.2.3', true, 123, {},
    { web: '1.2.3' }, { '.': '1.2.3', web: '1.2.3' }, { '.': '1.2.3', extra: null }]) {
    const text = JSON.stringify(manifest);
    fs.writeFileSync(file, text);
    assert.throws(() => validateReleaseManifest(root), /only the "\." package key/, text);
    assert.equal(fs.readFileSync(file, 'utf8'), text);
  }
});

test('manifest rejects nonstrings, prefixes, prereleases, build metadata and noncanonical integers', (t) => {
  const { root, file } = fixture(t);
  for (const version of [null, 123, true, {}, [], '', 'v1.2.3', 'V1.2.3', '1.2', '1.2.3.4',
    '1.2.3-beta.1', '1.2.3+build', '-1.2.3', '1.-2.3', '1.2.-3', '+1.2.3',
    '01.2.3', '1.02.3', '1.2.03', '1e2.2.3', ' 1.2.3', '1.2.3 ', '1.2.3\n',
    '1.2.3\r\n', '\n1.2.3', '1.2.3\t', '１.2.3']) {
    const text = JSON.stringify({ '.': version });
    fs.writeFileSync(file, text);
    assert.throws(() => validateReleaseManifest(root), /stable three-part nonnegative integer version/, text);
    assert.equal(fs.readFileSync(file, 'utf8'), text);
  }
});

const runnerFile = path.join(checkout, 'scripts/verify-ci.mjs');
const runnerURL = pathToFileURL(runnerFile).href;

function gitEnvironment() {
  const env = { ...process.env };
  const result = spawnSync('git', ['rev-parse', '--local-env-vars'], { encoding: 'utf8' });
  assert.equal(result.status, 0);
  for (const key of [...result.stdout.trim().split('\n'), 'GIT_CONFIG_COUNT', 'GIT_CONFIG_PARAMETERS']) delete env[key];
  for (const key of Object.keys(env)) if (/^GIT_CONFIG_(?:KEY|VALUE)_/.test(key)) delete env[key];
  return env;
}

function repository(t) {
  const { root } = fixture(t);
  const result = spawnSync('git', ['init', '--quiet', root], { env: gitEnvironment(), encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr);
  return root;
}

function nodeIn(root, source, extraEnv = {}) {
  return spawnSync(process.execPath, ['--input-type=module', '--eval', source], {
    cwd: root, env: { ...gitEnvironment(), ...extraEnv }, encoding: 'utf8', timeout: 20_000,
  });
}

test('runner rejects invalid imported arguments before callbacks or gate execution', async () => {
  let callbacks = 0;
  const beforeGate = () => { callbacks += 1; };
  for (const gates of [null, 'go', [], [''], ['unknown'], ['go', 'go'], ['go', 1]]) {
    await assert.rejects(runGates(gates, { beforeGate }), /nonempty list of distinct names/);
  }
  for (const options of [null, [], 'options', { unknown: true }, { beforeGate: 1 }, { afterGate: null }]) {
    await assert.rejects(runGates(['go'], options), /options|option|function/);
  }
  for (const [gates, options] of [
    [['postgres'], { postgresImage: 'postgres:16-alpine' }],
    [['postgres'], { postgresImage: 'postgres:16.0\n' }],
    [['go'], { postgresImage: 'postgres:16.0' }],
    [['docs', 'postgres'], { postgresImage: 'postgres:16.6' }],
    [['browser'], { shard: '0/2' }],
    [['browser'], { shard: '1/3' }],
    [['browser'], { shard: 1 }],
    [['browser'], { shard: '1/2\n' }],
    [['web', 'browser'], { shard: '1/2' }],
    [['postgres'], { shard: '1/2' }],
    [['browser'], { postgresImage: 'postgres:16.12', shard: '1/2' }],
  ]) {
    await assert.rejects(runGates(gates, { ...options, beforeGate }), /requires only/);
  }
  assert.equal(callbacks, 0);
});

test('CLI rejects unknown, duplicated, missing and semantically invalid arguments without starting gates', (t) => {
  const { root } = fixture(t);
  for (const args of [
    ['--unknown'], ['--gates'], ['--gates', 'go', '--gates', 'web'],
    ['--gates', 'go,go'], ['--gates', ''], ['--gates', 'go,'],
    ['--gates', 'browser', '--shard', '1/3'],
    ['--gates', 'web', '--postgres-image', 'postgres:16.0'],
    ['--postgres-image', 'postgres:16.0'], ['--shard', '1/2'],
    ['--gates', 'browser', '--shard', '1/2', '--shard', '2/2'],
  ]) {
    const result = spawnSync(process.execPath, [runnerFile, ...args], {
      cwd: root, env: gitEnvironment(), encoding: 'utf8', timeout: 10_000,
    });
    assert.equal(result.status, 1, JSON.stringify(args));
    assert.equal(result.stdout, '');
    assert.doesNotMatch(result.stderr, /Git query failed/);
  }
});

test('importing the runner outside Git has no execution side effects', (t) => {
  const { root } = fixture(t);
  const result = nodeIn(root, `
    import assert from 'node:assert/strict';
    process.argv[1] = 'not-an-existing-script';
    const { runGates, validateReleaseManifest } = await import(${JSON.stringify(runnerURL)});
    assert.equal(typeof runGates, 'function');
    assert.equal(typeof validateReleaseManifest, 'function');
  `);
  assert.equal(result.status, 0, result.stderr);
  assert.equal(result.stdout, '');
  assert.equal(fs.existsSync(path.join(root, 'tmp')), false);
});

test('before callbacks use fixed ordering, await completion and prevent execution on rejection', (t) => {
  const root = repository(t);
  const result = nodeIn(root, `
    import assert from 'node:assert/strict';
    import fs from 'node:fs';
    import { runGates } from ${JSON.stringify(runnerURL)};
    const calls = [];
    await assert.rejects(runGates(['browser', 'web', 'go'], {
      async beforeGate(gate) {
        await Promise.resolve();
        calls.push(gate);
        throw new Error('candidate changed');
      },
      afterGate() { calls.push('after'); },
    }), /candidate changed/);
    assert.deepEqual(calls, ['go']);
    assert.deepEqual(fs.readdirSync('tmp/agent'), []);
  `);
  assert.equal(result.status, 0, result.stderr);
});

test('valid restricted PG versions and browser shards reach the candidate boundary', (t) => {
  const root = repository(t);
  const result = nodeIn(root, `
    import assert from 'node:assert/strict';
    import { runGates } from ${JSON.stringify(runnerURL)};
    for (const postgresImage of ['postgres:16.0', 'postgres:16.6', 'postgres:16.12']) {
      await assert.rejects(runGates(['postgres'], {
        postgresImage, beforeGate() { throw new Error('boundary'); },
      }), /boundary/);
    }
    for (const shard of ['1/2', '2/2']) {
      await assert.rejects(runGates(['browser'], {
        shard, beforeGate() { throw new Error('boundary'); },
      }), /boundary/);
    }
  `);
  assert.equal(result.status, 0, result.stderr);
});

test('Web gates reject ignored and nonignored private environment inputs without reading or deleting them', (t) => {
  const root = repository(t);
  fs.mkdirSync(path.join(root, 'web'));
  fs.writeFileSync(path.join(root, '.gitignore'), 'web/.env*\n');
  const files = ['.env', '.env.local', '.env.test', '.env.test.local', '.env.production', '.env.production.private', '.env.test\nprivate'];
  for (const file of files) {
    fs.writeFileSync(path.join(root, 'web', file), 'private-value-that-must-not-appear');
  }
  const result = nodeIn(root, `
    import assert from 'node:assert/strict';
    import fs from 'node:fs';
    import { runGates } from ${JSON.stringify(runnerURL)};
    for (const gate of ['web', 'browser']) {
      const calls = [];
      await assert.rejects(runGates([gate], {
        async beforeGate(name) { calls.push('before:' + name); },
        async afterGate(name) { calls.push('after:' + name); },
      }), (error) => {
        assert.match(error.message, /Untracked Web environment files/);
        for (const file of ${JSON.stringify(files)}) assert.ok(error.message.includes(JSON.stringify('web/' + file)));
        assert.ok(!error.message.includes('private-value-that-must-not-appear'));
        return true;
      });
      assert.deepEqual(calls, ['before:' + gate, 'after:' + gate]);
      assert.deepEqual(fs.readdirSync('tmp/agent'), []);
    }
    fs.unlinkSync('.gitignore');
    await assert.rejects(runGates(['web']), /Untracked Web environment files/);
  `);
  assert.equal(result.status, 0, result.stderr);
  for (const file of files) {
    assert.equal(fs.readFileSync(path.join(root, 'web', file), 'utf8'), 'private-value-that-must-not-appear');
  }
});

test('after-candidate rejection preserves both failures and prevents subsequent lanes', (t) => {
  const root = repository(t);
  fs.mkdirSync(path.join(root, 'web'));
  fs.writeFileSync(path.join(root, 'web/.env.local'), 'private');
  const result = nodeIn(root, `
    import assert from 'node:assert/strict';
    import { runGates } from ${JSON.stringify(runnerURL)};
    const calls = [];
    await assert.rejects(runGates(['browser', 'web'], {
      beforeGate(name) { calls.push('before:' + name); },
      afterGate(name) {
        calls.push('after:' + name);
        throw new Error('index changed');
      },
    }), (error) => {
      assert.ok(error instanceof AggregateError);
      assert.equal(error.errors.length, 2);
      assert.match(error.errors[0].message, /Untracked Web environment files/);
      assert.match(error.errors[1].message, /index changed/);
      return true;
    });
    assert.deepEqual(calls, ['before:web', 'after:web']);
  `);
  assert.equal(result.status, 0, result.stderr);
});

test('gate subprocesses receive an isolated environment and report real child failure without mutating the caller', (t) => {
  const root = repository(t);
  const result = nodeIn(root, `
    import assert from 'node:assert/strict';
    import fs from 'node:fs';
    import path from 'node:path';
    import childProcess from 'node:child_process';
    import { syncBuiltinESMExports } from 'node:module';

    const poisoned = {
      HOUFENG_DATABASE_URL: 'private-database', HOUFENG_POSTGRES_INTEGRATION: '1',
      HOUFENG_RECORDS_KEEP_WORKSPACE: '1', DATABASE_URL: 'private-database',
      PGHOST: 'private-host', PGPASSWORD: 'private-password', VITE_API_URL: 'private-host',
      NODE_OPTIONS: '--throw-deprecation', NODE_PATH: '/unrelated/modules',
      NODE_ENV: 'development', GOFLAGS: '-run=^$', GOENV: '/unrelated/goenv',
      GOWORK: '/unrelated/go.work', GOTOOLCHAIN: 'auto', GOOS: 'windows',
      MAKEFLAGS: '-n', GO_VERIFY_PATTERNS: './nothing', NPM_CONFIG_IGNORE_SCRIPTS: 'true',
      GIT_DIR: '.git', GIT_WORK_TREE: '.', GIT_INDEX_FILE: 'candidate-index',
      GIT_CONFIG_COUNT: '1', GIT_CONFIG_KEY_0: 'core.abbrev', GIT_CONFIG_VALUE_0: '12',
      PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD: '1', CI: 'true', GITHUB_ACTIONS: 'false',
    };
    Object.assign(process.env, poisoned);
    process.env.GOCACHE = path.join(process.cwd(), 'cache');
    const retained = Object.fromEntries(['PATH', 'HOME', 'GOCACHE', 'HTTPS_PROXY']
      .filter((key) => process.env[key] !== undefined).map((key) => [key, process.env[key]]));
    const original = { ...process.env };
    const childSource = \`
      import assert from 'node:assert/strict';
      import fs from 'node:fs';
      import path from 'node:path';
      import { spawnSync } from 'node:child_process';
      for (const key of \${JSON.stringify(Object.keys(poisoned).filter((key) =>
        !['GOENV', 'GOWORK', 'GOTOOLCHAIN', 'GITHUB_ACTIONS'].includes(key)))}) {
        assert.equal(process.env[key], undefined, key + ' leaked into a gate');
      }
      assert.equal(process.env.GOTOOLCHAIN, 'local');
      assert.equal(process.env.GOWORK, 'off');
      assert.equal(process.env.GOENV, 'off');
      for (const [key, value] of Object.entries(\${JSON.stringify(retained)})) {
        assert.equal(process.env[key], value, key + ' was not retained');
      }
      const scratch = fs.realpathSync(process.env.TMPDIR);
      assert.ok(scratch.startsWith(path.join(process.cwd(), 'tmp/agent/verify-ci-')));
      assert.equal(process.env.TMP, scratch);
      assert.equal(process.env.TEMP, scratch);
      assert.equal(process.env.GOTMPDIR, scratch);
      assert.equal(fs.statSync(scratch).mode & 0o777, 0o700);
      const other = path.join(scratch, 'independent-repo');
      assert.equal(spawnSync('git', ['init', '--quiet', other]).status, 0);
      assert.equal(spawnSync('git', ['rev-parse', '--show-toplevel'], {
        cwd: other, encoding: 'utf8',
      }).stdout.trim(), other);
      process.exit(42);
    \`;
    // Replace only the external tool at the process boundary with a real Node
    // probe. Assertions run in the actual isolated child, not against wiring or
    // echoed command strings; selftests need no Make, Go, npm or Docker.
    const realSpawn = childProcess.spawn;
    let children = 0;
    childProcess.spawn = (_command, _args, options) => {
      children += 1;
      return realSpawn(process.execPath, ['--input-type=module', '--eval', childSource], options);
    };
    syncBuiltinESMExports();
    const { runGates } = await import(${JSON.stringify(runnerURL)});
    const calls = [];
    const interrupts = process.listenerCount('SIGINT');
    const terminations = process.listenerCount('SIGTERM');
    await assert.rejects(runGates(['image'], {
      beforeGate(gate) { calls.push('before:' + gate); },
      afterGate(gate) { calls.push('after:' + gate); },
    }), /docker failed \\(exit 42\\)/);
    assert.deepEqual(calls, ['before:image', 'after:image']);
    assert.equal(children, 1);
    assert.deepEqual({ ...process.env }, original);
    assert.equal(process.listenerCount('SIGINT'), interrupts);
    assert.equal(process.listenerCount('SIGTERM'), terminations);
    assert.deepEqual(fs.readdirSync('tmp/agent'), []);
  `);
  assert.equal(result.status, 0, result.stderr);
});

test('an interrupt rejects even if the owned subprocess handles termination successfully', (t) => {
  const root = repository(t);
  const result = nodeIn(root, `
    import assert from 'node:assert/strict';
    import fs from 'node:fs';
    import childProcess from 'node:child_process';
    import { syncBuiltinESMExports } from 'node:module';
    const realSpawn = childProcess.spawn;
    const probe = \`
      process.on('SIGTERM', () => process.exit(0));
      process.kill(process.ppid, 'SIGTERM');
      setInterval(() => {}, 1000);
    \`;
    childProcess.spawn = (_command, _args, options) =>
      realSpawn(process.execPath, ['--eval', probe], options);
    syncBuiltinESMExports();
    const { runGates } = await import(${JSON.stringify(runnerURL)});
    const calls = [];
    await assert.rejects(runGates(['image'], {
      beforeGate(gate) { calls.push('before:' + gate); },
      afterGate(gate) { calls.push('after:' + gate); },
    }), /Verification interrupted by SIGTERM/);
    assert.deepEqual(calls, ['before:image', 'after:image']);
    assert.deepEqual(fs.readdirSync('tmp/agent'), []);
  `);
  assert.equal(result.status, 0, result.stderr);
});

test('Web environment discovery normalizes pathspec controls while preserving relative effective indexes and ignored dotenv files', (t) => {
  const root = repository(t);
  fs.mkdirSync(path.join(root, 'web'));
  fs.mkdirSync(path.join(root, 'subdirectory'));
  fs.writeFileSync(path.join(root, '.gitignore'), 'web/.env*\n');
  fs.writeFileSync(path.join(root, 'web/.env.local'), 'do-not-read');
  const env = gitEnvironment();
  assert.equal(spawnSync('git', ['add', '-f', '--', 'web/.env.local'], { cwd: root, env }).status, 0);
  assert.equal(spawnSync('git', ['read-tree', '--empty'], {
    cwd: root, env: { ...env, GIT_INDEX_FILE: 'effective-index' },
  }).status, 0);
  const index = fs.readFileSync(path.join(root, 'effective-index'));
  const result = nodeIn(root, `
    import assert from 'node:assert/strict';
    import { runGates } from ${JSON.stringify(runnerURL)};
    process.chdir('subdirectory');
    process.env.GIT_INDEX_FILE = '../effective-index';
    const controls = ['GIT_LITERAL_PATHSPECS', 'GIT_GLOB_PATHSPECS', 'GIT_NOGLOB_PATHSPECS', 'GIT_ICASE_PATHSPECS'];
    for (const key of controls) delete process.env[key];
    for (const key of controls) {
      process.env[key] = '1';
      const original = { ...process.env };
      for (const gate of ['web', 'browser']) {
        await assert.rejects(runGates([gate]), /Untracked Web environment files/);
        assert.deepEqual({ ...process.env }, original);
      }
      delete process.env[key];
    }
  `);
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(fs.readFileSync(path.join(root, 'effective-index')), index);
  assert.equal(fs.readFileSync(path.join(root, 'web/.env.local'), 'utf8'), 'do-not-read');
});

test('scratch symlinks outside the candidate checkout fail before callbacks without deleting the target', (t) => {
  const root = repository(t);
  const outside = fixture(t).root;
  fs.mkdirSync(path.join(root, 'tmp'));
  fs.symlinkSync(outside, path.join(root, 'tmp/agent'), 'dir');
  fs.writeFileSync(path.join(outside, 'keep'), 'untouched');
  const result = nodeIn(root, `
    import assert from 'node:assert/strict';
    import { runGates } from ${JSON.stringify(runnerURL)};
    let called = false;
    await assert.rejects(runGates(['go'], {
      beforeGate() { called = true; },
    }), /scratch tmp\\/agent must resolve inside the checkout/);
    assert.equal(called, false);
  `);
  assert.equal(result.status, 0, result.stderr);
  assert.deepEqual(fs.readdirSync(outside), ['keep']);
  assert.equal(fs.readFileSync(path.join(outside, 'keep'), 'utf8'), 'untouched');
});

test('after callbacks rejecting with non-Error values cannot turn a successful child into success', (t) => {
  const root = repository(t);
  const result = nodeIn(root, `
    import assert from 'node:assert/strict';
    import childProcess from 'node:child_process';
    import { syncBuiltinESMExports } from 'node:module';
    const realSpawn = childProcess.spawn;
    childProcess.spawn = (_command, _args, options) =>
      realSpawn(process.execPath, ['--eval', 'process.exit(0)'], options);
    syncBuiltinESMExports();
    const { runGates } = await import(${JSON.stringify(runnerURL)});
    for (const reason of [undefined, null, false, 0]) {
      const calls = [];
      await assert.rejects(runGates(['image'], {
        beforeGate(gate) { calls.push('before:' + gate); },
        afterGate(gate) { calls.push('after:' + gate); return Promise.reject(reason); },
      }));
      assert.deepEqual(calls, ['before:image', 'after:image']);
    }
  `);
  assert.equal(result.status, 0, result.stderr);
});

test('Go-dependent gates reject mismatched or unavailable local toolchains after candidate callbacks, without running a lane', (t) => {
  const root = repository(t);
  fs.writeFileSync(path.join(root, 'go.mod'), 'module fixture\n\ngo 1.26.2 // exact CI version\n');
  const result = nodeIn(root, `
    import assert from 'node:assert/strict';
    import fs from 'node:fs';
    import childProcess from 'node:child_process';
    import { syncBuiltinESMExports } from 'node:module';
    const realSpawnSync = childProcess.spawnSync;
    let version = 'go1.27.2';
    childProcess.spawnSync = (command, args, options) => {
      if (command !== 'go') return realSpawnSync(command, args, options);
      return realSpawnSync(process.execPath, ['--input-type=module', '--eval', \`
        import assert from 'node:assert/strict';
        assert.equal(process.env.GOTOOLCHAIN, 'local');
        assert.equal(process.env.GOWORK, 'off');
        assert.equal(process.env.GOENV, 'off');
        assert.equal(process.env.GOFLAGS, undefined);
        if (process.argv[1] === 'unavailable') process.exit(2);
        process.stdout.write(process.argv[1] + '\\\\n');
      \`, version], options);
    };
    childProcess.spawn = () => { throw new Error('a rejected Go toolchain must not start a lane'); };
    syncBuiltinESMExports();
    const { runGates } = await import(${JSON.stringify(runnerURL)});
    Object.assign(process.env, { GOTOOLCHAIN: 'auto', GOWORK: 'ambient.work', GOENV: 'ambient.env', GOFLAGS: '-run=^$' });
    const original = { ...process.env };
    for (const gate of ['go', 'postgres', 'business-postgres']) {
      for (version of ['go1.26.1', 'go1.27.2', 'go1.26.2rc1', 'devel go1.26.2', 'unavailable']) {
        const calls = [];
        await assert.rejects(runGates([gate], {
          beforeGate(name) { calls.push('before:' + name); },
          afterGate(name) { calls.push('after:' + name); },
        }), version === 'unavailable' ? /Cannot verify local Go version.*no automatic download/ : /Local Go version must match go.mod exactly.*go1\\.26\\.2/);
        assert.deepEqual(calls, ['before:' + gate, 'after:' + gate]);
        assert.deepEqual({ ...process.env }, original);
        assert.deepEqual(fs.readdirSync('tmp/agent'), []);
      }
    }
  `);
  assert.equal(result.status, 0, result.stderr);
});

