# Release

Cut a new Dorgu CLI release. The argument to this command is the version to release (e.g. `/release v0.2.1`).

If no version is provided, determine the next version by reading `CHANGELOG.md` and the latest git tag.

## Step 1: Pre-flight checks

```bash
# Confirm you're on main and it's clean
git status
git log --oneline -5

# Run full CI check (must pass before tagging)
make check

# Run with coverage to see the current test state
make test-coverage
```

If `make check` fails, stop and fix the issues. Do not tag a failing build.

## Step 2: Verify the binary builds cleanly

```bash
make build
./build/dorgu version
# Confirm the version string looks right (will show "dev" for untagged builds)
```

## Step 3: Update CHANGELOG.md

Open `CHANGELOG.md` and move items from `[Unreleased]` into a new versioned section:

```markdown
## [<VERSION>] - <YYYY-MM-DD>

### Added
- ...

### Changed
- ...

### Fixed
- ...
```

Follow [Keep a Changelog](https://keepachangelog.com/en/1.1.0/) conventions:
- **Added** — new features
- **Changed** — changes to existing functionality
- **Fixed** — bug fixes
- **Removed** — removed features
- **Deprecated** — features that will be removed

Leave `[Unreleased]` section empty and ready for the next cycle.

## Step 4: Commit the changelog

```bash
git add CHANGELOG.md
git commit -m "chore: release <VERSION>"
```

## Step 5: Tag the release

```bash
git tag -a <VERSION> -m "Release <VERSION>"
```

Version must follow semver (`vMAJOR.MINOR.PATCH`). Pre-releases use `-rc.N` suffix (e.g. `v0.2.1-rc.1`).

## Step 6: Verify the build with GoReleaser (dry run)

```bash
make goreleaser
# This runs: goreleaser release --snapshot --clean
# Builds all platform binaries to ./dist/ without publishing
```

Check that `./dist/` contains binaries for:
- `dorgu_linux_amd64`
- `dorgu_linux_arm64`
- `dorgu_darwin_amd64`
- `dorgu_darwin_arm64`
- `dorgu_windows_amd64`

## Step 7: Push tag to trigger release workflow

```bash
git push origin main
git push origin <VERSION>
```

The `release.yaml` GitHub Actions workflow triggers on tag push and runs GoReleaser to publish binaries and create the GitHub Release.

## Step 8: Verify the release

After CI completes:
1. Check GitHub Releases page for `<VERSION>` with attached binaries and checksums
2. Test install from the release: `go install github.com/dorgu-ai/dorgu/cmd/dorgu@<VERSION>`
3. Verify `dorgu version` output matches the tag

## Step 9: Update the Operator release (if needed)

If this CLI release corresponds to an Operator release, go to `dorgu-ai/dorgu-operator` and repeat the same steps there. The Helm chart version in `charts/dorgu-operator/Chart.yaml` should also be bumped and published to GHCR.

## Rollback

If the release is broken:
```bash
# Delete the tag locally and remotely
git tag -d <VERSION>
git push origin :refs/tags/<VERSION>
```

Then fix the issue, update CHANGELOG.md (amend the section), and re-tag.
