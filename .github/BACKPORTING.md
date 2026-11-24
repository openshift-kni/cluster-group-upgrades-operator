# Automatic backports

The `cherry-pick-to-4-23.yml` workflow runs on pushes to `release-5.0` in the
upstream repository. `TARGET_BRANCH` in that workflow selects the destination.
It uses the job's `GITHUB_TOKEN`; no personal access token, fork, Prow command,
or comment is required.

## Repository setup

In **Settings > Actions > General > Workflow permissions**, enable **Allow
GitHub Actions to create and approve pull requests** (subject to organization
policy). The job requests `contents: write` and `pull-requests: write`. Branch
rules must permit creation of `backport/pr-<number>-to-<target>` branches; the
release branch still requires normal review and checks. These settings cannot
be enabled by the workflow itself.

GitHub Actions checks on PRs created with `GITHUB_TOKEN` can require a maintainer
to approve workflow execution. External Prow checks depend on repository
configuration and must be verified after deployment. Backports touching
`.github/workflows/` are eligible, but GitHub may restrict publishing workflow
file changes with this token; such failures are reported for manual handling.

## Selection and behavior

- Reads the complete first-parent `before..after` history locally, rather than
  relying on the potentially truncated push payload. API PR associations are
  paginated and only PRs merged into the pushed branch in that range qualify.
  Direct commits without a newly merged PR are skipped.
- Uses each PR's actual merged Git diff to evaluate the path exclusions in
  `scripts/backport.cjs`. Dependency/infra-only PRs are skipped. Mixed PRs are
  backported in full, including excluded files. Renames account for both paths.
- Handles merge commits (first parent), squash merges, and contiguous rebase
  merges identified through commit-to-PR associations. Multi-commit squash/rebase
  ranges are also checked against the complete original PR patch. Ambiguous ranges fail
  for manual handling rather than intentionally selecting a partial range.
- Each PR gets an independent branch from the target release. Several PRs in
  one push are processed in source order. Dependent PRs can require manual
  backports until their prerequisites have merged into the target.
- Branch creation/deletion and force pushes are not treated as ordinary merges.
  No code or hooks from the cherry-picked changes are executed.
- Existing backport PRs, including closed or merged ones, are left unchanged.
  Backport branches are created only if absent and are never overwritten.
  A provenance marker allows retrying PR creation after a successful push.
- During publication, conflicts and permission/API failures produce workflow
  errors; other prepared PRs in the push continue. No conflicted or partially cherry-picked branch is pushed.
  Changes already present on the target are skipped. Inspect the failed run,
  resolve the cause, then use **Re-run failed jobs**, or backport manually.

There is no shared concurrency queue that could discard an intermediate push.
Concurrent duplicate attempts cannot replace an existing branch; the losing
attempt may fail and can be rerun to find the existing branch or PR.

## Local validation

Run the dependency-free Node.js integration tests from the repository root:

```sh
node --test .github/scripts/backport.test.cjs
```

They use temporary local Git repositories and mocked GitHub APIs. They do not
push to GitHub or create real pull requests. A real deployment still needs a
merged PR to verify repository permissions and downstream CI end to end.
