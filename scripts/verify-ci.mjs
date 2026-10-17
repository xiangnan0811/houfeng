#!/usr/bin/env node
import fs from 'node:fs';
import path from 'node:path';
import { spawn, spawnSync } from 'node:child_process';
import { performance } from 'node:perf_hooks';
import { fileURLToPath } from 'node:url';

const GATES = ['docs', 'go', 'postgres', 'business-postgres', 'web', 'browser', 'image'];
const POSTGRES_IMAGES = ['postgres:16.0', 'postgres:16.6', 'postgres:16.12'];
const BROWSER_TIMEOUT_MS = 20 * 60 * 1000;
// Give strict database runners time to finish their owner-scoped cleanup traps.
const TERMINATION_GRACE_MS = 30_000;

export function validateReleaseManifest(root) {
  const file = path.join(root, '.release-please-manifest.json');
  let manifest;
  try {
    manifest = JSON.parse(fs.readFileSync(file, 'utf8'));
  } catch (error) {
    throw new Error(`Invalid .release-please-manifest.json: ${error.message}`, { cause: error });
  }
  if (manifest === null || typeof manifest !== 'object' || Array.isArray(manifest) ||
      Object.keys(manifest).length !== 1 || !Object.hasOwn(manifest, '.')) {
    throw new Error('.release-please-manifest.json must be an object with only the "." package key');
  }
  const version = manifest['.'];
  const stableVersion = /^(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)$/;
  // Compare the entire match because JavaScript's $ also permits a final newline.
  if (typeof version !== 'string' || stableVersion.exec(version)?.[0] !== version) {
    throw new Error('.release-please-manifest.json "." must be a stable three-part nonnegative integer version without a v prefix');
  }
  return manifest;
}

function validateArguments(gates, options) {
  if (!Array.isArray(gates) || gates.length === 0 ||
      gates.some((gate) => !GATES.includes(gate)) || new Set(gates).size !== gates.length) {
    throw new Error(`gates must be a nonempty list of distinct names: ${GATES.join(',')}`);
  }
  if (options === null || typeof options !== 'object' || Array.isArray(options)) {
    throw new Error('gate options must be an object');
  }
  const allowed = ['beforeGate', 'afterGate', 'postgresImage', 'shard'];
  if (Object.keys(options).some((key) => !allowed.includes(key))) {
    throw new Error(`unknown gate option; allowed options: ${allowed.join(',')}`);
  }
  for (const name of ['beforeGate', 'afterGate']) {
    if (options[name] !== undefined && typeof options[name] !== 'function') {
      throw new Error(`${name} must be a function`);
    }
  }
  if (options.postgresImage !== undefined &&
      (!POSTGRES_IMAGES.includes(options.postgresImage) || gates.length !== 1 || gates[0] !== 'postgres')) {
    throw new Error('--postgres-image requires only the postgres gate and postgres:16.0, postgres:16.6 or postgres:16.12');
  }
  if (options.shard !== undefined &&
      (!['1/2', '2/2'].includes(options.shard) || gates.length !== 1 || gates[0] !== 'browser')) {
    throw new Error('--shard requires only the browser gate and 1/2 or 2/2');
  }
}

function gitQuery(args, cwd, env) {
  const result = spawnSync('git', args, { cwd, env, encoding: 'utf8', maxBuffer: 16 * 1024 * 1024 });
  // Do not echo stderr: Git config and ambient input may contain credentials.
  if (result.error || result.status !== 0) {
    throw new Error(`Git query failed: git ${args[0]} (${result.error?.code ?? result.status ?? result.signal})`);
  }
  return result.stdout;
}

function gateEnvironment(source, gitLocalVariables, scratch) {
  const env = { ...source };
  const gitVariables = new Set([...gitLocalVariables, 'GIT_CONFIG_COUNT', 'GIT_CONFIG_PARAMETERS']);
  const overrides = new Set([
    'DATABASE_URL', 'NODE_OPTIONS', 'NODE_PATH', 'NODE_ENV',
    'GOFLAGS', 'GOENV', 'GOWORK', 'GOTOOLCHAIN', 'GOOS', 'GOARCH', 'GOAMD64',
    'GOARM', 'GOARM64', 'GO386', 'GOMIPS', 'GOMIPS64', 'GOPPC64', 'GORISCV64',
    'GOEXPERIMENT', 'GODEBUG', 'GO111MODULE', 'GOCOVERDIR', 'GOTMPDIR',
    'CGO_ENABLED', 'CGO_CFLAGS', 'CGO_CPPFLAGS', 'CGO_CXXFLAGS', 'CGO_FFLAGS', 'CGO_LDFLAGS',
    'GO', 'NPM', 'GO_VERIFY_PATTERNS', 'MAKEFLAGS', 'MFLAGS', 'MAKELEVEL',
    'MAKEOVERRIDES', 'GNUMAKEFLAGS', 'SHELL', 'BASH_ENV', 'ENV', 'CDPATH',
    'PWDEBUG', 'DEBUG',
  ]);
  // Retain caches and network configuration, not npm's test/install selectors.
  const npmNetworkOrCache = /^(?:cache|registry|proxy|https_proxy|noproxy|cafile|strict_ssl)$/i;
  for (const key of Object.keys(env)) {
    if (gitVariables.has(key) || overrides.has(key) ||
        /^(?:HOUFENG_|PG|VITE_|GIT_CONFIG_(?:KEY|VALUE)_)/.test(key) ||
        (key.startsWith('PLAYWRIGHT_') && key !== 'PLAYWRIGHT_BROWSERS_PATH') ||
        (/^npm_config_/i.test(key) && !npmNetworkOrCache.test(key.slice('npm_config_'.length)))) {
      delete env[key];
    }
  }
  env.GOTOOLCHAIN = 'local';
  env.GOWORK = 'off';
  env.GOENV = 'off';
  env.TMPDIR = scratch;
  env.TMP = scratch;
  env.GOTMPDIR = scratch;
  env.TEMP = scratch;
  // Keep Playwright's local/Actions worker counts; focus rejection is explicit.
  if (source.GITHUB_ACTIONS === 'true') env.CI = 'true';
  else delete env.CI;
  return env;
}

function createScratch(root) {
  const base = path.join(root, 'tmp/agent');
  fs.mkdirSync(base, { recursive: true });
  const realBase = fs.realpathSync(base);
  const relative = path.relative(root, realBase);
  if (!relative || relative === '..' || relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) {
    throw new Error('Verification scratch tmp/agent must resolve inside the checkout');
  }
  return fs.mkdtempSync(path.join(realBase, 'verify-ci-'));
}

function rejectPrivateWebEnvironment(cwd, env) {
  // No --exclude-standard: ignored private dotenv files are unsafe inputs too.
  // Preserve Git's effective index and original cwd for relative indexes, but
  // do not let ambient pathspec controls disable the explicit top-level query.
  const queryEnv = { ...env };
  for (const key of ['GIT_LITERAL_PATHSPECS', 'GIT_GLOB_PATHSPECS', 'GIT_NOGLOB_PATHSPECS', 'GIT_ICASE_PATHSPECS']) {
    delete queryEnv[key];
  }
  const files = gitQuery(['ls-files', '--full-name', '--others', '-z', '--', ':(top)web'], cwd, queryEnv)
    .split('\0').filter((file) => file === 'web/.env' || file === 'web/.env.local' ||
      file.startsWith('web/.env.test') || file.startsWith('web/.env.production'));
  if (files.length) {
    throw new Error(`Untracked Web environment files would change the verification candidate; move them outside web/:\n${files.map((file) => JSON.stringify(file)).join('\n')}`);
  }
}

function requireLocalGoVersion(root, env) {
  const module = fs.readFileSync(path.join(root, 'go.mod'), 'utf8');
  const directives = [...module.matchAll(/^go[ \t]+([0-9]+\.[0-9]+(?:\.[0-9]+)?)[ \t]*(?:\/\/[^\r\n]*)?\r?$/gm)];
  if (directives.length !== 1) {
    throw new Error('go.mod must contain exactly one stable Go version directive for CI parity');
  }
  const required = directives[0][1];
  const expected = `go${required.split('.').length === 2 ? `${required}.0` : required}`;
  const result = spawnSync('go', ['env', 'GOVERSION'], {
    cwd: root, env, encoding: 'utf8', timeout: 30_000, maxBuffer: 1024 * 1024,
  });
  // Query the actual local toolchain with the same normalized gate environment.
  // Do not print subprocess stderr or allow Go's automatic toolchain download.
  if (result.error || result.status !== 0) {
    throw new Error(`Cannot verify local Go version required by go.mod (${required}); install/select it on PATH (GOTOOLCHAIN=local; no automatic download)`);
  }
  const actual = result.stdout.trim();
  if (actual !== expected) {
    throw new Error(`Local Go version must match go.mod exactly: required ${expected}, found ${JSON.stringify(actual)}; select the required Go on PATH (GOTOOLCHAIN=local; no automatic download)`);
  }
}

function signalGroup(child, signal) {
  if (!child.pid) return false;
  try {
    process.kill(-child.pid, signal);
    return true;
  } catch (error) {
    if (error.code === 'ESRCH') return false;
    throw error;
  }
}

async function runCommand(command, args, { root, env, signal, timeoutMs }) {
  signal.throwIfAborted();
  await new Promise((resolve, reject) => {
    // Each command owns a POSIX process group, including npm's browser server.
    const child = spawn(command, args, { cwd: root, env, stdio: 'inherit', detached: true });
    let timer;
    let killTimer;
    let failure;
    let finished = false;
    const terminate = () => {
      if (signalGroup(child, 'SIGTERM') && !killTimer) {
        killTimer = setTimeout(() => signalGroup(child, 'SIGKILL'), TERMINATION_GRACE_MS);
      }
    };
    const abort = () => {
      failure ??= signal.reason ?? new Error('Verification interrupted');
      terminate();
    };
    signal.addEventListener('abort', abort, { once: true });
    if (timeoutMs !== undefined) {
      timer = setTimeout(() => {
        failure ??= new Error(`${command} exceeded its ${Math.ceil(timeoutMs / 1000)}s timeout`);
        terminate();
      }, timeoutMs);
    }
    const finish = async (error) => {
      if (finished) return;
      finished = true;
      clearTimeout(timer);
      signal.removeEventListener('abort', abort);
      // Even a successful leader must not leave an owned preview/server behind.
      if (signalGroup(child, 'SIGTERM')) {
        await new Promise((done) => {
          clearTimeout(killTimer);
          killTimer = setTimeout(() => {
            signalGroup(child, 'SIGKILL');
            done();
          }, TERMINATION_GRACE_MS);
        });
      }
      clearTimeout(killTimer);
      if (failure || error) reject(failure ?? error);
      else resolve();
    };
    child.once('error', (error) => {
      void finish(new Error(`Cannot start ${command}: ${error.code ?? 'spawn error'}`));
    });
    child.once('close', (code, exitSignal) => {
      void finish(code === 0 ? undefined : new Error(`${command} failed (${exitSignal ?? `exit ${code}`})`));
    });
    // An abort could have arrived synchronously while installing the listener.
    if (signal.aborted) abort();
  });
}

async function executeGate(gate, context, { postgresImage, shard, webBuilt }) {
  const command = (name, args, extra = {}) => runCommand(name, args, { ...context, ...extra });
  switch (gate) {
    case 'docs':
      await command('make', ['test-ci-tools']);
      await command('make', ['verify-docs']);
      validateReleaseManifest(context.root);
      break;
    case 'go':
      await command('make', ['verify-go']);
      break;
    case 'postgres':
      for (const image of postgresImage ? [postgresImage] : POSTGRES_IMAGES) {
        await command('bash', ['scripts/test-pg16-catalog.sh', image]);
      }
      break;
    case 'business-postgres':
      await command('bash', ['scripts/test-business-postgres.sh']);
      break;
    case 'web':
      // Vitest rejects focused tests in CI mode, including automatic local gates.
      // This environment is lane-local so browser worker selection stays intact.
      await command('make', ['verify-web'], { env: { ...context.env, NODE_ENV: 'production', CI: 'true' } });
      break;
    case 'browser': {
      const deadline = performance.now() + BROWSER_TIMEOUT_MS;
      const browserCommand = (name, args, timeoutMs) => {
        const remaining = deadline - performance.now();
        if (remaining <= 0) throw new Error('browser exceeded its 20 minute lane timeout');
        return command(name, args, { timeoutMs: Math.min(remaining, timeoutMs ?? remaining) });
      };
      if (!webBuilt) {
        await browserCommand('env', ['-u', 'NODE_ENV', 'npm', '--prefix', 'web', 'ci', '--include=dev']);
      }
      if (context.env.GITHUB_ACTIONS === 'true') {
        try {
          await browserCommand('timeout', ['240s', 'npm', '--prefix', 'web', 'exec', 'playwright', 'install-deps', '--', 'chromium'], 245_000);
        } catch (error) {
          context.signal.throwIfAborted();
          if (performance.now() >= deadline) throw error;
          console.error(`[verify-ci] Chromium host preparation failed: ${error.message}; continuing to the real browser gate`);
        }
      }
      await browserCommand('npm', ['--prefix', 'web', 'exec', 'playwright', 'install', '--', 'chromium'], 5 * 60 * 1000);
      const shardArgs = shard ? [`--shard=${shard}`] : [];
      if (webBuilt) {
        // npm exec's --prefix finds the binary but keeps cwd at the repository root.
        // Pin the primary config instead of discovering unrelated Vitest files.
        await browserCommand('npm', ['--prefix', 'web', 'exec', '--', 'playwright', 'test', '--config=web/playwright.config.ts', '--forbid-only', ...shardArgs]);
      } else {
        await browserCommand('npm', ['--prefix', 'web', 'run', 'test:e2e', '--', '--forbid-only', ...shardArgs]);
      }
      break;
    }
    case 'image':
      await command('docker', ['buildx', 'build', '--file', 'Dockerfile', '--build-arg', 'VERSION=dev', '--output=type=cacheonly', '.']);
      break;
  }
}

// CLI and import callers share all gate/option validation here. Success is never
// cached across calls; webBuilt only describes this invocation's verified build.
export async function runGates(gates = GATES, options = {}) {
  validateArguments(gates, options);
  const { beforeGate, afterGate, postgresImage, shard } = options;
  const selected = new Set(gates);
  const cwd = process.cwd();
  const gitEnvironment = { ...process.env };
  const root = fs.realpathSync(gitQuery(['rev-parse', '--show-toplevel'], cwd, gitEnvironment).trimEnd());
  const gitVariables = gitQuery(['rev-parse', '--local-env-vars'], cwd, gitEnvironment).trim().split('\n');
  const scratch = createScratch(root);
  const env = gateEnvironment(gitEnvironment, gitVariables, scratch);
  const controller = new AbortController();
  const onInterrupt = () => controller.abort(new Error('Verification interrupted by SIGINT'));
  const onTerminate = () => controller.abort(new Error('Verification interrupted by SIGTERM'));
  process.on('SIGINT', onInterrupt);
  process.on('SIGTERM', onTerminate);
  let webBuilt = false;
  let goVersionChecked = false;
  try {
    for (const gate of GATES) {
      if (!selected.has(gate)) {
        console.log(`[verify-ci] ${gate}: skipped (not selected for this change)`);
        continue;
      }
      controller.signal.throwIfAborted();
      const started = performance.now();
      console.log(`[verify-ci] ${gate}: starting`);
      try {
        await beforeGate?.(gate);
        controller.signal.throwIfAborted();
        let gateError;
        let gateFailed = false;
        try {
          if (!goVersionChecked && ['go', 'postgres', 'business-postgres'].includes(gate)) {
            requireLocalGoVersion(root, env);
            goVersionChecked = true;
          }
          if (gate === 'web' || gate === 'browser') rejectPrivateWebEnvironment(cwd, gitEnvironment);
          await executeGate(gate, { root, env, signal: controller.signal }, { postgresImage, shard, webBuilt });
        } catch (error) {
          gateError = error;
          gateFailed = true;
        }
        try {
          await afterGate?.(gate);
        } catch (error) {
          gateError = gateFailed
            ? new AggregateError([gateError, error], `${gateError?.message ?? String(gateError)}; candidate recheck failed: ${error?.message ?? String(error)}`)
            : error;
          gateFailed = true;
        }
        if (gateFailed) throw gateError;
        controller.signal.throwIfAborted();
        if (gate === 'web') webBuilt = true;
        console.log(`[verify-ci] ${gate}: passed (${((performance.now() - started) / 1000).toFixed(1)}s)`);
      } catch (error) {
        console.error(`[verify-ci] ${gate}: failed (${((performance.now() - started) / 1000).toFixed(1)}s)`);
        throw error;
      }
    }
  } finally {
    process.removeListener('SIGINT', onInterrupt);
    process.removeListener('SIGTERM', onTerminate);
    fs.rmSync(scratch, { recursive: true, force: true });
  }
}

function parseCLI(args) {
  let gates;
  const options = {};
  const seen = new Set();
  for (let index = 0; index < args.length; index += 2) {
    const flag = args[index];
    if (!['--gates', '--postgres-image', '--shard'].includes(flag) || seen.has(flag)) {
      throw new Error(`Unknown or duplicate argument: ${flag}`);
    }
    seen.add(flag);
    const value = args[index + 1];
    if (value === undefined || value.startsWith('--')) throw new Error(`Missing value for ${flag}`);
    if (flag === '--gates') gates = value.split(',');
    else options[flag === '--postgres-image' ? 'postgresImage' : 'shard'] = value;
  }
  return { gates, options };
}

function isDirectInvocation() {
  if (!process.argv[1]) return false;
  try {
    return fs.realpathSync(path.resolve(process.argv[1])) === fs.realpathSync(fileURLToPath(import.meta.url));
  } catch (error) {
    if (error.code === 'ENOENT' || error.code === 'ENOTDIR') return false;
    throw error;
  }
}

// Importing this module (including from candidate checks and Node selftests)
// must never start gates. Resolve symlinks for direct CLI invocation as well.
if (isDirectInvocation()) {
  try {
    const { gates, options } = parseCLI(process.argv.slice(2));
    await runGates(gates, options);
  } catch (error) {
    console.error(`[verify-ci] ${error?.message ?? String(error)}`);
    process.exitCode = 1;
  }
}
