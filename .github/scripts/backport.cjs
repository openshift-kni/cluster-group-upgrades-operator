// Used by actions/github-script; GitHub API access is injected for local tests.
const {spawnSync} = require('node:child_process');
const {mkdtempSync, rmSync} = require('node:fs');
const {tmpdir} = require('node:os');
const path = require('node:path');

const excludedPatterns = [
  /^go\.(mod|sum)$/,
  /^vendor\//,
  /^\.tekton\//,
  /^renovate\.json$/,
  /^\.konflux\//,
  /^bundle\//,
  // Submodule pointer — updated independently via Mintmaker on each branch
  /^telco5g-konflux$/,
];
const isExcluded = (filename) => excludedPatterns.some((pattern) => pattern.test(filename));

function git(cwd, args, allowFailure = false, input) {
  const result = spawnSync('git', [
    '-c', 'core.hooksPath=/dev/null', '-c', 'commit.gpgsign=false',
    '-c', 'user.name=github-actions[bot]',
    '-c', 'user.email=41898282+github-actions[bot]@users.noreply.github.com',
    ...args,
  ], {
    cwd, input, encoding: 'utf8', maxBuffer: 64 * 1024 * 1024,
    env: {...process.env, GIT_TERMINAL_PROMPT: '0'},
  });
  if (result.error) throw result.error;
  if (result.status !== 0 && !allowFailure) {
    throw new Error(`git ${args[0]} failed: ${result.stderr || result.stdout}`);
  }
  return result;
}
const lines = (text) => text.trim().split('\n').filter(Boolean);

async function run({github, context, core, cwd = process.cwd(), target = process.env.TARGET_BRANCH}) {
  const {owner, repo} = context.repo;
  const repository = `${owner}/${repo}`;
  const {before, after, ref, created, deleted, forced} = context.payload;
  if (context.eventName !== 'push' || !ref?.startsWith('refs/heads/')) {
    throw new Error('This backport workflow requires a branch push event.');
  }
  const source = ref.slice('refs/heads/'.length);
  if (deleted || created || /^0+$/.test(before || '')) {
    core.info('Skipping branch creation/deletion; no existing PR merge range.');
    return;
  }
  if (![before, after].every((sha) => /^[0-9a-f]{40}$/.test(sha || ''))) {
    throw new Error('Invalid push commit IDs.');
  }
  if (forced || git(cwd, ['merge-base', '--is-ancestor', before, after], true).status !== 0) {
    throw new Error('Refusing to backport a forced or non-fast-forward push.');
  }
  if (!target || target === source) throw new Error('A different TARGET_BRANCH is required.');
  git(cwd, ['check-ref-format', '--branch', target]);

  // Local history avoids webhook commit truncation and API file-list limits.
  // First-parent traversal excludes commits from the merged feature branches.
  const commits = lines(git(cwd, ['rev-list', '--reverse', '--first-parent', `${before}..${after}`]).stdout);
  const positions = new Map(commits.map((sha, index) => [sha, index]));
  const associations = new Map();
  const candidates = new Set();
  for (const sha of commits) {
    const prs = await github.paginate(github.rest.repos.listPullRequestsAssociatedWithCommit, {
      owner, repo, commit_sha: sha, per_page: 100,
    });
    const numbers = prs.filter((pr) => pr.merged_at && pr.base.ref === source &&
      pr.base.repo.full_name === repository).map((pr) => pr.number);
    associations.set(sha, new Set(numbers));
    numbers.forEach((number) => candidates.add(number));
  }

  const plans = [];
  for (const number of candidates) {
    const {data: pr} = await github.rest.pulls.get({owner, repo, pull_number: number});
    if (!pr.merged || pr.base.ref !== source || pr.base.repo.full_name !== repository ||
        !positions.has(pr.merge_commit_sha)) continue;
    const shas = commits.filter((sha) => associations.get(sha).has(number) &&
      positions.get(sha) <= positions.get(pr.merge_commit_sha));
    if (shas.at(-1) !== pr.merge_commit_sha || shas.some((sha, index) =>
      positions.get(sha) !== positions.get(shas[0]) + index)) {
      throw new Error(`Cannot identify a contiguous merged range for PR #${number}.`);
    }
    // Multiple first-parent commits represent a rebase merge. Never backport
    // only the final commit of a known multi-commit range.
    if (shas.length > 1 && (shas.length !== pr.commits || shas.some((sha) =>
      lines(git(cwd, ['rev-list', '--parents', '-n', '1', sha]).stdout)[0].split(' ').length !== 2))) {
      throw new Error(`Ambiguous rebase merge for PR #${number}; manual backport required.`);
    }
    if (pr.commits > 1 && git(cwd, ['rev-list', '--parents', '-n', '1', pr.merge_commit_sha])
      .stdout.trim().split(' ').length === 2) {
      // A one-commit association could be a squash OR an incomplete rebase.
      // Verify the complete original PR patch before accepting either case.
      git(cwd, ['fetch', '--no-tags', 'origin', `refs/pull/${number}/head`]);
      const head = git(cwd, ['rev-parse', 'FETCH_HEAD']).stdout.trim();
      if (head !== pr.head.sha) throw new Error(`PR #${number} head changed during discovery.`);
      const base = git(cwd, ['merge-base', `${shas[0]}^`, head]).stdout.trim();
      const patchId = (from, to) => {
        const diff = git(cwd, ['diff', '--binary', '--no-renames', from, to, '--']).stdout;
        return git(cwd, ['patch-id', '--verbatim'], false, diff).stdout.trim().split(' ')[0];
      };
      if (patchId(base, head) !== patchId(`${shas[0]}^`, shas.at(-1))) {
        throw new Error(`PR #${number} merged range does not match the complete PR patch; manual backport required.`);
      }
    }
    plans.push({pr, shas});
  }
  plans.sort((a, b) => positions.get(a.pr.merge_commit_sha) - positions.get(b.pr.merge_commit_sha));
  const claimed = new Set();
  for (const {pr, shas} of plans) {
    for (const sha of shas) {
      if (claimed.has(sha)) throw new Error(`Overlapping merged ranges at PR #${pr.number}.`);
      claimed.add(sha);
    }
  }
  for (const sha of commits) {
    if (!claimed.has(sha)) core.info(`No newly merged PR for ${sha}; skipping direct/unrelated commit.`);
  }
  if (!plans.length) return;

  git(cwd, ['fetch', '--no-tags', 'origin', `refs/heads/${target}`]);
  const targetSha = git(cwd, ['rev-parse', 'FETCH_HEAD']).stdout.trim();
  const failures = [];
  for (const {pr, shas} of plans) {
    let temporary;
    try {
      const files = git(cwd, ['diff', '--no-renames', '--name-only', '-z', `${shas[0]}^`, shas.at(-1), '--'])
        .stdout.split('\0').filter(Boolean);
      if (!files.some((filename) => !isExcluded(filename))) {
        core.info(`PR #${pr.number}: only excluded paths changed; skipping.`);
        continue;
      }
      const branch = `backport/pr-${pr.number}-to-${target}`;
      const marker = `Backport-of: ${repository}#${pr.number}@${pr.merge_commit_sha}`;
      const existing = await github.paginate(github.rest.pulls.list, {
        owner, repo, state: 'all', head: `${owner}:${branch}`, base: target, per_page: 100,
      });
      if (existing.length) {
        core.info(`PR #${pr.number}: backport already exists (${existing[0].html_url}); leaving it unchanged.`);
        continue;
      }
      const remote = git(cwd, ['ls-remote', '--exit-code', '--heads', 'origin', `refs/heads/${branch}`], true);
      if (remote.status === 0) {
        // Recover a run that pushed successfully but failed to open the PR.
        // Never overwrite branches, including branches edited by a maintainer.
        git(cwd, ['fetch', '--no-tags', 'origin', `refs/heads/${branch}`]);
        const message = git(cwd, ['log', '-1', '--format=%B', 'FETCH_HEAD']).stdout;
        if (!message.split('\n').includes(marker)) {
          throw new Error(`Existing branch ${branch} is not owned by this backport; leaving it unchanged.`);
        }
      } else {
        if (remote.status !== 2) throw new Error(`Cannot inspect backport branch: ${remote.stderr}`);
        temporary = mkdtempSync(path.join(process.env.RUNNER_TEMP || tmpdir(), 'backport-'));
        const worktree = path.join(temporary, 'tree');
        git(cwd, ['worktree', 'add', '--detach', worktree, targetSha]);
        try {
          for (const sha of shas) {
            const parents = git(cwd, ['rev-list', '--parents', '-n', '1', sha]).stdout.trim().split(' ').length - 1;
            const pick = git(worktree, ['cherry-pick', '-x', ...(parents > 1 ? ['-m', '1'] : []), sha], true);
            if (pick.status !== 0) {
              const conflicts = git(worktree, ['diff', '--name-only', '--diff-filter=U']).stdout.trim();
              const empty = git(worktree, ['diff', '--cached', '--quiet'], true).status === 0;
              if (!conflicts && empty) {
                git(worktree, ['cherry-pick', '--abort']);
                core.info(`PR #${pr.number}: ${sha} is already applied or empty.`);
                continue;
              }
              git(worktree, ['cherry-pick', '--abort'], true);
              throw new Error(`Cherry-pick failed; no branch or PR published. ${conflicts || pick.stderr}`);
            }
          }
          if (git(worktree, ['diff', '--quiet', targetSha, 'HEAD'], true).status === 0) {
            core.info(`PR #${pr.number}: changes already present in ${target}; skipping.`);
            continue;
          }
          const message = git(worktree, ['log', '-1', '--format=%B']).stdout.trimEnd();
          git(worktree, ['commit', '--amend', '-m', `${message}\n\n${marker}`]);
          // Empty expected value means create only if absent. A concurrent run
          // cannot replace an existing branch, even with different commits.
          git(worktree, ['push', `--force-with-lease=refs/heads/${branch}:`, 'origin', `HEAD:refs/heads/${branch}`]);
        } finally {
          git(cwd, ['worktree', 'remove', '--force', worktree], true);
        }
      }
      const {data: backport} = await github.rest.pulls.create({
        owner, repo, head: branch, base: target,
        title: `[${target}] ${pr.title}`.slice(0, 256),
        body: `Backport of #${pr.number} to \`${target}\`.\n\n` +
          `Source: ${pr.html_url}\n\n` +
          'Includes the complete merged PR, including dependency/infra files in mixed changes.\n\n' +
          `Source commits:\n${shas.map((sha) => `- ${sha}`).join('\n')}\n\n` +
          'Created automatically; normal review and required checks still apply.',
      });
      core.notice(`Created ${backport.html_url}`);
    } catch (error) {
      const message = `PR #${pr.number}: ${error.message}`;
      failures.push(message);
      core.error(message);
    } finally {
      if (temporary) rmSync(temporary, {recursive: true, force: true});
    }
  }
  if (failures.length) core.setFailed(`${failures.length} backport(s) need attention. See the errors above; rerun after resolving the cause.`);
}

module.exports = {run, isExcluded};
