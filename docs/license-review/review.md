# License and redistribution review

Reviewed on 2026-09-08 against commit `8a80082` and representative local build
artifacts. This is a technical compliance review, not a legal opinion or a
certification of every file in the distribution.

**Current status:** Apache 2.0 remains the license for original presso code.
The source-delivery and notice work identified in findings 1–2 is now implemented
in the release pipeline, with local rehearsals recorded in
[source-delivery.md](source-delivery.md). Each final artifact
must pass those checks again; source materials must be anonymously downloadable
before its image can be pushed. The Valkey change resolves finding 3.

The findings below preserve the original review's evidence and omissions. They
are historical observations, not an inventory of a future release. Automation
checks source identity, retained notices, completeness against the supported
package inventories, and availability; it does not replace a per-file legal audit.

Making source available and distributing compiled images are different review
scopes. A pre-1.0 label, a free download, or a no-warranty disclaimer does not
remove redistribution obligations.

## What was examined

- Current Dockerfiles, runtime download/installation scripts, release workflows,
  archive contents, and source/license documentation.
- The Linux amd64/arm64 snapshot binary dependency records. Their Go code,
  dependency files, and frontend sources match the reviewed commit; the earlier
  snapshot identifies `ff9c4aa` with documentation changes in the working tree.
- Root license files for 49 linked Go modules, including supplemental notices
  in the `modernc.org` modules. This is not a per-file copyright audit.
- All 230 npm lockfile license declarations and selected browser/font license
  files. The lockfile includes build tools and optional platform packages, not
  just shipped browser code.
- Representative local arm64 runtime bases: 276 Debian package entries in the
  PHP 8.5 image and 281 in the PHP 8.1 image; the edge's Alpine package inventory
  and its 155 linked Go modules; WP-CLI's PHAR notice filenames; the actual Redis
  server version selected by the local default tag.

Exact versions, inspected image IDs, and notice hashes are in
[the inventory](inventory.json). These local images predate
some notice changes. Their package inventories demonstrate the distribution
scope; they are not evidence that final tag images contain the latest notices.
Final amd64 and arm64 release digests still need their own inventory and checks.

## Apache and GPL boundaries

| Component | Assessment |
| --- | --- |
| Go engine and standalone web UI | Original code can remain Apache-2.0. The engine communicates with WordPress through container execution and HTTP, rather than linking the PHP drop-in into its executable. Dependencies retain their own terms. |
| Original PHP system plugin and configuration | Apache-2.0 is compatible with GPLv3. When distributed as part of a GPL-covered WordPress combination, preserve the combined work's GPL permissions; do not advertise that combination as Apache-only. |
| WordPress | GPLv2-or-later permits use under GPLv3 for the relevant combination. The runtime now retains WordPress's license outside the web root. |
| Redis Object Cache 2.8.0 | GPLv3. The actual PHP source is vendored and copied into the image; the upstream header and full license are present. Its recorded source hash is unchanged. No need to relicense the independent Go engine because of this file. |
| PHP, web servers, operating-system packages | Separate programs and libraries carry separate obligations. A container is not automatically one uniformly licensed work. |

This is an architectural assessment, not a court determination about derivative
works. Apache-to-GPLv3 compatibility is one-way: the GPL component cannot simply
be relicensed as Apache. The relevant principles are explained by
[Apache](https://www.apache.org/licenses/GPL-compatibility),
[GNU's aggregation guidance](https://www.gnu.org/licenses/gpl-faq.en.html#MereAggregation),
and [WordPress's license statement](https://en-gb.wordpress.org/about/license/).

## Findings before publishing artifacts

### 1. Container source delivery — original blocker, delivery pipeline added

The runtime and edge distributions contain more than WordPress source and
presso's Dockerfiles. The inspected images include packages such as Bash,
coreutils, MariaDB clients, BusyBox, socat, and libraries with their own
GPL/LGPL or other terms. PHP extensions and WP-CLI add another dependency layer.

`runtime/base.Dockerfile`, `runtime/base-legacy.Dockerfile`, and
`build/edge.Dockerfile` install binaries or compile downloaded components. The
release workflow publishes the resulting images without a corresponding
per-image source-delivery record. It does not currently collect matching source
packages, patches, configuration/build scripts, or source availability evidence.
No complete corresponding-source archive or established offer mechanism was
found. Having Debian copyright files in an image is useful but does not by
itself establish source delivery.

Mutable base tags, unversioned extension installation, and unpinned xcaddy
modules also mean the repository alone cannot identify everything used in a
future build. The local edge, for example, contains Caddy v2.11.4, Souin v1.7.8,
and 155 linked Go modules. Its dependency licenses were not all individually
reviewed here.

Before publication:

1. Record the actual base digest, package/source-package versions, PHP extension
   versions, and embedded Go/Composer dependencies for each release architecture.
2. Establish the applicable source-delivery method for each covered component.
   A retained source bundle with matching patches and build instructions is the
   easiest method to verify; it is not the only method licenses may permit.
3. Put clear source-access directions beside the image downloads and preserve
   access for the period the selected license/distribution method requires.
4. Verify that source records and notices are available before images become
   publicly downloadable. Keeping the GitHub release draft does not hide an
   image pushed to a public GHCR package.

See GPLv3's definition of Corresponding Source and section 6 in the
[included license](../../runtime/dropins/COPYING), and GPLv2 section 3 for components
using that version. Obligations differ between source and non-source forms.
The source-form Redis PHP file is not equivalent to distributing a stripped
native binary. Docker's own [PHP image documentation](https://raw.githubusercontent.com/docker-library/docs/master/php/README.md#license)
also makes clear that image users must account for the software inside it.

### 2. Dependency notices — original blocker, collection and archive gates added

The archives defined in `.goreleaser.yaml` include presso's license and policy
documents, but not a complete third-party notice bundle for the compiled engine
and embedded frontend. The 49 linked Go modules have these root license types:

| Root license type | Modules |
| --- | ---: |
| Apache-2.0 | 19 |
| MIT | 15 |
| BSD family | 14 |
| MPL-2.0 | 1 |

The MPL module is `github.com/go-sql-driver/mysql` v1.10.1. It can be used in an
Apache-licensed larger work, but executable recipients must be told how to get
its MPL-covered source. That explicit versioned notice has now been added to
`docs/licensing.md`, which is already included in binary archives and the
engine image. The upstream tagged source was reachable during this review.
Future driver changes must update the notice and keep source access working.
See [Mozilla's MPL FAQ, questions 8 and 11](https://www.mozilla.org/en-US/MPL/2.0/FAQ/).

The other modules still need their applicable copyright/license notices bundled
with distributions. Include supplemental third-party notices, particularly
`modernc.org/libc` and `modernc.org/memory`, and the Go toolchain/runtime notices.
A general sentence saying dependencies have their own licenses is insufficient
to replace the actual required notices.

The frontend ships fonts and browser runtime code, including Svelte and
SvelteKit even though npm marks them as development dependencies. An inventory
based only on `npm --omit=dev` would miss shipped code. Lightning CSS's MPL
entries are build-tool packages; their presence in the lockfile does not by
itself impose MPL on generated CSS.

Instrument Sans, Instrument Serif, and IBM Plex Mono use OFL-1.1. At the reviewed
commit, no standalone font notices were in the static build. The inspected WOFF
name tables for IBM Plex Mono and Instrument Serif contained copyright text and
a license URL, but no full license text; the variable font's WOFF2 metadata was
not inspected. The web build now bundles the original notices as a served text
file, together with notices for the current browser runtime packages. This
removes the need to rely on embedded font metadata for those notices. See the
[OFL's distribution conditions](https://openfontlicense.org/open-font-license-official-text/).

WP-CLI v2.12.0's inspected PHAR retains Composer's license file but not WP-CLI's
root MIT license as a separately named file. The download script now retains the
root license and a versioned source reference outside the PHAR. This fixes that
specific omission; auditing all PHAR dependency notices remains outstanding.

### 3. Default Redis server license policy — resolved by Valkey

The original review observed `redis:7-alpine` resolving to **Redis 7.4.11**,
under RSALv2 or SSPLv1. That historical observation remains in the inventory;
it does not describe the current default.

The pre-release backend change selects **Valkey 8.1.10** with BSD-3-Clause
server licensing, using `valkey/valkey:8.1.10-alpine`. The engine pulls this
upstream image directly; the release does not republish it as a presso image.
Its dependencies still retain their respective licenses. See the
[separate verification record](valkey.md) for the inspected
image index, platform digests, compatibility coverage, and boundaries.

The unchanged GPLv3 PHP drop-in, PHP-licensed phpredis extension, and
BSD-licensed Go client connect using Valkey's compatible protocol. They do not
pull in the Redis server's service-related license. Apache 2.0 remains the
license for original presso code. This change does not resolve findings 1–2.

No existing Redis container, cache file, site volume, or engine database is
automatically migrated or removed. The server runs as an ephemeral cache with
persistence disabled; Redis 7.4 data files are not compatible with Valkey.

Sources: [Valkey 8.1.10's BSD license](https://raw.githubusercontent.com/valkey-io/valkey/8.1.10/COPYING)
and [Valkey's migration/protocol documentation](https://valkey.io/topics/migration/).

### 4. Independently published runtime bases lacked presso's own notice — fixed

The site image included Apache 2.0 and the presso notice through `mu-plugins/`,
but `presso-runtime-base` is published separately and contained original presso
scripts/configuration without those files. Both base Dockerfiles now copy the
existing canonical license and notice to `/usr/share/licenses/presso/`.
This does not satisfy or replace licenses for the upstream base contents.

## Changes made during this review

- Added the explicit MPL source notice for the MySQL driver.
- Added the original presso license/notice to independently published bases.
- Retained WP-CLI's versioned root license and source reference during download.
- Added browser/font notice generation before web builds; missing package
  license files fail that generation step. The explicit package list must be
  maintained when runtime dependencies change.
- Clarified the server/drop-in distinction and remaining scope in
  `docs/licensing.md`.
- Added this report to the binary archive's document list and saved the review
  inventory. That initial review made no backend substitution; the subsequent
  Valkey change is documented in finding 3 and its separate verification record.

## Follow-up implementation and release checks

- The collector inventories actual linked Go modules, shipped frontend modules,
  Debian/Alpine source packages, PHP extensions, and WP-CLI Composer packages.
  It retains source archives, build inputs, and original license/notice files.
- Binary archives and documented images include `THIRD_PARTY_LICENSES.txt` and
  `SOURCE_BUNDLE`. The source bundle identifies the inspected payload, records
  every retained file's hash, and includes presso's source/build instructions.
- Publishing jobs upload retained source bundles to the public
  `sources-<version>` prerelease and verify anonymous downloads before image
  pushes. Private or draft source releases cannot satisfy the gate.
- The tagged commit repeats these checks for the actual architectures and
  images. A new/missing dependency, failed download, or failed required job
  blocks publication; no earlier inventory substitutes for this run.
- Operators publishing their own modified builds must retain corresponding
  materials for those builds. See [licensing and source access](../licensing.md), including
  retention and access after moving downloads.

No license conflict found here requires abandoning Apache 2.0 for original
presso code. The separately licensed components keep their own permissions and
distribution requirements.
