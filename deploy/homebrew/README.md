# Emergent Homebrew Tap

This directory contains the Homebrew Formula for the Emergent CLI.

## How to Publish (One-Time Setup)

1.  Create a new public GitHub repository named `homebrew-emergent` (or `homebrew-tap`).
2.  Copy the `emergent-cli.rb` file to the root of that repository.
3.  Push the changes.

## How Users Install

Once the tap repository is public, users can install the CLI using:

```bash
brew tap emergent-company/emergent
brew install emergent-cli
```

(Replace `emergent-company` with the actual owner of the `homebrew-emergent` repo).

## How to Update

When releasing a new version of the CLI:

1.  Update the `version` field in `emergent-cli.rb`.
2.  Update the `url` and `sha256` for each platform.
    - You can find the SHA256 checksums in the `checksums.txt` file of the release assets.
3.  Commit and push the updated `emergent-cli.rb` to the `homebrew-emergent` repository.

Users will get the update when they run `brew upgrade`.
