<?php
/**
 * Container health check, run by Docker as root every 15s. Exit 0 = healthy,
 * 1 = unhealthy, with a one-line reason on stdout for `docker inspect`.
 *
 * Two probes: FrankenPHP answers /-/ping (Caddy alone, no PHP), and when a
 * database is configured, a real connection with SELECT 1 using the same
 * PRESSO_DB_* variables wp-config.php reads. No curl: file_get_contents with
 * a stream timeout keeps this dependency-free.
 */

function presso_health_env( string $name, ?string $default = null ): ?string {
	$value = getenv( $name );
	return ( $value === false || $value === '' ) ? $default : $value;
}

function presso_health_fail( string $reason ): never {
	echo $reason, "\n";
	exit( 1 );
}

// (a) Web server liveness.
$ctx  = stream_context_create( array( 'http' => array( 'timeout' => 3, 'ignore_errors' => true ) ) );
$body = @file_get_contents( 'http://127.0.0.1:8080/-/ping', false, $ctx );
if ( $body === false ) {
	presso_health_fail( 'ping: no response from frankenphp on :8080' );
}
if ( trim( $body ) !== 'ok' ) {
	presso_health_fail( 'ping: unexpected body ' . json_encode( substr( $body, 0, 64 ) ) );
}

// (b) Database reachability, only when a database is configured.
$host = presso_health_env( 'PRESSO_DB_HOST' );
if ( $host !== null ) {
	$port = 3306;
	if ( str_contains( $host, ':' ) && ! str_starts_with( $host, '/' ) ) {
		[ $host, $p ] = explode( ':', $host, 2 );
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
