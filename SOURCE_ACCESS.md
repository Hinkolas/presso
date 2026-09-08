# Source code and third-party notices

Original presso code is Apache-2.0. Each bundled dependency retains its own
license. `THIRD_PARTY_LICENSES.txt` contains the collected upstream notices.
It accompanies binary archives and lives in container images under
`/usr/share/licenses/presso/`.

For a released version `<version>`, the matching source-only GitHub prerelease
is **sources-<version>**, at:

https://github.com/Hinkolas/presso/releases

Each binary/image has a source bundle containing a manifest of exact versions,
checksums, original source archives, distribution patches/build recipes,
applicable notices, and presso's build files. Container labels and the
`SOURCE_BUNDLE` file beside this document identify its bundle. Source bundles
are published and their anonymous download verified before the corresponding
container image is pushed. They are not temporary GitHub Actions artifacts.

Large bundles are split into numbered parts smaller than GitHub's 2 GiB asset
limit. Download the matching manifest, checksum file, and **all** numbered parts.
Verify the part checksums, concatenate parts in name order, then extract:

```sh
sha256sum -c <bundle>.sha256
cat <bundle>.tar.gz.part-* > source.tar.gz
tar -xzf source.tar.gz
tar -xzf <extracted-bundle-directory>/sources/presso/source.tar.gz
python3 presso/scripts/compliance/collect.py verify <extracted-bundle-directory>
```

The downloaded repository source includes the verification script. Debian
sources include the exact `.dsc`, upstream archives and Debian patches; use
`dpkg-source -x package.dsc` to reconstruct a package's preferred source form.
Alpine sources include the recorded aports recipes and checksum-verified
upstream distfiles. Go sources include module archives and the matching Go
source distribution; PHP sources include the source archive, configure options,
extension sources and build helpers. WP-CLI's source includes its matching
Composer references and upstream package archives.

For local/development builds, source materials are generated locally and are
not automatically uploaded. Publishing custom builds means preserving and
making their own matching materials available; the upstream release's bundles
must not be used to describe a different build.

## Retention

Do not delete or replace published source assets while their associated
binaries/images remain available. Preserve them longer if a component's license
or a distribution commitment requires it. Keep an independent backup of release
materials. Moving downloads requires maintaining equivalent source access and
updating the directions beside the binary/image downloads. A release marked
experimental has the same third-party distribution obligations.
