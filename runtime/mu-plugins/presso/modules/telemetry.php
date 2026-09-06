<?php
/**
 * Module: telemetry. Sampled per-request timing, query count and peak memory
 * as one JSON line on stderr (RUNTIME.md section 10), so the engine can show
 * latency and slow requests per site without a profiler. PRESSO_TELEMETRY_SAMPLE
 * (0..1, default 0.1) is the sampling rate; WP-CLI runs are never sampled.
 */

namespace Presso\Telemetry;

use function Presso\env;
use function Presso\is_cli;
use function Presso\log;

defined( 'ABSPATH' ) || exit;

function boot(): void {
	if ( is_cli() ) {
		return;
	}
	$rate = (float) env( 'PRESSO_TELEMETRY_SAMPLE', '0.1' );
	if ( $rate <= 0 ) {
		return;
	}
	// Late on shutdown: after the cache module's purge flush (100).
	add_action( 'shutdown', fn() => record( $rate ), 200 );
}

function record( float $rate ): void {
	if ( $rate < 1 && mt_rand() / mt_getrandmax() >= $rate ) {
		return;
	}
	$start = $_SERVER['REQUEST_TIME_FLOAT'] ?? microtime( true );
	log(
		'req',
		array(
			'ms'      => round( ( microtime( true ) - (float) $start ) * 1000, 1 ),
			'queries' => function_exists( 'get_num_queries' ) ? get_num_queries() : null,
			'mem'     => memory_get_peak_usage( true ),
			'method'  => $_SERVER['REQUEST_METHOD'] ?? '',
			'uri'     => substr( (string) ( $_SERVER['REQUEST_URI'] ?? '' ), 0, 200 ),
			'status'  => http_response_code(),
			'cached'  => function_exists( 'wp_using_ext_object_cache' ) && (bool) wp_using_ext_object_cache(),
		)
	);
}
