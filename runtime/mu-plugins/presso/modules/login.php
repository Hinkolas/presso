<?php
/**
 * Module: login. One-click login for the engine and the harness (RUNTIME.md
 * section 14). The engine mints a short-lived token signed with the per-site
 * PRESSO_LOGIN_SECRET; wp-login.php?presso_login=<token> validates it and
 * creates a normal session. The secret is read with getenv() only, never
 * defined as a constant. Inactive when no secret is set.
 *
 * Token: base64url(json) "." base64url(hmac_sha256(json, secret)), where json
 * is {"login":"<user_login>","exp":<unix seconds>,"nonce":"<random>"}. exp
 * may be at most 300s in the future; each nonce is accepted once.
 */

namespace Presso\Login;

use function Presso\env;
use function Presso\log;

defined( 'ABSPATH' ) || exit;

const MAX_LIFETIME = 300;

function boot(): void {
	if ( env( 'PRESSO_LOGIN_SECRET' ) === null ) {
		return;
	}
	add_action( 'login_init', __NAMESPACE__ . '\\handle' );
}

function handle(): void {
	if ( ! isset( $_GET['presso_login'] ) || ! is_string( $_GET['presso_login'] ) ) {
		return;
	}
	$user = verify( $_GET['presso_login'], (string) env( 'PRESSO_LOGIN_SECRET' ) );
	if ( $user === null ) {
		log( 'login_rejected', array( 'ip' => $_SERVER['REMOTE_ADDR'] ?? '' ) );
		wp_die( 'Invalid login link', 'Invalid login link', array( 'response' => 403 ) );
	}

	wp_set_current_user( $user->ID );
	wp_set_auth_cookie( $user->ID, false, is_ssl() );
	do_action( 'wp_login', $user->user_login, $user );
	log( 'login', array( 'user' => $user->user_login ) );

	wp_safe_redirect( admin_url() );
	exit;
}

/**
 * Returns the user the token grants access to, or null for any defect:
 * malformed, bad signature, expired, too long-lived, replayed, unknown user.
 */
function verify( string $token, string $secret ): ?\WP_User {
	$parts = explode( '.', $token, 2 );
	if ( count( $parts ) !== 2 ) {
		return null;
	}
	$json = b64url_decode( $parts[0] );
	$sig  = b64url_decode( $parts[1] );
	if ( $json === null || $sig === null ) {
		return null;
	}
	if ( ! hash_equals( hash_hmac( 'sha256', $json, $secret, true ), $sig ) ) {
		return null;
	}

	$claims = json_decode( $json, true );
	if ( ! is_array( $claims ) || empty( $claims['login'] ) || empty( $claims['nonce'] ) || ! isset( $claims['exp'] ) ) {
		return null;
	}
	$exp = (int) $claims['exp'];
	$now = time();
	if ( $exp < $now || $exp - $now > MAX_LIFETIME ) {
		return null;
	}

	// Single use: remember the nonce for longer than any token can live.
	$key = 'presso_login_' . substr( hash( 'sha256', (string) $claims['nonce'] ), 0, 40 );
	if ( get_transient( $key ) ) {
		return null;
	}
	set_transient( $key, 1, 2 * MAX_LIFETIME );

	$user = get_user_by( 'login', (string) $claims['login'] );
	return $user instanceof \WP_User ? $user : null;
}

function b64url_decode( string $s ): ?string {
	$decoded = base64_decode( strtr( $s, '-_', '+/' ), true );
	return $decoded === false ? null : $decoded;
}
