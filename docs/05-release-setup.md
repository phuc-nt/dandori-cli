# Release Setup Guide

One-time setup for publishing releases via GitHub Releases + Homebrew tap.

## Prerequisites

- Admin access to the `phuc-nt/dandori-cli` repo
- Ability to create a new GitHub repo `phuc-nt/homebrew-dandori`

## One-time Setup

### 1. Create Homebrew tap repo

```bash
gh repo create phuc-nt/homebrew-dandori --public --description "Homebrew tap for dandori-cli"
cd ..
git clone https://github.com/phuc-nt/homebrew-dandori.git
cd homebrew-dandori
mkdir Formula
git add Formula
git commit --allow-empty -m "init"
git push
cd ../dandori-cli
```

goreleaser will push the formula to `phuc-nt/homebrew-dandori/Formula/dandori.rb` on each release.

### 2. Create HOMEBREW_TAP_TOKEN

The default `GITHUB_TOKEN` only has access to the current repo. goreleaser needs a separate token with write access to the tap repo.

1. Go to https://github.com/settings/tokens/new
2. Name: `dandori-homebrew-tap`
3. Expiration: 1 year (rotate later)
4. Scopes: **`repo`** (full control of private repositories — required even for public tap)
5. Generate and copy token
6. Add as repo secret: `gh secret set HOMEBREW_TAP_TOKEN --body "ghp_xxx..."`

### 3. Test locally (dry run)

```bash
# Install goreleaser
brew install goreleaser

# Dry-run a snapshot build — no pushes, no GitHub release
goreleaser release --snapshot --clean --skip=publish

# Output in dist/ — verify binaries work
./dist/dandori_darwin_arm64_v1/dandori version
```

## Publishing a Release

1. Update `CHANGELOG.md` — move items from `[Unreleased]` to a new version heading
2. Commit and push:
   ```bash
   git commit -am "chore: prep v0.2.0 release"
   git push
   ```
3. Tag:
   ```bash
   git tag -a v0.2.0 -m "v0.2.0 — shell alias + watch daemon"
   git push origin v0.2.0
   ```
4. GitHub Actions `release.yml` picks up the tag, runs goreleaser:
   - Builds binaries for darwin/linux/windows × amd64/arm64
   - Creates GitHub Release with all tarballs + `checksums.txt`
   - Pushes Homebrew formula to `phuc-nt/homebrew-dandori`

5. Verify:
   ```bash
   # GitHub Release
   gh release view v0.2.0

   # Homebrew
   brew update
   brew install phuc-nt/dandori/dandori
   dandori version
   ```

## Rollback

```bash
# Delete the bad tag
git tag -d v0.2.0
git push --delete origin v0.2.0

# Delete the GitHub release (keeps tag if still present)
gh release delete v0.2.0 --yes

# Revert the Homebrew formula manually (the tap repo has git history)
cd ../homebrew-dandori
git revert HEAD
git push
```

## Version Numbering

Follows [SemVer](https://semver.org/):
- `v0.x.y` — pre-1.0, breaking changes allowed in minor bumps
- `v1.0.0` — first stable release (when feature-complete and no known breaking changes)
- `v1.x.y` — breaking changes require major bump to `v2.0.0`
