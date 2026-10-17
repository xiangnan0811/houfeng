#!/usr/bin/env node
import path from 'node:path';
import { fileURLToPath } from 'node:url';

function assertChangesResult(changesResult) {
  if (changesResult !== 'success') throw new Error('changes must succeed');
}

function assertSelection(name, value) {
  if (value !== 'true' && value !== 'false') {
    throw new Error(`${name} must be the string "true" or "false"`);
  }
}

function assertLaneResult(name, selected, result) {
  const expected = selected === 'true' ? 'success' : 'skipped';
  if (result !== expected) {
    throw new Error(`${name} must be ${expected} when ${selected === 'true' ? 'selected' : 'not selected'}`);
  }
}

export function assertBrowserResult({ changesResult, browserSelected, shardsResult } = {}) {
  assertChangesResult(changesResult);
  assertSelection('browserSelected', browserSelected);
  assertLaneResult('web-browser-shard', browserSelected, shardsResult);
}

export function assertGoResult({ changesResult, goSelected, postgresSelected, goResult, catalogResult, businessResult } = {}) {
  assertChangesResult(changesResult);
  assertSelection('goSelected', goSelected);
  assertSelection('postgresSelected', postgresSelected);
  assertLaneResult('go-check', goSelected, goResult);
  assertLaneResult('record-platform-pg16-catalog', postgresSelected, catalogResult);
  assertLaneResult('business-postgres', postgresSelected, businessResult);
}

function main(args) {
  if (args.length !== 1 || !['browser', 'go'].includes(args[0])) {
    throw new Error('Usage: node scripts/check-ci-results.mjs browser|go');
  }
  if (args[0] === 'browser') {
    assertBrowserResult({
      changesResult: process.env.CHANGES_RESULT,
      browserSelected: process.env.BROWSER_SELECTED,
      shardsResult: process.env.SHARDS_RESULT,
    });
  } else {
    assertGoResult({
      changesResult: process.env.CHANGES_RESULT,
      goSelected: process.env.GO_SELECTED,
      postgresSelected: process.env.POSTGRES_SELECTED,
      goResult: process.env.GO_RESULT,
      catalogResult: process.env.CATALOG_RESULT,
      businessResult: process.env.BUSINESS_RESULT,
    });
  }
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    main(process.argv.slice(2));
  } catch (error) {
    console.error(`check-ci-results: ${error.message}`);
    process.exitCode = 1;
  }
}
