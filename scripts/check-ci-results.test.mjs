import test from 'node:test';
import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { assertBrowserResult, assertGoResult } from './check-ci-results.mjs';

const script = fileURLToPath(new URL('./check-ci-results.mjs', import.meta.url));
const results = ['success', 'failure', 'cancelled', 'skipped', undefined];
const invalidResults = [null, '', 'neutral', 'timed_out', 'action_required', 'stale', 'startup_failure',
  'queued', 'in_progress', 'waiting', 'pending', 'requested', 'unknown', 'SUCCESS', ' success', 'success\n',
  true, false, 0, 1, [], {}, new String('success'), new String('skipped')];
const invalidSelections = [undefined, null, '', true, false, 0, 1, '0', '1', 'TRUE', 'FALSE',
  ' true', 'false ', 'true\n', 'success', 'skipped', [], {}, new String('true'), new String('false')];
const browser = { changesResult: 'success', browserSelected: 'true', shardsResult: 'success' };
const go = {
  changesResult: 'success', goSelected: 'true', postgresSelected: 'true',
  goResult: 'success', catalogResult: 'success', businessResult: 'success',
};

function assertOutcome(run, allowed) {
  if (allowed) assert.equal(run(), undefined);
  else assert.throws(run, Error);
}

function cli(args, values = {}) {
  const env = { ...process.env };
  for (const name of ['CHANGES_RESULT', 'BROWSER_SELECTED', 'SHARDS_RESULT', 'GO_SELECTED',
    'POSTGRES_SELECTED', 'GO_RESULT', 'CATALOG_RESULT', 'BUSINESS_RESULT']) delete env[name];
  for (const [name, value] of Object.entries(values)) {
    if (value !== undefined) env[name] = value;
  }
  return spawnSync(process.execPath, [script, ...args], { env, encoding: 'utf8' });
}

function assertCliSuccess(run) {
  assert.equal(run.error, undefined);
  assert.equal(run.status, 0, run.stderr);
  assert.equal(run.stdout, '');
  assert.equal(run.stderr, '');
}

function assertCliFailure(run, message) {
  assert.equal(run.error, undefined);
  assert.equal(run.status, 1);
  assert.equal(run.stdout, '');
  assert.equal(run.stderr, `check-ci-results: ${message}\n`);
}

test('browser truth table accepts only success when selected and skipped when not selected', () => {
  for (const browserSelected of ['true', 'false']) {
    for (const shardsResult of [...results, ...invalidResults]) {
      assertOutcome(() => assertBrowserResult({ ...browser, browserSelected, shardsResult }),
        shardsResult === (browserSelected === 'true' ? 'success' : 'skipped'));
    }
  }
});

test('Go truth table independently enforces Go, catalog and business results', () => {
  for (const goSelected of ['true', 'false']) {
    for (const postgresSelected of ['true', 'false']) {
      for (const goResult of results) {
        for (const catalogResult of results) {
          for (const businessResult of results) {
            const expectedGo = goSelected === 'true' ? 'success' : 'skipped';
            const expectedBusiness = postgresSelected === 'true' ? 'success' : 'skipped';
            assertOutcome(() => assertGoResult({ ...go, goSelected, postgresSelected, goResult, catalogResult, businessResult }),
              goResult === expectedGo && catalogResult === 'success' && businessResult === expectedBusiness);
          }
        }
      }
    }
  }
});

test('each Go lane rejects invalid or non-string results for both selection values', () => {
  for (const selected of ['true', 'false']) {
    const expected = selected === 'true' ? 'success' : 'skipped';
    const valid = { ...go, goSelected: selected, postgresSelected: selected,
      goResult: expected, catalogResult: 'success', businessResult: expected };
    for (const field of ['goResult', 'catalogResult', 'businessResult']) {
      for (const result of invalidResults) {
        assert.throws(() => assertGoResult({ ...valid, [field]: result }), Error);
      }
    }
  }
});

test('catalog must succeed in both PostgreSQL selection modes, never fail, cancel, skip or disappear', () => {
  for (const postgresSelected of ['true', 'false']) {
    const valid = { ...go, postgresSelected,
      businessResult: postgresSelected === 'true' ? 'success' : 'skipped' };
    assert.equal(assertGoResult(valid), undefined);
    for (const catalogResult of ['failure', 'cancelled', 'skipped', undefined]) {
      assert.throws(() => assertGoResult({ ...valid, catalogResult }), {
        message: 'record-platform-pg16-catalog must be success',
      });
    }
  }
});

test('changes must be exactly success even when no heavy lanes are selected', () => {
  for (const changesResult of [...results, ...invalidResults]) {
    for (const selected of ['true', 'false']) {
      const expected = selected === 'true' ? 'success' : 'skipped';
      assertOutcome(() => assertBrowserResult({ changesResult, browserSelected: selected, shardsResult: expected }),
        changesResult === 'success');
      assertOutcome(() => assertGoResult({ changesResult, goSelected: selected, postgresSelected: selected,
        goResult: expected, catalogResult: 'success', businessResult: expected }), changesResult === 'success');
    }
  }
});

test('selection values must be exact strings, never missing, coerced or trimmed', () => {
  for (const selection of invalidSelections) {
    assert.throws(() => assertBrowserResult({ ...browser, browserSelected: selection }), {
      message: 'browserSelected must be the string "true" or "false"',
    });
    assert.throws(() => assertGoResult({ ...go, goSelected: selection }), {
      message: 'goSelected must be the string "true" or "false"',
    });
    assert.throws(() => assertGoResult({ ...go, postgresSelected: selection }), {
      message: 'postgresSelected must be the string "true" or "false"',
    });
  }
  assert.throws(() => assertBrowserResult(), { message: 'changes must succeed' });
  assert.throws(() => assertGoResult(), { message: 'changes must succeed' });
  assert.throws(() => assertBrowserResult({}), { message: 'changes must succeed' });
  assert.throws(() => assertGoResult({}), { message: 'changes must succeed' });
});

test('errors name the failing lane and required result without echoing supplied values', () => {
  for (const selected of ['true', 'false']) {
    const expected = selected === 'true' ? 'success' : 'skipped';
    const suffix = `${expected} when ${selected === 'true' ? 'selected' : 'not selected'}`;
    assert.throws(() => assertBrowserResult({ ...browser, browserSelected: selected, shardsResult: 'private-value' }), {
      message: `web-browser-shard must be ${suffix}`,
    });
    const valid = { ...go, goSelected: selected, postgresSelected: selected,
      goResult: expected, catalogResult: 'success', businessResult: expected };
    assert.throws(() => assertGoResult({ ...valid, catalogResult: 'private-value' }), {
      message: 'record-platform-pg16-catalog must be success',
    });
    for (const [field, name] of [['goResult', 'go-check'], ['businessResult', 'business-postgres']]) {
      assert.throws(() => assertGoResult({ ...valid, [field]: 'private-value' }), {
        message: `${name} must be ${suffix}`,
      });
    }
  }
});

test('browser CLI reads fixed environment keys and permits only its selected result', () => {
  for (const BROWSER_SELECTED of ['true', 'false']) {
    const env = { CHANGES_RESULT: 'success', BROWSER_SELECTED,
      SHARDS_RESULT: BROWSER_SELECTED === 'true' ? 'success' : 'skipped' };
    assertCliSuccess(cli(['browser'], env));
    assertCliFailure(cli(['browser'], { ...env, SHARDS_RESULT: 'cancelled' }),
      `web-browser-shard must be ${BROWSER_SELECTED === 'true' ? 'success when selected' : 'skipped when not selected'}`);
  }
  assertCliFailure(cli(['browser'], { CHANGES_RESULT: 'success', browserSelected: 'true', shardsResult: 'success' }),
    'browserSelected must be the string "true" or "false"');
  assertCliFailure(cli(['browser'], { CHANGES_RESULT: 'failure', BROWSER_SELECTED: 'false', SHARDS_RESULT: 'skipped' }),
    'changes must succeed');
});

test('Go CLI requires catalog success in both modes and business success or skipped according to selection', () => {
  for (const GO_SELECTED of ['true', 'false']) {
    for (const POSTGRES_SELECTED of ['true', 'false']) {
      const env = {
        CHANGES_RESULT: 'success', GO_SELECTED, POSTGRES_SELECTED,
        GO_RESULT: GO_SELECTED === 'true' ? 'success' : 'skipped',
        CATALOG_RESULT: 'success',
        BUSINESS_RESULT: POSTGRES_SELECTED === 'true' ? 'success' : 'skipped',
      };
      assertCliSuccess(cli(['go'], env));
      for (const CATALOG_RESULT of ['failure', 'cancelled', 'skipped', undefined]) {
        assertCliFailure(cli(['go'], { ...env, CATALOG_RESULT }),
          'record-platform-pg16-catalog must be success');
      }
      for (const BUSINESS_RESULT of results.filter(result => result !== env.BUSINESS_RESULT)) {
        assertCliFailure(cli(['go'], { ...env, BUSINESS_RESULT }),
          `business-postgres must be ${POSTGRES_SELECTED === 'true' ? 'success when selected' : 'skipped when not selected'}`);
      }
    }
  }
  const env = { CHANGES_RESULT: 'success', GO_SELECTED: 'true', POSTGRES_SELECTED: 'true',
    GO_RESULT: 'success', CATALOG_RESULT: 'success', BUSINESS_RESULT: 'success' };
  for (const [key, name] of [['GO_RESULT', 'go-check'], ['BUSINESS_RESULT', 'business-postgres']]) {
    for (const result of ['failure', undefined]) {
      assertCliFailure(cli(['go'], { ...env, [key]: result }), `${name} must be success when selected`);
    }
  }
  for (const [key, name] of [['GO_SELECTED', 'goSelected'], ['POSTGRES_SELECTED', 'postgresSelected']]) {
    assertCliFailure(cli(['go'], { ...env, [key]: undefined }), `${name} must be the string "true" or "false"`);
  }
});

test('CLI fails closed for missing changes and invalid arguments', () => {
  for (const mode of ['browser', 'go']) assertCliFailure(cli([mode]), 'changes must succeed');
  for (const args of [[], ['unknown'], ['--help'], ['browser', 'go'], ['go', '--extra']]) {
    assertCliFailure(cli(args), 'Usage: node scripts/check-ci-results.mjs browser|go');
  }
});

test('importing aggregators does not execute the CLI or inspect environment', () => {
  const source = `import { assertBrowserResult, assertGoResult } from ${JSON.stringify(new URL('./check-ci-results.mjs', import.meta.url).href)};
    if (typeof assertBrowserResult !== 'function' || typeof assertGoResult !== 'function') process.exitCode = 2;`;
  const run = spawnSync(process.execPath, ['--input-type=module', '--eval', source], {
    env: { ...process.env, CHANGES_RESULT: 'failure' }, encoding: 'utf8',
  });
  assertCliSuccess(run);
});
