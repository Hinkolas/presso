<?php
/**
 * Plugin Name: presso platform
 * Description: Wires a site into the presso platform: edge cache headers and purges, hardening defaults, one-click login, request telemetry. Shipped read-only in the runtime image (RUNTIME.md section 14).
 * Version: 0.1.0
 * Author: presso
 * License: MIT
 *
 * WordPress only loads top-level files from WPMU_PLUGIN_DIR, so this file is
 * the loader; the modules live in presso/. Every module is toggled by
 * environment variables the orchestrator sets; there are no site-owner
 * settings. Scope rule: only things that must happen inside a web request
 * belong here; everything else goes through WP-CLI driven by the engine.
 */

namespace Presso;

defined( 'ABSPATH' ) || exit;

const VERSION = '0.1.0';

require_once __DIR__ . '/presso/helpers.php';
require_once __DIR__ . '/presso/modules/cache.php';
require_once __DIR__ . '/presso/modules/enforce.php';
require_once __DIR__ . '/presso/modules/login.php';
require_once __DIR__ . '/presso/modules/loopback.php';
require_once __DIR__ . '/presso/modules/telemetry.php';

Cache\boot();
Enforce\boot();
Login\boot();
Loopback\boot();
Telemetry\boot();
