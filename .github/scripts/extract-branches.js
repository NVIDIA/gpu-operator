module.exports = async ({ github, context, core }) => {
  let branches = [];

  // Get PR number
  const prNumber = context.payload.pull_request?.number || context.payload.issue?.number;

  if (!prNumber) {
    core.setFailed('Could not determine PR number from event');
    return [];
  }

  // Check PR body
  if (context.payload.pull_request?.body) {
    const prBody = context.payload.pull_request.body;
    // Enforce release-X.Y or release-X.Y.Z
    const bodyMatches = prBody.matchAll(/\/cherry-pick\s+(release-\d+\.\d+(?:\.\d+)?)/g);
    branches.push(...Array.from(bodyMatches, m => m[1]));
  }

  // Check all comments
  const comments = await github.rest.issues.listComments({
    owner: context.repo.owner,
    repo: context.repo.repo,
    issue_number: prNumber
  });

  for (const comment of comments.data) {
    const commentMatches = comment.body.matchAll(/\/cherry-pick\s+(release-\d+\.\d+(?:\.\d+)?)/g);
    branches.push(...Array.from(commentMatches, m => m[1]));
  }

  // Deduplicate
  branches = [...new Set(branches)];

  if (branches.length === 0) {
    core.setFailed('No valid release branches found in /cherry-pick comments');
    return [];
  }

  core.info(`Target branches: ${branches.join(', ')}`);
  return branches;
};
