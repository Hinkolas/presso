<?php
/**
 * Module: loopback. Requests a site makes to itself (Site Health's loopback,
 * REST and page-cache checks, plugins calling their own REST API, the
 * pseudo-cron spawn on sites that re-enable it) go to the site's public
 * hostname. Inside the container that name is useless: `.localhost` resolves
 * to nothing, and a public domain resolves to the host's public IP, which
 * depends on hairpin NAT and DNS being right on the machine.
 *
 * Instead the site's own hostname is pinned to the edge container for every
 * WP HTTP API request (CURLOPT_RESOLVE, before curl connects). The URL, Host
 * header and SNI stay intact, so the edge routes and caches the request
 * exactly like a visitor's. Same idea as the browser container's
 * --host-resolver-rules (ORCHESTRATION.md, "Page previews").
 *
 * PRESSO_EDGE_HOST names the edge on the presso network; the engine sets it
 * to the edge container's name, which is also the default.
 */

namespace Presso\Loopback;

use function Presso\env;

defined( 'ABSPATH' ) || exit;

function boot(): void {
	add_action( 'http_api_curl', __NAMESPACE__ . '\\pin_to_edge', 10, 3 );
}

/**
 * Adds a CURLOPT_RESOLVE entry when the request targets the site's own host.
 *
 * @param resource|\CurlHandle $handle      The curl handle, before curl_exec.
 * @param array                $parsed_args Request arguments (unused).
 * @param string               $url         The request URL.
 */
function pin_to_edge( $handle, $parsed_args, $url ): void {
	$host = wp_parse_url( $url, PHP_URL_HOST );
	if ( ! is_string( $host ) || strcasecmp( $host, own_host() ) !== 0 ) {
		return;
	}
	$ip = edge_ip();
	if ( $ip === null ) {
		return;
	}
	curl_setopt( $handle, CURLOPT_RESOLVE, array( "$host:80:$ip", "$host:443:$ip" ) );
}

/**
 * The hostname of WP_HOME (set from PRESSO_SITE_URL by wp-config).
 */
function own_host(): string {
	static $host = null;
	if ( $host === null ) {
		$host = (string) wp_parse_url( home_url(), PHP_URL_HOST );
	}
	return $host;
}

/**
 * The edge container's address on the presso network, from Docker's embedded
 * DNS, resolved once per process. null when it does not resolve (no edge on
 * this network: a site running outside the platform), in which case the
 * request goes out unchanged.
 */
function edge_ip(): ?string {
	static $ip = false;
	if ( $ip === false ) {
		$name = (string) env( 'PRESSO_EDGE_HOST', 'presso-edge' );
		$resolved = gethostbyname( $name );
		$ip = ( $resolved !== $name && filter_var( $resolved, FILTER_VALIDATE_IP ) ) ? $resolved : null;
	}
	return $ip;
}
