<?php
/**
 * Shared helpers for the presso system plugin: environment access and a
 * structured logger. Everything the plugin needs from the platform arrives as
 * PRESSO_* environment variables (the env contract in docs/runtime.md); secrets
 * are read here with getenv() and never defined as constants.
 */

namespace Presso;

defined( 'ABSPATH' ) || exit;

/**
 * Read an environment variable, treating unset and empty alike.
 */
function env( string $name, ?string $default = null ): ?string {
	$value = getenv( $name );
	if ( $value === false || $value === '' ) {
		return $default;
	}
	return $value;
}

/**
 * Integer variant with a lower bound so a bad value cannot disable a limit.
 */
function env_int( string $name, int $default, int $min = 0 ): int {
	$value = env( $name );
	if ( $value === null || ! is_numeric( $value ) ) {
		return $default;
	}
	return max( $min, (int) $value );
}

/**
 * Prefix test. str_starts_with() needs PHP 8 (WordPress polyfills it only
 * from 5.9) and the plugin also runs on PHP 7.4.
 */
function starts_with( string $haystack, string $needle ): bool {
	return strncmp( $haystack, $needle, strlen( $needle ) ) === 0;
}

/**
 * Whether this process is WP-CLI rather than a web request.
 */
function is_cli(): bool {
	return defined( 'WP_CLI' ) && WP_CLI;
}

/**
 * One JSON line on stderr (php/presso.ini routes error_log there), so the
 * engine can pick platform events out of the container log by the "presso"
 * key.
 */
function log( string $event, array $fields = array() ): void {
	error_log( (string) json_encode( array( 'presso' => $event ) + $fields, JSON_UNESCAPED_SLASHES ) );
}
