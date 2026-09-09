# Source delivery and notice verification

Implemented on 2026-09-08 following the initial license review. This record
describes technical distribution checks, not a legal certification.

## Release behavior

The repository must be public before a publishing run. A separate
`sources-<version>` GitHub prerelease contains source assets; its name does not
match the product tag trigger and it is excluded from the stable update feed.
The main product release remains draft until all required jobs succeed.

Every native image build loads locally first. `scripts/compliance/collect.py`
exports a stopped, disposable container, inventories its actual contents, and
collects these materials:

- All installed Debian source packages: exact versions from dpkg, snapshot
  archives, `.dsc` files, upstream sources and distribution patches; source
  descriptors and archive sizes/SHA256 checksums must match.
- Alpine's exact aports revisions, package recipes and upstream distfiles.
  An isolated helper uses Alpine's source mirror and `abuild fetch verify` to
  verify the recipe checksums. It receives no Docker socket or host credentials.
- Go module archives identified by the executable's build information, checked
  by Go's module download/checksum mechanism, plus the matching Go sources.
  Caddy retains its generated xcaddy main module and module lock files.
  FrankenPHP's local replacement resolves to its recorded unmodified VCS commit.
- PHP's retained matching source archive, configure options and shipped build
  helpers; loaded PECL extension sources; actual WP-CLI PHAR package references
  cross-checked against its upstream lock file; WordPress and presso source.
- Original named notices, including nested dependency notices, Debian copyright
  files and shared license texts. Oxymel's exact pinned revision declares MIT
  only in `composer.json`; the collector preserves it with credited authorship
  and a clearly identified MIT supplement. It cannot waive arbitrary omissions.

Binary collection reads actual compiled Go dependencies. The frontend build
checks bundled npm module IDs against its notice list, and collection checks
the recorded versions and npm integrity hashes, including browser fonts.

`release.py` verifies the bundle, splits it into parts at most 1 GiB each,
uploads uniquely named assets, and hashes downloads without authentication.
Only after this succeeds can it push the corresponding image. Documentation
is attached in one filesystem layer; the collected original rootfs must be
an exact prefix of the resulting image. Its inventory records the source image
immutable Docker image ID, while the image's `SOURCE_BUNDLE` identifies matching assets.
The source-only release is populated incrementally; a failed build may leave
valid source assets there, but cannot bypass the product publication gates.

## Local rehearsal evidence

The initial source collection rehearsals used isolated local Linux arm64 images:

| Artifact | Actual inventory covered |
| --- | --- |
| pressod, Linux amd64 and arm64 snapshot | 49 Go modules, Go source distribution, 9 shipped npm/font packages |
| Edge | 36 Alpine packages from 23 aports origins, 155 Go modules, generated xcaddy build files |
| PHP 8.1 legacy base | 281 Debian binary packages from 183 source packages, PHP source, 2 PECL extensions, 55 Composer packages |
| PHP 8.5 modern base | 276 Debian binary packages from 171 source packages, 182 Go modules, PHP source, 2 PECL extensions, 55 Composer packages |

The inspected edge image ID was
`sha256:09e421dce5b055a310309c52241f3fad2cf860d9754cb56c42e48290faf5feef`;
the modern base was
`sha256:daa345e1052636197cfaf38d4aee1953ee9f5522b23d3e12b5ffa2b5ca98afda`.
These identify rehearsal inputs, not future published images. Generated
manifests provide the complete per-file checksums for each subsequent run.

GoReleaser's local snapshot produced both architecture archives containing the
full notice file, source instructions and bundle identity. Negative tests cover
missing, modified and unrecorded materials, unsafe paths, split reassembly,
draft source releases, and failed anonymous downloads blocking image pushes.
Workflow checks enforce source preparation, the verified publishing wrapper,
and failed/cancelled/skipped prerequisite rejection. Frontend check/lint/build
and workflow syntax validation also passed.

The complete local image-packaging path passed too: the documented edge ran
Caddy with its cache module, and its real source assets reassembled and passed
manifest verification. The attached documentation preserved all original
filesystem layers. The output image ID was
`sha256:f988663c737d978a043aef632981d5ac32e79727b5575129517295bb2c7acd34`.

No public release or image upload was performed during this rehearsal. The
updated CI must pass on the committed changes, and the tag workflow must verify
the final native builds and their actual anonymous source downloads. Download
availability is tested during publication rather than claimed from a local run.

## Reproduce

Requires Python 3.12+, Go, Docker, public upstream downloads and several GB of
temporary storage. GitHub CLI credentials are needed only for publication.

```sh
python3 -m unittest discover -s scripts/compliance -p 'test_*.py' -v
ruby scripts/check-release-gates.rb
task release:snapshot
docker build -f build/edge.Dockerfile -t presso-edge:source-test build
python3 scripts/compliance/release.py image --image presso-edge:source-test \
  --target presso-edge:documented-test --release dev \
  --output /tmp/presso-source-test-assets
```

The last command collects sources and creates a local documented image. It
does not publish without `--push`. Run against unused test tags and directories.
The collector never invokes the engine or modifies existing site volumes.
Keep published materials available according to [licensing and source access](../licensing.md).
