<?php
/**
 * Module: enforce. Platform-wide hardening and de-bloat defaults that need a
 * hook inside the request (RUNTIME.md section 7 / 14): XML-RPC and pingbacks
 * off, heartbeat throttled, emoji script gone, generator/RSD/WLW links gone,
 * automatic updates off (the image owns core; the engine runs updates).
 * Everything here is additive: a site exported without the plugin still works.
 */

namespace Presso\Enforce;

defined( 'ABSPATH' ) || exit;

function boot(): void {
	// XML-RPC is a brute-force and pingback-amplification magnet. The file
	// stays in place (removing it breaks some mobile apps' probing); every
	// authenticated method is refused and the method list is emptied, leaving
	// only the system.* introspection built-ins WordPress adds afterwards.
	add_filter( 'xmlrpc_enabled', '__return_false' );
	add_filter( 'xmlrpc_methods', '__return_empty_array' );
	add_filter( 'pings_open', '__return_false', 20 );
	add_filter( 'wp_headers', __NAMESPACE__ . '\\strip_pingback_header' );

	// Heartbeat: one poll a minute is plenty for post locks and autosave.
	add_filter( 'heartbeat_settings', __NAMESPACE__ . '\\throttle_heartbeat' );

	// Updates are a WP-CLI job from the engine, never a web request.
	add_filter( 'automatic_updater_disabled', '__return_true' );

	// Application passwords stay available: REST access for the engine.
	add_filter( 'wp_is_application_passwords_available', '__return_true' );

	add_action( 'init', __NAMESPACE__ . '\\remove_head_cruft' );
}

function strip_pingback_header( array $headers ): array {
	unset( $headers['X-Pingback'] );
	return $headers;
}

function throttle_heartbeat( array $settings ): array {
	$settings['interval'] = 60;
	return $settings;
}

/**
 * Drop the legacy front-end extras: emoji detection (browsers have handled
 * emoji for a decade), generator meta, RSD/WLW discovery links.
 */
function remove_head_cruft(): void {
	remove_action( 'wp_head', 'print_emoji_detection_script', 7 );
	remove_action( 'admin_print_scripts', 'print_emoji_detection_script' );
	remove_action( 'wp_print_styles', 'print_emoji_styles' );
	remove_action( 'admin_print_styles', 'print_emoji_styles' );
	remove_action( 'embed_head', 'print_emoji_detection_script' );
	remove_filter( 'the_content_feed', 'wp_staticize_emoji' );
	remove_filter( 'comment_text_rss', 'wp_staticize_emoji' );
	remove_filter( 'wp_mail', 'wp_staticize_emoji_for_email' );
	add_filter( 'emoji_svg_url', '__return_false' );
	add_filter( 'tiny_mce_plugins', fn( $plugins ) => array_diff( (array) $plugins, array( 'wpemoji' ) ) );
	add_filter( 'wp_resource_hints', __NAMESPACE__ . '\\drop_emoji_prefetch', 10, 2 );

	remove_action( 'wp_head', 'wp_generator' );
	add_filter( 'the_generator', '__return_empty_string' );
	remove_action( 'wp_head', 'rsd_link' );
	remove_action( 'wp_head', 'wlwmanifest_link' );
}

function drop_emoji_prefetch( array $urls, string $relation ): array {
	if ( $relation !== 'dns-prefetch' ) {
		return $urls;
	}
	return array_values(
		array_filter( $urls, fn( $url ) => strpos( (string) $url, 's.w.org' ) === false )
	);
}
