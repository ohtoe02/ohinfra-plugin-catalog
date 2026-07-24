# Contributing a plugin

1. Publish a static `linux/amd64` executable as an immutable GitHub Release
   asset.
2. Add `plugins/<name>/<version>.yaml` using stable `X.Y.Z` versioning.
3. Include the exact manifest returned by
   `plugin manifest --protocol=1`, asset byte size and SHA-256.
4. Run the validation commands from the README.
5. Open a pull request. A maintainer must review and merge it before a signed
   catalog release can be published.

Changing an existing released entry is prohibited. Publish a new plugin version
or mark an existing version as yanked in a separately reviewed change.
