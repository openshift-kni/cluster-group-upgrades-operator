const {test} = require('node:test');
const assert = require('node:assert/strict');
const {mkdtempSync, mkdirSync, writeFileSync, rmSync} = require('node:fs');
const {tmpdir} = require('node:os');
const path = require('node:path');
const {execFileSync} = require('node:child_process');
const {run, isExcluded} = require('./backport.cjs');

function fixture(t) {
  const dir = mkdtempSync(path.join(tmpdir(), 'backport-test-'));
  t.after(() => rmSync(dir, {recursive: true, force: true}));
  const remote = path.join(dir, 'origin.git');
  const cwd = path.join(dir, 'checkout');
  const raw = (where, ...args) => execFileSync('git', args, {
    cwd: where, encoding: 'utf8', stdio: ['ignore', 'pipe', 'pipe'],
    env: {...process.env, GIT_CONFIG_NOSYSTEM: '1', GIT_CONFIG_GLOBAL: '/dev/null'},
  }).trim();
  raw(dir, 'init', '--bare', remote);
  raw(dir, 'clone', remote, cwd);
  const git = (...args) => raw(cwd, ...args);
  git('config', 'user.name', 'Test Author');
  git('config', 'user.email', 'author@example.com');
  git('config', 'commit.gpgsign', 'false');
  git('checkout', '-b', 'release-5.0');
  function commit(files, message = 'Change') {
    for (const [file, content] of Object.entries(files)) {
      mkdirSync(path.dirname(path.join(cwd, file)), {recursive: true});
      writeFileSync(path.join(cwd, file), content);
    }
    git('add', '--all');
    git('commit', '-m', message);
    return git('rev-parse', 'HEAD');
  }
  commit({'code.txt': 'original\n', 'vendor/dep': 'old\n'});
  const before = git('rev-parse', 'HEAD');
  git('branch', 'release-4.23');
  git('push', 'origin', 'release-5.0', 'release-4.23');
  const prs = new Map();
  const links = new Map();
  const backports = [];
  const errors = [];
  const logs = [];
  let createFailures = 0;
  const marker = () => {};
  const core = {
    info: (message) => logs.push(message), notice: (message) => logs.push(message),
    error: (message) => errors.push(message), setFailed: (message) => errors.push(message),
  };
  const github = {
    rest: {
      repos: {listPullRequestsAssociatedWithCommit: marker},
      pulls: {
        list: () => {},
        get: async ({pull_number}) => ({data: prs.get(pull_number)}),
        create: async (params) => {
          if (createFailures-- > 0) throw new Error('API unavailable');
          const data = {...params, html_url: `https://example.com/backport/${backports.length + 1}`};
          backports.push(data);
          return {data};
        },
      },
    },
    paginate: async (method, params) => {
      assert.equal(params.per_page, 100);
      if (method === marker) return (links.get(params.commit_sha) || []).map((number) => prs.get(number));
      assert.equal(method, github.rest.pulls.list);
      assert.equal(params.state, 'all');
      return backports.filter((pr) => `owner:${pr.head}` === params.head && pr.base === params.base);
    },
  };
  function register(number, shas, count = shas.length) {
    prs.set(number, {
      number, title: `Change ${number}`, html_url: `https://example.com/pull/${number}`,
      merged: true, merged_at: '2026-01-01T00:00:00Z', commits: count,
      merge_commit_sha: shas.at(-1), base: {ref: 'release-5.0', repo: {full_name: 'owner/repo'}},
    });
    for (const sha of shas) links.set(sha, [...(links.get(sha) || []), number]);
  }
  function merge(number, changes, mode = 'merge') {
    git('checkout', '-b', `feature-${number}`);
    const original = changes.map((files) => commit(files, `PR ${number} part`));
    git('push', 'origin', `HEAD:refs/pull/${number}/head`);
    git('checkout', 'release-5.0');
    let shas;
    if (mode === 'merge') {
      git('merge', '--no-ff', `feature-${number}`, '-m', `Merge PR ${number}`);
      shas = [git('rev-parse', 'HEAD')];
    } else if (mode === 'squash') {
      git('merge', '--squash', `feature-${number}`);
      git('commit', '-m', `Squash PR ${number}`);
      shas = [git('rev-parse', 'HEAD')];
    } else {
      commit({[`direct-${number}.txt`]: 'unrelated\n'}, 'Direct change before rebase');
      git('checkout', `feature-${number}`);
      git('rebase', 'release-5.0');
      git('checkout', 'release-5.0');
      const start = git('rev-parse', 'HEAD');
      git('merge', '--ff-only', `feature-${number}`);
      shas = git('rev-list', '--reverse', `${start}..HEAD`).split('\n');
    }
    register(number, shas, original.length);
    prs.get(number).head = {sha: original.at(-1)};
    return shas;
  }
  const context = {eventName: 'push', repo: {owner: 'owner', repo: 'repo'}, payload: {
    before, after: before, ref: 'refs/heads/release-5.0', created: false, deleted: false, forced: false,
  }};
  const execute = async () => {
    context.payload.after = git('rev-parse', 'release-5.0');
    git('push', 'origin', 'release-5.0');
    await run({github, context, core, cwd, target: 'release-4.23'});
  };
  const branch = (number) => `backport/pr-${number}-to-release-4.23`;
  const branchSha = (number) => raw(remote, 'rev-parse', `refs/heads/${branch(number)}`);
  const content = (number, file) => raw(remote, 'show', `${branch(number)}:${file}`);
  return {git, commit, merge, register, execute, context, prs, links, backports, errors, logs,
    branch, branchSha, content, failCreate: () => {createFailures = 1;}};
}

for (const mode of ['merge', 'squash', 'rebase']) {
  test(`backports the entire ${mode} PR, including excluded files`, async (t) => {
    const f = fixture(t);
    f.merge(1, [{'code.txt': 'changed\n'}, {'vendor/dep': 'new\n'}], mode);
    await f.execute();
    assert.deepEqual(f.errors, []);
    assert.equal(f.backports.length, 1);
    assert.equal(f.content(1, 'code.txt'), 'changed');
    assert.equal(f.content(1, 'vendor/dep'), 'new');
    assert.equal(f.backports[0].base, 'release-4.23');
    assert.match(f.backports[0].body, /Backport of #1/);
    if (mode === 'rebase') assert.throws(() => f.content(1, 'direct-1.txt'));
  });
}

test('processes multiple PRs independently and ignores direct and excluded-only changes', async (t) => {
  const f = fixture(t);
  f.merge(1, [{'vendor/dep': 'dependency-only\n'}]);
  f.commit({'direct.txt': 'direct\n'});
  f.merge(2, [{'feature.txt': 'eligible\n'}]);
  f.merge(3, [{'another.txt': 'also eligible\n'}]);
  await f.execute();
  assert.deepEqual(f.errors, []);
  assert.equal(f.backports.length, 2);
  assert.throws(() => f.branchSha(1));
  assert.equal(f.content(2, 'vendor/dep'), 'old');
  assert.throws(() => f.content(2, 'direct.txt'));
  assert.throws(() => f.content(3, 'feature.txt'));
});

test('conflicts fail visibly without publishing a partial branch, and other PRs continue', async (t) => {
  const f = fixture(t);
  f.git('checkout', 'release-4.23');
  f.commit({'code.txt': 'target change\n'});
  f.git('push', 'origin', 'release-4.23');
  f.git('checkout', 'release-5.0');
  f.merge(1, [{'code.txt': 'source change\n'}]);
  f.merge(2, [{'safe.txt': 'independent\n'}]);
  await f.execute();
  assert.match(f.errors.join('\n'), /Cherry-pick failed/);
  assert.throws(() => f.branchSha(1));
  assert.equal(f.backports.length, 1);
  assert.equal(f.content(2, 'safe.txt'), 'independent');
});

test('rerunning does not duplicate or overwrite open, closed or merged backports', async (t) => {
  const f = fixture(t);
  f.merge(1, [{'code.txt': 'changed\n'}]);
  await f.execute();
  const sha = f.branchSha(1);
  for (const state of ['open', 'closed', 'merged']) {
    f.backports[0].state = state;
    await f.execute();
    assert.equal(f.backports.length, 1);
    assert.equal(f.branchSha(1), sha);
  }
  assert.deepEqual(f.errors, []);
});

test('recovers after pushing the branch but failing to create the PR', async (t) => {
  const f = fixture(t);
  f.merge(1, [{'code.txt': 'changed\n'}]);
  f.failCreate();
  await f.execute();
  const sha = f.branchSha(1);
  assert.equal(f.backports.length, 0);
  assert.match(f.errors.join('\n'), /API unavailable/);
  f.errors.length = 0;
  await f.execute();
  assert.deepEqual(f.errors, []);
  assert.equal(f.backports.length, 1);
  assert.equal(f.branchSha(1), sha);
});

test('does not overwrite an existing branch without a backport ownership marker', async (t) => {
  const f = fixture(t);
  f.git('push', 'origin', `HEAD:refs/heads/${f.branch(1)}`);
  const original = f.branchSha(1);
  f.merge(1, [{'code.txt': 'changed\n'}]);
  await f.execute();
  assert.match(f.errors.join('\n'), /not owned/);
  assert.equal(f.branchSha(1), original);
  assert.equal(f.backports.length, 0);
});

test('skips a change already applied to the target', async (t) => {
  const f = fixture(t);
  const [sha] = f.merge(1, [{'code.txt': 'changed\n'}], 'squash');
  f.git('checkout', 'release-4.23');
  f.git('cherry-pick', sha);
  f.git('push', 'origin', 'release-4.23');
  f.git('checkout', 'release-5.0');
  await f.execute();
  assert.deepEqual(f.errors, []);
  assert.equal(f.backports.length, 0);
  assert.throws(() => f.branchSha(1));
});

test('renaming an eligible file into an excluded directory remains eligible', async (t) => {
  const f = fixture(t);
  f.git('checkout', '-b', 'rename');
  f.git('mv', 'code.txt', 'vendor/code.txt');
  f.git('commit', '-m', 'Move code');
  f.git('checkout', 'release-5.0');
  f.git('merge', '--no-ff', 'rename', '-m', 'Merge rename');
  f.register(1, [f.git('rev-parse', 'HEAD')]);
  await f.execute();
  assert.deepEqual(f.errors, []);
  assert.equal(f.backports.length, 1);
  assert.throws(() => f.content(1, 'code.txt'));
  assert.equal(f.content(1, 'vendor/code.txt'), 'original');
});

test('branch creation is skipped and force pushes are rejected', async (t) => {
  const f = fixture(t);
  f.merge(1, [{'code.txt': 'changed\n'}]);
  f.context.payload.created = true;
  await f.execute();
  assert.equal(f.backports.length, 0);
  f.context.payload.created = false;
  f.context.payload.forced = true;
  await assert.rejects(f.execute(), /forced or non-fast-forward/);
});

test('PRs from another base and PRs merged outside this push are not backported', async (t) => {
  const f = fixture(t);
  f.merge(1, [{'code.txt': 'changed\n'}]);
  f.prs.get(1).base.ref = 'main';
  f.merge(2, [{'feature.txt': 'changed\n'}]);
  f.prs.get(2).merge_commit_sha = f.context.payload.before;
  await f.execute();
  assert.deepEqual(f.errors, []);
  assert.equal(f.backports.length, 0);
});

test('partial multi-commit rebase associations fail instead of publishing an incomplete PR', async (t) => {
  const f = fixture(t);
  const shas = f.merge(1, [{'a.txt': 'a\n'}, {'b.txt': 'b\n'}, {'c.txt': 'c\n'}], 'rebase');
  f.links.delete(shas[0]);
  await assert.rejects(f.execute(), /Ambiguous rebase merge/);
  assert.equal(f.backports.length, 0);
});

test('dependency and infra exclusions preserve eligible Dockerfiles and workflows', () => {
  for (const file of ['vendor/crate/file', '.konflux/build.Dockerfile', '.tekton/task.yaml', 'renovate.json']) {
    assert.equal(isExcluded(file), true, file);
  }
  for (const file of ['Dockerfile', 'src/code.rs', 'controllers/main.go', '.github/workflows/ci.yml', 'vendor-code.go']) {
    assert.equal(isExcluded(file), false, file);
  }
  for (const file of ['go.mod', 'go.sum', 'bundle/manifest.yaml', 'telco5g-konflux']) assert.equal(isExcluded(file), true, file);
});


test('a last-commit-only rebase association cannot masquerade as a squash', async (t) => {
  const f = fixture(t);
  const shas = f.merge(1, [{'a.txt': 'first change\n'}, {'b.txt': 'second change\n'}], 'rebase');
  f.links.delete(shas[0]);
  await assert.rejects(f.execute(), /does not match the complete PR patch/);
  assert.equal(f.backports.length, 0);
});


test('partial rebase discovery also detects a missing whitespace-only change', async (t) => {
  const f = fixture(t);
  const shas = f.merge(1, [{'code.txt': '  original\n'}, {'feature.txt': 'new\n'}], 'rebase');
  f.links.delete(shas[0]);
  await assert.rejects(f.execute(), /does not match the complete PR patch/);
  assert.equal(f.backports.length, 0);
});

test('a conflict after an earlier successful cherry-pick publishes neither commit', async (t) => {
  const f = fixture(t);
  f.git('checkout', 'release-4.23');
  f.commit({'code.txt': 'target change\n'});
  f.git('push', 'origin', 'release-4.23');
  f.git('checkout', 'release-5.0');
  f.merge(1, [{'first.txt': 'clean change\n'}, {'code.txt': 'conflicting change\n'}], 'rebase');
  await f.execute();
  assert.match(f.errors.join('\n'), /Cherry-pick failed/);
  assert.equal(f.backports.length, 0);
  assert.throws(() => f.branchSha(1));
});
