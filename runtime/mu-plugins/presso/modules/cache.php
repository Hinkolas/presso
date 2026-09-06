<?php
/**
 * Module: cache. The WordPress half of the edge page cache (RUNTIME.md
 * section 4). Per response it tells the edge whether the page may be cached
 * (Cache-Control) and what it depended on (Surrogate-Key tags); on content
 * changes it collects the affected tags and, once per request on shutdown,
 * asks the engine to purge them. The edge itself, its hard bypass rules and
 * the purge fan-out are owned by ORCHESTRATION. Disabled by PRESSO_CACHE=off.
 *
 * Tag namespace: every tag is prefixed with "s" + PRESSO_SITE_ID so one shared
 * edge can hold many sites:
 *   s<ID>           whole site          s<ID>:home     front page / blog index
 *   s<ID>:p<post>   a post              s<ID>:t<term>  a term archive
 *   s<ID>:u<user>   an author archive   s<ID>:archive  any archive or search
 *   s<ID>:404
 */

namespace Presso\Cache;

use function Presso\env;
use function Presso\env_int;
use function Presso\is_cli;
use function Presso\log;

defined( 'ABSPATH' ) || exit;

const MAX_TAGS   = 100;
const SHORT_TTL  = 600; // Search results and 404s.
const NO_STORE   = 'private, no-store, max-age=0';

/** Cookie prefixes that mark a personalised session. Mirrors the edge rules. */
const PERSONAL_COOKIES = array(
	'wordpress_logged_in_',
	'wp-postpass_',
	'comment_author_',
	'woocommerce_',
	'wp_woocommerce_session_',
);

/** Path prefixes that are never cacheable, whatever WordPress says. */
const PERSONAL_PATHS = array( '/wp-admin', '/wp-login.php', '/wp-json', '/xmlrpc.php', '/wp-cron.php' );

function boot(): void {
	if ( env( 'PRESSO_CACHE', 'on' ) === 'off' ) {
		return;
	}

	if ( ! is_cli() ) {
		// Request-level facts (method, cookies, path, admin/REST) are known
		// early; query-level facts (singular, archive, posts) only after the
		// main query, at template_redirect. Admin, login and REST requests
		// never reach template_redirect, so the early hooks cover them.
		add_action( 'init', __NAMESPACE__ . '\\send_early_headers', PHP_INT_MAX );
		add_action( 'send_headers', __NAMESPACE__ . '\\send_early_headers', PHP_INT_MAX );
		add_action( 'template_redirect', __NAMESPACE__ . '\\send_headers', 0 );
	}

	// Purge collection. Hooks fire in web requests and WP-CLI alike.
	add_action( 'transition_post_status', __NAMESPACE__ . '\\on_transition_post_status', 10, 3 );
	add_action( 'save_post', __NAMESPACE__ . '\\on_post_change', 10, 2 );
	add_action( 'delete_post', __NAMESPACE__ . '\\on_post_change', 10, 2 );
	add_action( 'trashed_post', __NAMESPACE__ . '\\on_post_change', 10, 1 );
	add_action( 'comment_post', __NAMESPACE__ . '\\on_new_comment', 10, 2 );
	add_action( 'wp_set_comment_status', __NAMESPACE__ . '\\on_comment_change', 10, 1 );
	add_action( 'edit_comment', __NAMESPACE__ . '\\on_comment_change', 10, 1 );
	add_action( 'created_term', __NAMESPACE__ . '\\on_term_change', 10, 1 );
	add_action( 'edited_term', __NAMESPACE__ . '\\on_term_change', 10, 1 );
	add_action( 'delete_term', __NAMESPACE__ . '\\on_term_change', 10, 1 );

	foreach ( array(
		'switch_theme',
		'customize_save_after',
		'wp_update_nav_menu',
		'activated_plugin',
		'deactivated_plugin',
		'upgrader_process_complete',
		'update_option_blogname',
		'update_option_blogdescription',
		'update_option_permalink_structure',
		'update_option_show_on_front',
		'update_option_page_on_front',
		'update_option_page_for_posts',
		'update_option_posts_per_page',
		'update_option_sidebars_widgets',
	) as $hook ) {
		add_action( $hook, __NAMESPACE__ . '\\purge_all', 10, 0 );
	}
	// theme_mods_<theme> has the theme name in the option name.
	add_action( 'updated_option', __NAMESPACE__ . '\\on_updated_option', 10, 1 );

	add_action( 'shutdown', __NAMESPACE__ . '\\flush', 100 );

	if ( is_cli() && class_exists( '\\WP_CLI' ) ) {
		\WP_CLI::add_command( 'presso purge', __NAMESPACE__ . '\\cli_purge' );
	}
}

// --- Response headers --------------------------------------------------------

/** Whether a decision has been sent for this request; headers are sent once. */
function decided( ?bool $set = null ): bool {
	static $decided = false;
	if ( $set !== null ) {
		$decided = $set;
	}
	return $decided;
}

function site_tag(): string {
	return 's' . (string) env( 'PRESSO_SITE_ID', '' );
}

/**
 * Request-level reasons this response is personal, available before the
 * main query runs.
 */
function request_is_personal(): bool {
	$method = strtoupper( (string) ( $_SERVER['REQUEST_METHOD'] ?? 'GET' ) );
	if ( $method !== 'GET' && $method !== 'HEAD' ) {
		return true;
	}
	$path = (string) parse_url( (string) ( $_SERVER['REQUEST_URI'] ?? '/' ), PHP_URL_PATH );
	foreach ( PERSONAL_PATHS as $prefix ) {
		if ( str_starts_with( $path, $prefix ) ) {
			return true;
		}
	}
	foreach ( array_keys( $_COOKIE ) as $name ) {
		foreach ( PERSONAL_COOKIES as $prefix ) {
			if ( str_starts_with( (string) $name, $prefix ) ) {
				return true;
			}
		}
	}
	if ( is_admin() ) {
		return true;
	}
	if ( function_exists( 'is_user_logged_in' ) && is_user_logged_in() ) {
		return true;
	}
	return defined( 'DONOTCACHEPAGE' ) && DONOTCACHEPAGE;
}

/**
 * Query-level reasons, valid once the main query has run.
 */
function query_is_personal(): bool {
	if ( is_preview() || is_customize_preview() ) {
		return true;
	}
	foreach ( array( 'is_cart', 'is_checkout', 'is_account_page' ) as $fn ) {
		if ( function_exists( $fn ) && $fn() ) {
			return true;
		}
	}
	return false;
}

/**
 * init / send_headers: only commit when the answer is "not cacheable"; a
 * positive decision needs the main query.
 */
function send_early_headers(): void {
	if ( decided() || headers_sent() ) {
		return;
	}
	if ( request_is_personal() ) {
		header( 'Cache-Control: ' . NO_STORE );
		decided( true );
	}
}

/**
 * template_redirect (priority 0): the final decision for front-end pages.
 */
function send_headers(): void {
	if ( decided() || headers_sent() ) {
		return;
	}
	decided( true );

	if ( request_is_personal() || query_is_personal() ) {
		header( 'Cache-Control: ' . NO_STORE );
		return;
	}

	$ttl = ( is_search() || is_404() ) ? SHORT_TTL : env_int( 'PRESSO_CACHE_TTL', 43200, 1 );
	header( sprintf( 'Cache-Control: public, s-maxage=%d, max-age=0', $ttl ) );
	// Comma-separated: the edge cache (Souin) splits surrogate keys on commas.
	header( 'Surrogate-Key: ' . implode( ', ', surrogate_keys() ) );
}

/**
 * What the current main query depended on, as tags (capped).
 */
function surrogate_keys(): array {
	global $wp_query;
	$s    = site_tag();
	$tags = array( $s );

	if ( is_front_page() || is_home() ) {
		$tags[] = "$s:home";
	}
	if ( is_archive() || is_search() ) {
		$tags[] = "$s:archive";
	}
	if ( is_404() ) {
		$tags[] = "$s:404";
	}
	if ( is_singular() ) {
		$tags[] = "$s:p" . (int) get_queried_object_id();
	}
	if ( is_author() ) {
		$tags[] = "$s:u" . (int) get_queried_object_id();
	}
	if ( is_category() || is_tag() || is_tax() ) {
		$term = get_queried_object();
		if ( $term instanceof \WP_Term ) {
			$tags[] = "$s:t" . (int) $term->term_id;
		}
	}
	if ( $wp_query instanceof \WP_Query && is_array( $wp_query->posts ) ) {
		foreach ( $wp_query->posts as $post ) {
			if ( isset( $post->ID ) ) {
				$tags[] = "$s:p" . (int) $post->ID;
			}
		}
	}

	return array_slice( array_values( array_unique( $tags ) ), 0, MAX_TAGS );
}

// --- Purge collection --------------------------------------------------------

/**
 * The set of tags to purge at shutdown. `all` short-circuits everything else.
 */
function &pending(): array {
	static $pending = array( 'all' => false, 'tags' => array() );
	return $pending;
}

function purge_tags( array $tags ): void {
	$p = &pending();
	foreach ( $tags as $tag ) {
		$p['tags'][ (string) $tag ] = true;
	}
}

function purge_all(): void {
	$p        = &pending();
	$p['all'] = true;
}

/** Tags a post change invalidates: the post, home, archives, its terms, author. */
function post_tags( \WP_Post $post ): array {
	$s    = site_tag();
	$tags = array( "$s:p{$post->ID}", "$s:home", "$s:archive" );
	if ( $post->post_author ) {
		$tags[] = "$s:u" . (int) $post->post_author;
	}
	foreach ( get_object_taxonomies( $post ) as $taxonomy ) {
		$terms = get_the_terms( $post, $taxonomy );
		if ( is_array( $terms ) ) {
			foreach ( $terms as $term ) {
				$tags[] = "$s:t{$term->term_id}";
			}
		}
	}
	// Pages set as front/posts page also render the home tag; already included.
	return $tags;
}

function is_relevant_post( \WP_Post $post ): bool {
	if ( wp_is_post_revision( $post ) || wp_is_post_autosave( $post ) ) {
		return false;
	}
	return $post->post_type !== 'customize_changeset';
}

function on_transition_post_status( string $new, string $old, \WP_Post $post ): void {
	if ( $new !== 'publish' && $old !== 'publish' ) {
		return; // Draft to draft: nothing public changed.
	}
	if ( is_relevant_post( $post ) ) {
		purge_tags( post_tags( $post ) );
	}
}

function on_post_change( int $post_id, $post = null ): void {
	$post = $post instanceof \WP_Post ? $post : get_post( $post_id );
	if ( ! $post instanceof \WP_Post || ! is_relevant_post( $post ) ) {
		return;
	}
	// Unpublished content has no cached pages; the status transition hook
	// handles publish <-> anything else.
	if ( $post->post_status !== 'publish' && current_action() === 'save_post' ) {
		return;
	}
	purge_tags( post_tags( $post ) );
}

/** A new comment only changes the page when it is approved right away. */
function on_new_comment( int $comment_id, $approved ): void {
	if ( (int) $approved === 1 ) {
		on_comment_change( $comment_id );
	}
}

function on_comment_change( int $comment_id ): void {
	$comment = get_comment( $comment_id );
	if ( $comment && $comment->comment_post_ID ) {
		purge_tags( array( site_tag() . ':p' . (int) $comment->comment_post_ID ) );
	}
}

function on_term_change( int $term_id ): void {
	$s = site_tag();
	purge_tags( array( "$s:t$term_id", "$s:archive" ) );
}

function on_updated_option( string $option ): void {
	if ( str_starts_with( $option, 'theme_mods_' ) ) {
		purge_all();
	}
}

// --- Purge delivery ----------------------------------------------------------

/**
 * shutdown (priority 100): one non-blocking POST to the engine per request.
 * Failure is logged; the TTL is the safety net.
 */
function flush(): void {
	$p = pending();
	if ( ! $p['all'] && $p['tags'] === array() ) {
		return;
	}
	$engine = env( 'PRESSO_ENGINE_URL' );
	$site   = env( 'PRESSO_SITE_ID' );
	$token  = env( 'PRESSO_PURGE_TOKEN' );
	if ( $engine === null || $site === null ) {
		return; // Standalone (harness without an engine): nothing to purge.
	}

	$body = $p['all'] ? array( 'all' => true ) : array( 'tags' => array_keys( $p['tags'] ) );
	try {
		$response = wp_remote_post(
			rtrim( $engine, '/' ) . '/v1/internal/sites/' . rawurlencode( $site ) . '/purge',
			array(
				'blocking' => false,
				'timeout'  => 2,
				'headers'  => array(
					'Authorization' => 'Bearer ' . (string) $token,
					'Content-Type'  => 'application/json',
				),
				'body'     => (string) json_encode( $body ),
			)
		);
		if ( is_wp_error( $response ) ) {
			log( 'purge_error', array( 'error' => $response->get_error_message() ) );
		}
	} catch ( \Throwable $e ) {
		log( 'purge_error', array( 'error' => $e->getMessage() ) );
	}
}

/**
 * wp presso purge [<tag>...] [--all]
 *
 * Purge the edge cache for this site. Tags are given without the site prefix.
 */
function cli_purge( array $args, array $assoc ): void {
	if ( ! empty( $assoc['all'] ) || $args === array() ) {
		purge_all();
		\WP_CLI::success( 'Queued purge of the whole site.' );
		return;
	}
	$s = site_tag();
	purge_tags( array_map( fn( $t ) => "$s:$t", $args ) );
	\WP_CLI::success( sprintf( 'Queued purge of %d tag(s).', count( $args ) ) );
}
