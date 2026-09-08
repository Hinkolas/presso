<?php
/**
 * Container health check, run by Docker as root every 15s. Exit 0 = healthy,
 * 1 = unhealthy, with a one-line reason on stdout for `docker inspect`.
 *
 * Probes: the web server answers /-/ping without touching PHP (Caddy, or
 * nginx in the FPM flavor); the FPM flavor additionally answers /-/fpm-ping
 * from php-fpm itself; and when a database is configured, a real connection
 * with SELECT 1 using the same PRESSO_DB_* variables wp-config.php reads. No
 * curl: file_get_contents with a stream timeout keeps this dependency-free.
 * Runs on PHP 7.4 too, so no PHP 8 syntax here.
 */

function presso_health_env( string $name, ?string $default = null ): ?string {
	$value = getenv( $name );
	return ( $value === false || $value === '' ) ? $default : $value;
}

function presso_health_fail( string $reason ): void {
	echo $reason, "\n";
	exit( 1 );
}

/**
 * GET a local path and require an exact body.
 */
function presso_health_probe( string $path, string $want ): void {
	$ctx  = stream_context_create( array( 'http' => array( 'timeout' => 3, 'ignore_errors' => true ) ) );
	$body = @file_get_contents( 'http://127.0.0.1:8080' . $path, false, $ctx );
	if ( $body === false ) {
		presso_health_fail( $path . ': no response on :8080' );
	}
	if ( trim( $body ) !== $want ) {
		presso_health_fail( $path . ': unexpected body ' . json_encode( substr( $body, 0, 64 ) ) );
	}
}

// (a) Web server liveness; in the FPM flavor php-fpm's own ping as well.
presso_health_probe( '/-/ping', 'ok' );
if ( presso_health_env( 'PRESSO_FLAVOR' ) === 'fpm' ) {
	presso_health_probe( '/-/fpm-ping', 'pong' );
}

// (b) Database reachability, only when a database is configured.
$host = presso_health_env( 'PRESSO_DB_HOST' );
if ( $host !== null ) {
	$port = 3306;
	if ( strpos( $host, ':' ) !== false && $host[0] !== '/' ) {
		list( $host, $p ) = explode( ':', $host, 2 );
		if ( ctype_digit( $p ) ) {
			$port = (int) $p;
		}
	}

	mysqli_report( MYSQLI_REPORT_OFF );
	$db = mysqli_init();
	$db->options( MYSQLI_OPT_CONNECT_TIMEOUT, 3 );
	$db->options( MYSQLI_OPT_READ_TIMEOUT, 3 );
	$ok = @$db->real_connect(
		$host,
		presso_health_env( 'PRESSO_DB_USER', '' ),
		presso_health_env( 'PRESSO_DB_PASSWORD', '' ),
		presso_health_env( 'PRESSO_DB_NAME', '' ),
		$port
	);
	if ( ! $ok ) {
		presso_health_fail( 'db: connect failed: ' . $db->connect_error );
	}
	$result = $db->query( 'SELECT 1' );
	if ( $result === false ) {
		presso_health_fail( 'db: SELECT 1 failed: ' . $db->error );
	}
	$db->close();
}

echo "ok\n";
exit( 0 );
