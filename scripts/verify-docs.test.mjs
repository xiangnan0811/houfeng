import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { anchors, auditRepository, links } from './verify-docs.mjs';

function fixture(t, files = {}) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'houfeng-docs-'));
  t.after(() => fs.rmSync(root, { recursive: true, force: true }));
  for (const [name, text] of Object.entries({ 'docs/README.md': '# 文档\n[指南](guide.md)\n', 'docs/guide.md': '# 指南\n', ...files })) {
    const target = path.join(root, name);
    fs.mkdirSync(path.dirname(target), { recursive: true });
    fs.writeFileSync(target, text);
  }
  return root;
}

test('valid local links, images, outside-docs links, resources and platform imports', (t) => {
  const root = fixture(t, {
    'docs/README.md': '# 文档\n[指南](guide.md)\n[部署](deploy/)\n![图](image.svg)\n',
    'docs/guide.md': '# 指南\n[开发](../CONTRIBUTING.md#开发)\n[查询](guide.md?view=raw#指南)\n',
    'docs/deploy/README.md': '# 部署\n[单元](systemd/agent.service)\n',
    'docs/deploy/systemd/agent.service': '[Service]\n',
    'docs/image.svg': '<svg/>',
    'CONTRIBUTING.md': '# 开发\n[文档](docs/)\n',
    'CLAUDE.md': '@docs/README.md\n',
  });
  assert.deepEqual(auditRepository(root), []);
});

test('Chinese, duplicate and setext anchors use stable slug suffixes', () => {
  assert.deepEqual([...anchors('# 中文标题\n## 中文标题\n## 中文标题-1\n## `Code` & API\n下划线\n---\n<a id="explicit"></a>')],
    ['中文标题', '中文标题-1', '中文标题-1-1', 'code--api', '下划线', 'explicit']);
});

test('code identifiers keep underscores in heading fragments', () => {
  assert.ok(anchors('## `network_rates_valid`').has('network_rates_valid'));
  assert.ok(anchors('## _Emphasized_ title').has('emphasized-title'));
});

test('references support full, collapsed, shortcut and images', () => {
  assert.deepEqual(links('[full][target]\n[collapse][]\n[shortcut]\n![image][target]\n[target]: guide.md "Title"\n[collapse]: <guide.md#指南>\n[shortcut]: guide.md\n'),
    [{ target: 'guide.md' }, { target: 'guide.md#指南' }, { target: 'guide.md' }, { target: 'guide.md' }]);
});

test('balanced parentheses and angle destinations', () => {
  assert.deepEqual(links('[a](guide(foo).md) [b](<with space.md> "Title")'), [{ target: 'guide(foo).md' }, { target: 'with space.md' }]);
});

test('fenced, inline and indented code and comments do not create Markdown links', (t) => {
  const root = fixture(t, { 'docs/guide.md': '# 指南\n`[x](missing.md)`\n``[x](missing.md)``\n```md\n[x](missing.md)\n```\n~~~~\n[x](missing.md)\n~~~~\n    [x](missing.md)\n<!-- [x](missing.md) -->\n' });
  assert.deepEqual(auditRepository(root), []);
});

test('missing target, missing anchor, missing reference, bad import are rejected', (t) => {
  const root = fixture(t, { 'docs/guide.md': '# 指南\n[x](absent.md)\n[x](#不存在)\n[x][undefined]\n', 'CLAUDE.md': '@docs/absent.md\n' });
  const errors = auditRepository(root).join('\n');
  assert.match(errors, /missing target absent\.md/);
  assert.match(errors, /missing anchor #不存在/);
  assert.match(errors, /undefined reference \[undefined\]/);
  assert.match(errors, /CLAUDE\.md: missing target docs\/absent\.md/);
});

test('encoded Chinese fragments and duplicate fragments resolve', (t) => {
  const root = fixture(t, { 'docs/guide.md': '# 指南\n## 中文\n## 中文\n[a](#%E4%B8%AD%E6%96%87) [b](#中文-1)\n' });
  assert.deepEqual(auditRepository(root), []);
});

test('unindexed resources, missing child indexes and unreachable Markdown fail', (t) => {
  const root = fixture(t, { 'docs/orphan.md': '# 孤立\n', 'docs/resource.json': '{}', 'docs/nested/guide.md': '# 子目录\n' });
  const errors = auditRepository(root).join('\n');
  assert.match(errors, /index does not link docs\/resource.json/);
  assert.match(errors, /docs\/nested: missing README.md index/);
  assert.match(errors, /docs\/orphan.md: unreachable/);
});

test('removed paths in inline examples and runtime strings fail; docs/spec is valid', (t) => {
  const root = fixture(t, {
    'docs/guide.md': '# 指南\n`spec/backend/quality-guidelines.md`\n',
    'consumer.go': 'var old = "docs/design/current/README.md"\nvar current = "docs/spec/backend/README.md"\n',
    'old-ui.sh': 'cat docs/operations/ui-preview-and-browser-sanity.md\n',
  });
  const errors = auditRepository(root);
  assert.equal(errors.filter((error) => error.includes('removed documentation path')).length, 3);
});

test('removed directories fail even when empty', (t) => {
  const root = fixture(t);
  fs.mkdirSync(path.join(root, 'spec'));
  assert.match(auditRepository(root).join('\n'), /spec: removed documentation directory still exists/);
});

test('valid relative spec links and reference definitions are not root-path remnants', (t) => {
  const root = fixture(t, {
    'docs/README.md': '# 文档\n[指南](guide.md)\n[规范](spec/README.md)\n[ref][spec]\n[spec]: spec/README.md\n',
    'docs/spec/README.md': '# 规范\n',
  });
  assert.deepEqual(auditRepository(root), []);
});

test('root-absolute old spec references and root-path prose still fail', (t) => {
  const root = fixture(t, {
    'docs/README.md': '# 文档\n[指南](guide.md)\n[规范](spec/README.md)\n`spec/README.md`\n',
    'docs/spec/README.md': '# 规范\n',
    'consumer.go': 'var old = "/spec/backend/README.md"\n',
  });
  const errors = auditRepository(root);
  assert.equal(errors.filter((error) => error.includes('removed documentation path')).length, 2);
});

test('code examples containing old Markdown destinations are still scanned for retired paths', (t) => {
  const root = fixture(t, {
    'docs/README.md': '# 文档\n[指南](guide.md)\n[规范](spec/README.md)\n```md\n[旧示例](spec/README.md)\n```\n',
    'docs/spec/README.md': '# 规范\n',
  });
  assert.equal(auditRepository(root).filter((error) => error.includes('removed documentation path')).length, 1);
});

test('escaped parentheses in local destinations resolve', (t) => {
  const root = fixture(t, {
    'docs/README.md': '# 文档\n[指南](guide.md)\n[括号](guide\\(foo\\).md)\n',
    'docs/guide(foo).md': '# 括号\n',
  });
  assert.deepEqual(auditRepository(root), []);
});

test('links to documentation directories require a README even in resource-only dirs', (t) => {
  const root = fixture(t, {
    'docs/README.md': '# 文档\n[指南](guide.md)\n[资源](assets/)\n[文件](assets/data.json)\n',
    'docs/assets/data.json': '{}',
  });
  assert.match(auditRepository(root).join('\n'), /documentation directory lacks README.md: assets\//);
});

test('external URLs are ignored, repository escape and malformed URLs fail', (t) => {
  const root = fixture(t, { 'docs/guide.md': '# 指南\n[web](https://example.test/404)\n[email](mailto:x@example.test)\n[x](../../escape.md)\n[x](bad%ZZ.md)\n' });
  const errors = auditRepository(root);
  assert.equal(errors.length, 2);
  assert.match(errors.join('\n'), /link escapes repository/);
  assert.match(errors.join('\n'), /malformed URL/);
});

test('nested list links and multiline captions are checked, while nested code stays excluded', (t) => {
  const root = fixture(t, {
    'docs/guide.md': '# 指南\n- parent\n    - [broken](missing-nested.md)\n\n[caption\ncontinued](missing-multiline.md)\n\n- item\n\n      [code](missing-indented-code.md)\n\n  ```md\n  [code](missing-fenced-code.md)\n  ```\n\n    - nested\n      ~~~md\n      [code](missing-nested-fence.md)\n      ~~~\n\n[valid\ncaption](#指南)\n',
  });
  const errors = auditRepository(root);
  assert.equal(errors.length, 2, errors.join('\n'));
  assert.match(errors.join('\n'), /missing target missing-nested.md/);
  assert.match(errors.join('\n'), /missing target missing-multiline.md/);
});

test('nested ordered lists retain continuation links and ignore fenced examples', () => {
  assert.deepEqual(links('1. parent\n   1. [valid](guide.md)\n      [continued](guide.md#指南)\n\n      ```md\n      [code](missing.md)\n      ```\n'),
    [{ target: 'guide.md' }, { target: 'guide.md#指南' }]);
});

test('Tab-indented nested list links remain visible but standalone Tab code is excluded', () => {
  assert.deepEqual(links('- parent\n\t- [nested](guide.md)\n'), [{ target: 'guide.md' }]);
  assert.deepEqual(links('\t[example](missing.md)\n'), []);
});

test('completed proposal references and file are rejected', (t) => {
  const root = fixture(t, {
    'docs/guide.md': '# 指南\n`docs/design/proposals/monitoring-instance-detail-redesign.md`\n',
    'docs/design/proposals/monitoring-instance-detail-redesign.md': '# 已完成\n',
  });
  const errors = auditRepository(root).join('\n');
  assert.match(errors, /removed documentation path .*monitoring-instance-detail-redesign\.md/);
  assert.match(errors, /monitoring-instance-detail-redesign\.md: completed proposal still exists/);
});

test('new unfinished proposals with README navigation remain allowed', (t) => {
  const root = fixture(t, {
    'docs/README.md': '# 文档\n[指南](guide.md)\n[设计](design/README.md)\n',
    'docs/design/README.md': '# 设计\n[提案](proposals/README.md)\n',
    'docs/design/proposals/README.md': '# 提案\n[未完成](new-feature.md)\n',
    'docs/design/proposals/new-feature.md': '# 新提案\n尚未完成，不属于现行合同。\n',
  });
  assert.deepEqual(auditRepository(root), []);
});

test('root-absolute retired documentation prose paths cannot bypass the scan', (t) => {
  const root = fixture(t, {
    'docs/guide.md': '# 指南\n`/docs/design/proposals/monitoring-instance-detail-redesign.md`\n`/docs/design/current/README.md`\n`/docs/operations/ui-preview-and-browser-sanity.md`\n',
  });
  const errors = auditRepository(root);
  assert.equal(errors.filter((error) => error.includes('removed documentation path')).length, 3, errors.join('\n'));
});

test('relative prose resolving to the exact completed proposal is rejected', (t) => {
  const root = fixture(t, {
    'docs/README.md': '# 文档\n[指南](guide.md)\n[设计](design/README.md)\n',
    'docs/design/README.md': '# 设计\n`proposals/monitoring-instance-detail-redesign.md`\n',
  });
  const errors = auditRepository(root);
  assert.equal(errors.length, 1, errors.join('\n'));
  assert.match(errors[0], /removed documentation path proposals\/monitoring-instance-detail-redesign.md/);
});
